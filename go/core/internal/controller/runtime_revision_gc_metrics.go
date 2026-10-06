package controller

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type runtimeRevisionGCStage string

const (
	gcStageDiscovery  runtimeRevisionGCStage = conv.KagentGCStageDiscovery
	gcStageCollection runtimeRevisionGCStage = conv.KagentGCStageCollection
)

type runtimeRevisionGCMetrics struct {
	pending  atomic.Pointer[int64]
	duration metric.Float64Histogram
}

func newRuntimeRevisionGCMetrics(provider metric.MeterProvider) (*runtimeRevisionGCMetrics, error) {
	if provider == nil {
		return nil, fmt.Errorf("runtime revision GC metrics require a meter provider")
	}
	meter := provider.Meter("github.com/kagent-dev/kagent/go/core/internal/controller",
		metric.WithInstrumentationVersion(version.Version))
	duration, err := meter.Float64Histogram(conv.KagentRuntimeRevisionGCDuration,
		metric.WithUnit(conv.KagentRuntimeRevisionGCDurationUnit),
		metric.WithDescription(conv.KagentRuntimeRevisionGCDurationDescription),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60))
	if err != nil {
		return nil, fmt.Errorf("create runtime revision GC duration metric: %w", err)
	}
	metrics := &runtimeRevisionGCMetrics{duration: duration}
	_, err = meter.Int64ObservableGauge(conv.KagentRuntimeRevisionGCPending,
		metric.WithUnit(conv.KagentRuntimeRevisionGCPendingUnit),
		metric.WithDescription(conv.KagentRuntimeRevisionGCPendingDescription),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			if count := metrics.pending.Load(); count != nil {
				observer.Observe(*count)
			}
			return nil
		}))
	if err != nil {
		return nil, fmt.Errorf("create runtime revision GC pending metric: %w", err)
	}
	return metrics, nil
}

func (m *runtimeRevisionGCMetrics) recordPending(count int64) {
	m.pending.Store(&count)
}

func (m *runtimeRevisionGCMetrics) recordDuration(ctx context.Context, stage runtimeRevisionGCStage, duration time.Duration, err error) {
	if ctx.Err() != nil {
		return
	}
	attributes := []attribute.KeyValue{conv.KagentGCStageKey.String(string(stage))}
	if err != nil {
		errorType := "_OTHER"
		if st, ok := status.FromError(err); ok && st.Code() >= codes.Canceled && st.Code() <= codes.Unauthenticated {
			errorType = st.Code().String()
		}
		attributes = append(attributes, semconv.ErrorTypeKey.String(errorType))
	}
	m.duration.Record(ctx, duration.Seconds(), metric.WithAttributes(attributes...))
}
