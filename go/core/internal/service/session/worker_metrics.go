package session

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

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
