package session

import (
	"context"
	"fmt"
	"sync"
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
)

func newWorkClaimCounter(ctx context.Context, provider metric.MeterProvider, name, unit, description string) (metric.Int64Counter, error) {
	counter, err := provider.Meter("github.com/kagent-dev/kagent/go/core/internal/service/session",
		metric.WithInstrumentationVersion(version.Version)).Int64Counter(name,
		metric.WithDescription(description),
		metric.WithUnit(unit))
	if err != nil {
		return nil, fmt.Errorf("failed to create %s counter: %w", name, err)
	}
	for _, trigger := range [...]workTrigger{workSignal, workRecovery, workStartup, workRetry} {
		counter.Add(ctx, 0, metric.WithAttributes(conv.KagentWorkerTriggerKey.String(string(trigger))))
	}
	return counter, nil
}

func recordWorkClaim(ctx context.Context, counter metric.Int64Counter, trigger workTrigger) {
	counter.Add(ctx, 1, metric.WithAttributes(conv.KagentWorkerTriggerKey.String(string(trigger))))
}

// workDrain carries attribution through concurrent handoffs. Its discovery log
// is shared by all workers in the drain, while every claim is counted separately.
type workDrain struct {
	trigger   workTrigger
	discovery sync.Once
}

func (w *workDrain) logRecoveryClaims(ctx context.Context, operation string, interval time.Duration, claimed int) {
	if w.trigger != workRecovery || claimed == 0 {
		return
	}
	w.discovery.Do(func() {
		logging.FromContext(ctx).InfoContext(ctx, "recovery scan found work",
			"operation", operation, "trigger", string(w.trigger),
			"recovery_interval_seconds", interval.Seconds(), "claimed_count", claimed)
	})
}
