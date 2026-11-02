package controller

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"time"
	"unicode"

	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/status"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

const goldenRetryingReason = "ActorTemplateRetrying"

// goldenRetries is the backoff policy for rebuilding a failed golden snapshot.
// Substrate cannot rebuild a failed ActorTemplate in place, so each retry
// deletes it and lets the next reconcile create it again.
type goldenRetries struct {
	delays            []time.Duration
	maxJitterFraction float64
}

var defaultGoldenRetries = goldenRetries{
	delays:            []time.Duration{15 * time.Second, time.Minute, 4 * time.Minute, 10 * time.Minute},
	maxJitterFraction: 0.2,
}

// retryAt is when the failed template is deleted: its failure time plus the
// delay for this retry, past the last delay repeating it.
func (g goldenRetries) retryAt(retry database.GoldenRetry, failedUID string) time.Time {
	delay := g.delays[min(max(retry.Retries-1, 0), len(g.delays)-1)]
	return retry.FailedAt.Add(jitter(delay, g.maxJitterFraction, failedUID))
}

// jitter stretches delay by up to maxFraction. The failed template's UID picks
// the amount, so every reconcile and restart computes the same retry time while
// retries of different templates still spread out.
func jitter(delay time.Duration, maxFraction float64, uid string) time.Duration {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(uid))
	fraction := float64(hash.Sum64()) / math.MaxUint64
	return delay + time.Duration(maxFraction*fraction*float64(delay))
}

type goldenPhase uint8

const (
	goldenUnobserved goldenPhase = iota
	goldenBuilding
	goldenReady
	goldenRetrying
)

// goldenProgress is what one reconcile learned about a revision's golden build.
// The poll loops and status read it instead of the raw ActorTemplate.
type goldenProgress struct {
	phase     goldenPhase
	retries   int
	message   string
	nextCheck time.Time
}

// goldenRecordOptions reports a template's golden build to the store: the
// failure time once the build fails, or Ready once the golden tag exists.
func goldenRecordOptions(template *ateapipb.ActorTemplate, now time.Time) database.RecordRevisionOptions {
	golden := template.GetStatus().GetGoldenSnapshotStatus()
	if golden.GetErrorMessage() != "" {
		return database.RecordRevisionOptions{GoldenFailedAt: now}
	}
	return database.RecordRevisionOptions{Ready: golden.GetGoldenTag() != nil}
}

// advance classifies the golden build from the recorded retry state. A failed
// build goes to retry.
func (g goldenRetries) advance(ctx context.Context, templates actorTemplateClient, revision string,
	template *ateapipb.ActorTemplate, retry database.GoldenRetry, now time.Time) (goldenProgress, error) {
	golden := template.GetStatus().GetGoldenSnapshotStatus()
	switch {
	case golden.GetErrorMessage() != "":
		return g.retry(ctx, templates, revision, template, retry, now)
	case golden.GetGoldenTag() != nil:
		return goldenProgress{phase: goldenReady}, nil
	}
	return goldenProgress{phase: goldenBuilding, retries: retry.Retries}, nil
}

// retry waits out the backoff for a failed template, then deletes it by UID so
// the next reconcile creates a fresh one and Substrate builds a new golden.
func (g goldenRetries) retry(ctx context.Context, templates actorTemplateClient, revision string,
	failed *ateapipb.ActorTemplate, retry database.GoldenRetry, now time.Time) (goldenProgress, error) {
	ref := failed.GetMetadata()
	failure := failed.GetStatus().GetGoldenSnapshotStatus().GetErrorMessage()
	// Substrate's message can quote configuration, so status names only its reason.
	reason := goldenFailureReason(failure)
	if retry.New {
		logging.FromContext(ctx).WarnContext(ctx, "golden build failed", "revision", revision,
			"atespace", ref.GetAtespace(), "actor_template", ref.GetName(), "uid", ref.GetUid(), "error", failure)
	}
	retryAt := g.retryAt(retry, ref.GetUid())
	progress := goldenProgress{phase: goldenRetrying, retries: retry.Retries}
	if retryAt.After(now) {
		progress.message = fmt.Sprintf("golden build failed (%s), retry %d at %s", reason, retry.Retries, retryAt.UTC().Format(time.RFC3339))
		progress.nextCheck = retryAt
		return progress, nil
	}
	if err := templates.DeleteActorTemplate(ctx, ref.GetAtespace(), ref.GetName(), ref.GetUid()); err != nil {
		progress.message = fmt.Sprintf("golden build failed (%s), retry %d waits to delete the failed template (%s)", reason, retry.Retries, status.Code(err))
		return progress, fmt.Errorf("delete failed ActorTemplate %s/%s: %w", ref.GetAtespace(), ref.GetName(), err)
	}
	progress.message = fmt.Sprintf("golden build failed (%s), retry %d started", reason, retry.Retries)
	progress.nextCheck = now
	return progress, nil
}

// goldenFailureReason returns the reason that prefixes Substrate's
// "<Reason>: <message>" format, or "unknown" when the message has none.
func goldenFailureReason(message string) string {
	reason, _, found := strings.Cut(message, ":")
	if !found || reason == "" || strings.ContainsFunc(reason, unicode.IsSpace) {
		return "unknown"
	}
	return reason
}

func (p goldenProgress) failure() *ReconciliationFailure {
	if p.phase != goldenRetrying {
		return nil
	}
	return &ReconciliationFailure{Condition: kagentv1alpha3.AgentConditionReady, Reason: goldenRetryingReason, Message: p.message, Retryable: true}
}

func (p goldenProgress) ready() bool {
	return p.phase == goldenReady
}

// due reports whether the poll loop should requeue the revision: a build is in
// progress, or a retry's backoff has elapsed.
func (p goldenProgress) due(now time.Time) bool {
	return p.phase == goldenBuilding || (p.phase == goldenRetrying && !p.nextCheck.IsZero() && !now.Before(p.nextCheck))
}

func (p goldenProgress) pendingMessage(waiting string) string {
	if p.retries == 0 {
		return waiting
	}
	return fmt.Sprintf("rebuilding after golden failure, retry %d; %s", p.retries, waiting)
}
