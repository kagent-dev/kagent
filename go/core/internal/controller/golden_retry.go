package controller

import (
	"context"
	"fmt"
	"time"

	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/wait"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
)

const goldenRetryingReason = "ActorTemplateRetrying"

type goldenRetryPolicy struct {
	Delays            []time.Duration
	MaxJitterFraction float64
}

var defaultGoldenRetryPolicy = goldenRetryPolicy{
	Delays:            []time.Duration{15 * time.Second, time.Minute, 4 * time.Minute, 10 * time.Minute},
	MaxJitterFraction: 0.2,
}

func (p goldenRetryPolicy) jitteredDelays(jitter func(time.Duration, float64) time.Duration) []time.Duration {
	delays := make([]time.Duration, len(p.Delays))
	for i, delay := range p.Delays {
		delays[i] = jitter(delay, p.MaxJitterFraction)
	}
	return delays
}

type goldenRetryStore interface {
	EnsureGoldenRetry(ctx context.Context, revision, crashedTemplateUID string, now time.Time, delays []time.Duration) (database.GoldenRetry, error)
	GoldenRetryAttempts(ctx context.Context, revision string) (int, error)
}

type goldenRetries struct {
	policy goldenRetryPolicy
	jitter func(time.Duration, float64) time.Duration
}

func (g goldenRetries) delays() []time.Duration {
	policy, jitter := g.policy, g.jitter
	if len(policy.Delays) == 0 {
		policy = defaultGoldenRetryPolicy
	}
	if jitter == nil {
		jitter = wait.Jitter
	}
	return policy.jitteredDelays(jitter)
}

type goldenPhase uint8

const (
	goldenUnobserved goldenPhase = iota
	goldenBuilding
	goldenReady
	goldenRetrying
)

type goldenProgress struct {
	phase     goldenPhase
	retries   int
	message   string
	nextCheck time.Time
}

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

func (g goldenRetries) retry(ctx context.Context, store goldenRetryStore, templates actorTemplateClient, revision string, crashed *ateapipb.ActorTemplate) (goldenProgress, error) {
	ref := crashed.GetMetadata()
	crash := crashed.GetStatus().GetGoldenSnapshotStatus().GetErrorMessage()
	now := time.Now()
	retry, err := store.EnsureGoldenRetry(ctx, revision, ref.GetUid(), now, g.delays())
	if err != nil {
		return goldenProgress{}, err
	}
	progress := goldenProgress{phase: goldenRetrying, retries: retry.Retries}
	if retry.RetryAt.After(now) {
		progress.message = fmt.Sprintf("golden build failed, retry %d at %s: %s", retry.Retries, retry.RetryAt.UTC().Format(time.RFC3339), crash)
		progress.nextCheck = retry.RetryAt
		return progress, nil
	}
	if err := templates.DeleteActorTemplate(ctx, ref.GetAtespace(), ref.GetName(), ref.GetUid()); err != nil {
		progress.message = fmt.Sprintf("golden build failed, retry %d waits to delete the crashed template (%s): %s", retry.Retries, status.Code(err), crash)
		return progress, fmt.Errorf("delete crashed ActorTemplate %s/%s: %w", ref.GetAtespace(), ref.GetName(), err)
	}
	progress.message = fmt.Sprintf("golden build failed, retry %d started: %s", retry.Retries, crash)
	progress.nextCheck = now
	return progress, nil
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

func (p goldenProgress) waiting(now time.Time) bool {
	return p.phase == goldenRetrying && (p.nextCheck.IsZero() || now.Before(p.nextCheck))
}

func (p goldenProgress) due(now time.Time) bool {
	return p.phase == goldenBuilding || (p.phase == goldenRetrying && !p.waiting(now))
}

func (p goldenProgress) pendingMessage(waiting string) string {
	if p.retries == 0 {
		return waiting
	}
	return fmt.Sprintf("rebuilding after golden failure, retry %d; %s", p.retries, waiting)
}
