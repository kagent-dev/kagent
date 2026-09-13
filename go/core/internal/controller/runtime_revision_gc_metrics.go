package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/prometheus/client_golang/prometheus"
)

type runtimeRevisionGCStage string

const (
	gcStageDiscovery           runtimeRevisionGCStage = "discovery"
	gcStageBacklogObservation  runtimeRevisionGCStage = "backlog_observation"
	gcStageBeginDeletion       runtimeRevisionGCStage = "begin_deletion"
	gcStageGetActorTemplate    runtimeRevisionGCStage = "get_actor_template"
	gcStageUIDCheck            runtimeRevisionGCStage = "uid_check"
	gcStageDeleteActorTemplate runtimeRevisionGCStage = "delete_actor_template"
	gcStageFinalize            runtimeRevisionGCStage = "finalize"
)

type runtimeRevisionGCMetrics struct {
	pendingDesc  *prometheus.Desc
	ageDesc      *prometheus.Desc
	observedDesc *prometheus.Desc
	failures     *prometheus.CounterVec

	mu         sync.RWMutex
	active     bool
	pending    int64
	oldest     time.Time
	observedAt time.Time
}

var _ prometheus.Collector = (*runtimeRevisionGCMetrics)(nil)

func newRuntimeRevisionGCMetrics(registerer prometheus.Registerer) (*runtimeRevisionGCMetrics, error) {
	if registerer == nil {
		return nil, fmt.Errorf("runtime revision GC metrics require a registerer")
	}
	metrics := &runtimeRevisionGCMetrics{
		pendingDesc: prometheus.NewDesc(
			"kagent_runtime_revision_gc_pending",
			"Number of cleanup-eligible runtime revisions in the last successful backlog observation.",
			nil, nil,
		),
		ageDesc: prometheus.NewDesc(
			"kagent_runtime_revision_gc_oldest_pending_age_seconds",
			"Seconds since the oldest pending cleanup was first durably observed; zero for an observed empty backlog.",
			nil, nil,
		),
		observedDesc: prometheus.NewDesc(
			"kagent_runtime_revision_gc_last_successful_backlog_observation_timestamp_seconds",
			"Unix timestamp of the last successful backlog observation by the active collector; zero before its first observation.",
			nil, nil,
		),
		failures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kagent_runtime_revision_gc_failures_total",
			Help: "Number of failed runtime revision GC operations by stage, excluding controller cancellation.",
		}, []string{"stage"}),
	}
	for _, stage := range []runtimeRevisionGCStage{
		gcStageDiscovery, gcStageBacklogObservation, gcStageBeginDeletion,
		gcStageGetActorTemplate, gcStageUIDCheck, gcStageDeleteActorTemplate, gcStageFinalize,
	} {
		metrics.failures.WithLabelValues(string(stage))
	}
	if err := registerer.Register(metrics); err != nil {
		return nil, fmt.Errorf("register runtime revision GC metrics: %w", err)
	}
	return metrics, nil
}

func (m *runtimeRevisionGCMetrics) Describe(ch chan<- *prometheus.Desc) {
	ch <- m.pendingDesc
	ch <- m.ageDesc
	ch <- m.observedDesc
	m.failures.Describe(ch)
}

func (m *runtimeRevisionGCMetrics) Collect(ch chan<- prometheus.Metric) {
	m.failures.Collect(ch)
	m.mu.RLock()
	active, pending, oldest, observedAt := m.active, m.pending, m.oldest, m.observedAt
	m.mu.RUnlock()
	if !active {
		return
	}
	var timestamp float64
	if !observedAt.IsZero() {
		timestamp = float64(observedAt.UnixNano()) / float64(time.Second)
	}
	ch <- prometheus.MustNewConstMetric(m.observedDesc, prometheus.GaugeValue, timestamp)
	if observedAt.IsZero() {
		return
	}
	var age float64
	if pending > 0 {
		age = max(0, time.Since(oldest).Seconds())
	}
	ch <- prometheus.MustNewConstMetric(m.pendingDesc, prometheus.GaugeValue, float64(pending))
	ch <- prometheus.MustNewConstMetric(m.ageDesc, prometheus.GaugeValue, age)
}

func (m *runtimeRevisionGCMetrics) setActive(active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active = active
	m.pending = 0
	m.oldest = time.Time{}
	m.observedAt = time.Time{}
}

func (m *runtimeRevisionGCMetrics) observe(backlog database.RuntimeRevisionCleanupBacklog) error {
	if backlog.Pending < 0 || (backlog.Pending == 0) != (backlog.OldestPendingSince == nil) ||
		(backlog.OldestPendingSince != nil && backlog.OldestPendingSince.IsZero()) {
		return fmt.Errorf("invalid runtime revision cleanup backlog observation")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = backlog.Pending
	m.oldest = time.Time{}
	if backlog.OldestPendingSince != nil {
		m.oldest = *backlog.OldestPendingSince
	}
	m.observedAt = time.Now()
	return nil
}

func (m *runtimeRevisionGCMetrics) recordFailure(ctx context.Context, stage runtimeRevisionGCStage) {
	if ctx.Err() == nil {
		m.failures.WithLabelValues(string(stage)).Inc()
	}
}
