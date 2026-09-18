package controller

import (
	"fmt"
	"math"

	"github.com/prometheus/client_golang/prometheus"
)

type runtimeRevisionGCStage string

const (
	gcStageDiscovery  runtimeRevisionGCStage = "discovery"
	gcStageCollection runtimeRevisionGCStage = "collection"
)

type runtimeRevisionGCMetrics struct {
	pending  prometheus.Gauge
	failures *prometheus.CounterVec
}

func newRuntimeRevisionGCMetrics(registerer prometheus.Registerer) (*runtimeRevisionGCMetrics, error) {
	if registerer == nil {
		return nil, fmt.Errorf("runtime revision GC metrics require a registerer")
	}
	metrics := &runtimeRevisionGCMetrics{
		pending: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kagent_runtime_revision_gc_pending",
			Help: "Number of cleanup-eligible runtime revisions in the last successful discovery; NaN before discovery or while inactive.",
		}),
		failures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kagent_runtime_revision_gc_failures_total",
			Help: "Number of failed runtime revision GC attempts by stage, excluding controller cancellation.",
		}, []string{"stage"}),
	}
	metrics.pending.Set(math.NaN())
	for _, stage := range []runtimeRevisionGCStage{gcStageDiscovery, gcStageCollection} {
		metrics.failures.WithLabelValues(string(stage))
	}
	if err := registerer.Register(metrics.pending); err != nil {
		return nil, fmt.Errorf("register runtime revision GC pending metric: %w", err)
	}
	if err := registerer.Register(metrics.failures); err != nil {
		registerer.Unregister(metrics.pending)
		return nil, fmt.Errorf("register runtime revision GC failure metric: %w", err)
	}
	return metrics, nil
}

func (m *runtimeRevisionGCMetrics) recordFailure(stage runtimeRevisionGCStage) {
	m.failures.WithLabelValues(string(stage)).Inc()
}
