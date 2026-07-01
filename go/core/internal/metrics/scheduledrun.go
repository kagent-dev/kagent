package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// ScheduledRun metrics. The label cardinality is bounded by the number of
// ScheduledRun resources, which is operator-controlled and typically small.
var (
	scheduledRunDispatchTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kagent_scheduledrun_dispatch_total",
			Help: "Total number of ScheduledRun dispatch attempts, labelled by dispatch status.",
		},
		[]string{"namespace", "name", "status"},
	)
	scheduledRunOutcomeTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kagent_scheduledrun_outcome_total",
			Help: "Total number of ScheduledRun resolved outcomes (post async session-state polling).",
		},
		[]string{"namespace", "name", "outcome"},
	)
	scheduledRunDispatchDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kagent_scheduledrun_dispatch_duration_seconds",
			Help:    "Duration of the synchronous A2A dispatch call.",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
		},
		[]string{"namespace", "name"},
	)
	scheduledRunActiveSchedules = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "kagent_scheduledrun_active_schedules",
			Help: "Number of ScheduledRun cron entries currently scheduled (excludes suspended).",
		},
	)
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		scheduledRunDispatchTotal,
		scheduledRunOutcomeTotal,
		scheduledRunDispatchDurationSeconds,
		scheduledRunActiveSchedules,
	)
}

// ObserveScheduledRunDispatch records a dispatch attempt and its duration.
func ObserveScheduledRunDispatch(namespace, name, status string, durationSeconds float64) {
	scheduledRunDispatchTotal.WithLabelValues(namespace, name, status).Inc()
	scheduledRunDispatchDurationSeconds.WithLabelValues(namespace, name).Observe(durationSeconds)
}

// ObserveScheduledRunOutcome records a resolved outcome (post-polling).
func ObserveScheduledRunOutcome(namespace, name, outcome string) {
	scheduledRunOutcomeTotal.WithLabelValues(namespace, name, outcome).Inc()
}

// SetActiveSchedules updates the gauge of active cron entries.
func SetActiveSchedules(n int) {
	scheduledRunActiveSchedules.Set(float64(n))
}
