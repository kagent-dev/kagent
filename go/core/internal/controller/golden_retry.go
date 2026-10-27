package controller

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/wait"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

const goldenRetryingReason = "ActorTemplateRetrying"

type goldenRetryStore interface {
	EnsureGoldenRetry(ctx context.Context, revision, failedTemplateUID string, now time.Time, delays []time.Duration) (database.GoldenRetry, error)
	GoldenRetryAttempts(ctx context.Context, revision string) (int, error)
}

// goldenRetries is the backoff policy for rebuilding a failed golden snapshot.
// Substrate cannot rebuild a failed ActorTemplate in place, so each retry
// deletes it and lets the next reconcile create it again.
type goldenRetries struct {
	delays            []time.Duration
	maxJitterFraction float64
	jitter            func(time.Duration, float64) time.Duration
}

var defaultGoldenRetries = goldenRetries{
	delays:            []time.Duration{15 * time.Second, time.Minute, 4 * time.Minute, 10 * time.Minute},
	maxJitterFraction: 0.2,
	jitter:            wait.Jitter,
}

func (g goldenRetries) jitteredDelays() []time.Duration {
	delays := make([]time.Duration, len(g.delays))
	for i, delay := range g.delays {
		delays[i] = g.jitter(delay, g.maxJitterFraction)
	}
	return delays
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

// observe records the revision, marking it Ready once the golden tag exists,
// then classifies the build. A failed build goes to retry.
func (g goldenRetries) observe(ctx context.Context, store goldenRetryStore, templates actorTemplateClient, revision string,
	template *ateapipb.ActorTemplate, record func(database.RecordRevisionOptions) error) (goldenProgress, error) {
	golden := template.GetStatus().GetGoldenSnapshotStatus()
	if err := record(database.RecordRevisionOptions{Ready: golden.GetGoldenTag() != nil}); err != nil {
		return goldenProgress{}, err
	}
	switch {
	case golden.GetErrorMessage() != "":
		return g.retry(ctx, store, templates, revision, template)
	case golden.GetGoldenTag() != nil:
		return goldenProgress{phase: goldenReady}, nil
	}
	retries, err := store.GoldenRetryAttempts(ctx, revision)
	if err != nil {
		return goldenProgress{}, err
	}
	return goldenProgress{phase: goldenBuilding, retries: retries}, nil
}

// retry waits out the backoff for a failed template, then deletes it by UID so
// the next reconcile creates a fresh one and Substrate builds a new golden.
func (g goldenRetries) retry(ctx context.Context, store goldenRetryStore, templates actorTemplateClient, revision string, failed *ateapipb.ActorTemplate) (goldenProgress, error) {
	ref := failed.GetMetadata()
	failure := failed.GetStatus().GetGoldenSnapshotStatus().GetErrorMessage()
	// Substrate's message can quote configuration, so status names only its reason.
	reason := goldenFailureReason(failure)
	logging.FromContext(ctx).WarnContext(ctx, "golden build failed", "revision", revision,
		"atespace", ref.GetAtespace(), "actor_template", ref.GetName(), "uid", ref.GetUid(), "error", failure)
	now := time.Now()
	retry, err := store.EnsureGoldenRetry(ctx, revision, ref.GetUid(), now, g.jitteredDelays())
	if err != nil {
		return goldenProgress{}, err
	}
	progress := goldenProgress{phase: goldenRetrying, retries: retry.Retries}
	if retry.RetryAt.After(now) {
		progress.message = fmt.Sprintf("golden build failed (%s), retry %d at %s", reason, retry.Retries, retry.RetryAt.UTC().Format(time.RFC3339))
		progress.nextCheck = retry.RetryAt
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

// waiting reports whether a retry is still inside its backoff.
func (p goldenProgress) waiting(now time.Time) bool {
	return p.phase == goldenRetrying && (p.nextCheck.IsZero() || now.Before(p.nextCheck))
}

// due reports whether the poll loop should requeue the revision: a build is in
// progress, or a retry's backoff has elapsed.
func (p goldenProgress) due(now time.Time) bool {
	return p.phase == goldenBuilding || (p.phase == goldenRetrying && !p.waiting(now))
}

func (p goldenProgress) pendingMessage(waiting string) string {
	if p.retries == 0 {
		return waiting
	}
	return fmt.Sprintf("rebuilding after golden failure, retry %d; %s", p.retries, waiting)
}
