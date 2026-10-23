package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.ate.dev/substrate/pkg/proto/ateapipb"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// goldenCrashPrefix starts Substrate's "<Reason>: <detail>" error_message when
// the golden actor crashed before its snapshot. Other reasons stay terminal.
const goldenCrashPrefix = "GoldenActorCrashed:"

// goldenRetryPolicy is the golden-build retry budget after GoldenActorCrashed:
// one delay per retry, each lengthened by up to Jitter of itself.
type goldenRetryPolicy struct {
	Delays []time.Duration
	Jitter float64
}

// defaultGoldenRetryPolicy allows 3 retries after 15s, 1m and 4m, plus up to 20%.
var defaultGoldenRetryPolicy = goldenRetryPolicy{
	Delays: []time.Duration{15 * time.Second, time.Minute, 4 * time.Minute},
	Jitter: 0.2,
}

// jitteredDelays draws every delay; the store uses only the next retry's.
func (p goldenRetryPolicy) jitteredDelays(random func() float64) []time.Duration {
	delays := make([]time.Duration, len(p.Delays))
	for i, delay := range p.Delays {
		delays[i] = delay + time.Duration(float64(delay)*p.Jitter*random())
	}
	return delays
}

func isGoldenCrash(template *ateapipb.ActorTemplate) bool {
	return strings.HasPrefix(template.GetStatus().GetGoldenSnapshotStatus().GetErrorMessage(), goldenCrashPrefix)
}

// retryGoldenCrash spends the revision's durable budget on a crashed template:
// it waits out the backoff, deletes the crashed template by UID and lets the
// next pass recreate it under the same name. An exhausted budget is terminal.
func (r *Reconciler) retryGoldenCrash(ctx context.Context, state AgentReconciliation, crashed *ateapipb.ActorTemplate) error {
	ref := crashed.GetMetadata()
	crash := crashed.GetStatus().GetGoldenSnapshotStatus().GetErrorMessage()
	budget := len(r.goldenRetry.Delays)
	now := time.Now()
	retry, err := r.store.ScheduleGoldenRetry(ctx, state.Target.RevisionID.String(), ref.GetUid(), now, r.goldenRetry.jitteredDelays(r.random))
	if err != nil {
		return r.observePreparationError(state, err)
	}
	if retry.Exhausted {
		r.observePreparation(state, nil, &ReconciliationFailure{
			Condition: kagentv1alpha3.AgentConditionReady,
			Reason:    "ActorTemplateFailed",
			Message:   fmt.Sprintf("golden build crashed %d times, all %d retries used: %s", retry.Attempts+1, budget, crash),
		})
		return nil
	}
	key := state.ResourceName()
	if wait := retry.RetryAt.Sub(now); wait > 0 {
		r.observePreparation(state, nil, goldenRetrying(fmt.Sprintf("golden build crashed, retry %d of %d at %s: %s",
			retry.Attempts, budget, retry.RetryAt.UTC().Format(time.RFC3339), crash)))
		time.AfterFunc(wait, func() { r.agents.Add(key) })
		return nil
	}
	if err := r.templates.DeleteActorTemplateUID(ctx, ref.GetAtespace(), ref.GetName(), ref.GetUid()); err != nil {
		return r.observePreparationError(state, fmt.Errorf("delete crashed ActorTemplate %s/%s: %w", ref.GetAtespace(), ref.GetName(), err))
	}
	r.observePreparation(state, nil, goldenRetrying(fmt.Sprintf("golden build crashed, retry %d of %d started: %s", retry.Attempts, budget, crash)))
	r.agents.Add(key)
	return nil
}

func goldenRetrying(message string) *ReconciliationFailure {
	return &ReconciliationFailure{Condition: kagentv1alpha3.AgentConditionReady, Reason: "ActorTemplateRetrying", Message: message, Retryable: true}
}
