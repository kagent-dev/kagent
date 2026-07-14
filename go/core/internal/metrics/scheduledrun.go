package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// ScheduledRun metrics use only bounded lifecycle labels. Resource identity is
// available from logs and Kubernetes status without creating unbounded
// Prometheus series as ScheduledRuns are created and deleted.
var (
	scheduledRunDispatchTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kagent_scheduledrun_dispatch_total",
			Help: "Total number of ScheduledRun dispatch attempts, labelled by dispatch status.",
		},
		[]string{"status"},
	)
	scheduledRunOutcomeTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kagent_scheduledrun_outcome_total",
			Help: "Total number of ScheduledRun outcomes resolved by asynchronous task polling.",
		},
		[]string{"outcome"},
	)
	scheduledRunDispatchDurationSeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "kagent_scheduledrun_dispatch_duration_seconds",
			Help:    "Duration of the synchronous A2A dispatch call.",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
		},
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
func ObserveScheduledRunDispatch(status string, durationSeconds float64) {
	scheduledRunDispatchTotal.WithLabelValues(status).Inc()
	scheduledRunDispatchDurationSeconds.Observe(durationSeconds)
}

// ObserveScheduledRunOutcome records an asynchronously resolved outcome.
func ObserveScheduledRunOutcome(outcome string) {
	scheduledRunOutcomeTotal.WithLabelValues(outcome).Inc()
}

// SetActiveSchedules updates the gauge of active cron entries.
func SetActiveSchedules(n int) {
	scheduledRunActiveSchedules.Set(float64(n))
}
