package session

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
)

type workTrigger string

const (
	operationSessionQuiescence = "session_quiescence"
	operationPushDelivery      = "push_delivery"

	workSignal   workTrigger = conv.KagentWorkerTriggerSignal
	workRecovery workTrigger = conv.KagentWorkerTriggerRecovery
	workStartup  workTrigger = conv.KagentWorkerTriggerStartup
	workRetry    workTrigger = conv.KagentWorkerTriggerRetry
	workBacklog  workTrigger = conv.KagentWorkerTriggerBacklog
)

func newWorkClaimCounter(ctx context.Context, provider metric.MeterProvider, name, unit, description string) (metric.Int64Counter, error) {
	counter, err := provider.Meter("github.com/kagent-dev/kagent/go/core/internal/service/session",
		metric.WithInstrumentationVersion(version.Version)).Int64Counter(name,
		metric.WithDescription(description),
		metric.WithUnit(unit))
	if err != nil {
		return nil, fmt.Errorf("failed to create %s counter: %w", name, err)
	}
	for _, trigger := range [...]workTrigger{workSignal, workRecovery, workStartup, workRetry, workBacklog} {
		counter.Add(ctx, 0, metric.WithAttributes(conv.KagentWorkerTriggerKey.String(string(trigger))))
	}
	return counter, nil
}

func recordWorkClaim(ctx context.Context, counter metric.Int64Counter, trigger workTrigger) {
	counter.Add(ctx, 1, metric.WithAttributes(conv.KagentWorkerTriggerKey.String(string(trigger))))
}

// logRecoveryClaim reports a timer-driven discovery before processing starts.
func (t workTrigger) logRecoveryClaim(ctx context.Context, operation string, interval time.Duration, claimID slog.Attr) {
	if t != workRecovery {
		return
	}
	logging.FromContext(ctx).LogAttrs(ctx, slog.LevelInfo, "recovery scan found work",
		slog.String("operation", operation), slog.Float64("recovery_interval_seconds", interval.Seconds()), claimID)
}

const (
	lifecycleEventClaim    = conv.KagentRuntimeLifecycleJobEventClaimed
	lifecycleEventRetry    = conv.KagentRuntimeLifecycleJobEventRetry
	lifecycleEventPark     = conv.KagentRuntimeLifecycleJobEventParked
	lifecycleEventSuspend  = conv.KagentRuntimeLifecycleJobEventSuspending
	lifecycleEventComplete = conv.KagentRuntimeLifecycleJobEventCompleted
	lifecycleEventRelease  = conv.KagentRuntimeLifecycleJobEventReleased
	lifecycleEventStopped  = conv.KagentRuntimeLifecycleJobEventStopped
)

// newQuiescenceWorker initializes counters on a workflow copy before workers
// start. Inline lifecycle callers can keep using the original without mutation.
func (w *ActorWorkflow) newQuiescenceWorker(ctx context.Context) (*ActorWorkflow, error) {
	claims, err := newWorkClaimCounter(ctx, w.meterProvider,
		conv.KagentSessionQuiescenceClaimed, conv.KagentSessionQuiescenceClaimedUnit, conv.KagentSessionQuiescenceClaimedDescription)
	if err != nil {
		return nil, err
	}
	counter, err := w.meterProvider.Meter("github.com/kagent-dev/kagent/go/core/internal/service/session",
		metric.WithInstrumentationVersion(version.Version)).Int64Counter(
		conv.KagentRuntimeLifecycleJobEvents, metric.WithDescription(conv.KagentRuntimeLifecycleJobEventsDescription), metric.WithUnit(conv.KagentRuntimeLifecycleJobEventsUnit))
	if err != nil {
		return nil, fmt.Errorf("create runtime lifecycle counter: %w", err)
	}
	worker := *w
	worker.lifecycleEvents = counter
	worker.quiescenceClaims = claims
	return &worker, nil
}

// recordDispatchCleanup keeps runtime IDs in logs and restricts counter labels
// to the fixed workflow, phase, and event values.
func (w *ActorWorkflow) recordDispatchCleanup(ctx context.Context, work *database.SessionDispatchCleanup, event string, err error) {
	phase := database.RuntimeLifecycleJobRecovering
	if work.Suspending {
		phase = database.RuntimeLifecycleJobSuspending
	}
	w.lifecycleEvents.Add(ctx, 1, metric.WithAttributes(
		conv.KagentRuntimeLifecycleJobKindKey.String(database.RuntimeLifecycleJobRecoverAbandonedDispatch),
		conv.KagentRuntimeLifecycleJobPhaseKey.String(phase), conv.KagentRuntimeLifecycleJobEventKey.String(event)))
	args := []any{"runtime_id", work.Session.Id, "kind", database.RuntimeLifecycleJobRecoverAbandonedDispatch,
		"generation", work.DispatchID, "phase", phase, "executor_id", work.ExecutorID,
		"available_at", work.AvailableAt, "event", event}
	if err != nil {
		args = append(args, "error", err)
	}
	logging.FromContext(ctx).DebugContext(ctx, "runtime lifecycle job", args...)
}
