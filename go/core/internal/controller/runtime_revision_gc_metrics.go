package controller

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
	"go.opentelemetry.io/otel/metric"
)

type runtimeRevisionGCStage string

const (
	gcStageDiscovery  runtimeRevisionGCStage = conv.KagentGCStageDiscovery
	gcStageCollection runtimeRevisionGCStage = conv.KagentGCStageCollection
)

type runtimeRevisionGCMetrics struct {
	pending      atomic.Pointer[int64]
	failures     metric.Int64Counter
	registration metric.Registration
}

func newRuntimeRevisionGCMetrics(provider metric.MeterProvider) (*runtimeRevisionGCMetrics, error) {
	if provider == nil {
		return nil, fmt.Errorf("runtime revision GC metrics require a meter provider")
	}
	meter := provider.Meter("github.com/kagent-dev/kagent/go/core/internal/controller")
	pending, err := meter.Int64ObservableGauge(conv.KagentRuntimeRevisionGCPending,
		metric.WithUnit(conv.KagentRuntimeRevisionGCPendingUnit),
		metric.WithDescription(conv.KagentRuntimeRevisionGCPendingDescription))
	if err != nil {
		return nil, fmt.Errorf("create runtime revision GC pending metric: %w", err)
	}
	failures, err := meter.Int64Counter(conv.KagentRuntimeRevisionGCFailures,
		metric.WithUnit(conv.KagentRuntimeRevisionGCFailuresUnit),
		metric.WithDescription(conv.KagentRuntimeRevisionGCFailuresDescription))
	if err != nil {
		return nil, fmt.Errorf("create runtime revision GC failure metric: %w", err)
	}
	metrics := &runtimeRevisionGCMetrics{failures: failures}
	metrics.registration, err = meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		if count := metrics.pending.Load(); count != nil {
			observer.ObserveInt64(pending, *count)
		}
		return nil
	}, pending)
	if err != nil {
		return nil, fmt.Errorf("register runtime revision GC pending callback: %w", err)
	}
	for _, stage := range []runtimeRevisionGCStage{gcStageDiscovery, gcStageCollection} {
		failures.Add(context.Background(), 0, metric.WithAttributes(conv.KagentGCStageKey.String(string(stage))))
	}
	return metrics, nil
}

func (m *runtimeRevisionGCMetrics) recordPending(count int64) {
	m.pending.Store(&count)
}

func (m *runtimeRevisionGCMetrics) recordFailure(ctx context.Context, stage runtimeRevisionGCStage) {
	m.failures.Add(ctx, 1, metric.WithAttributes(conv.KagentGCStageKey.String(string(stage))))
}
