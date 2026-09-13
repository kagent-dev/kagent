package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"
)

const (
	gcPendingMetric  = "kagent_runtime_revision_gc_pending"
	gcAgeMetric      = "kagent_runtime_revision_gc_oldest_pending_age_seconds"
	gcObservedMetric = "kagent_runtime_revision_gc_last_successful_backlog_observation_timestamp_seconds"
	gcFailuresMetric = "kagent_runtime_revision_gc_failures_total"
)

type gcMetricSnapshot struct {
	gauges   map[string]float64
	failures map[string]float64
}

func gatherRuntimeRevisionGCMetrics(t *testing.T, registry *prometheus.Registry) gcMetricSnapshot {
	t.Helper()
	families, err := registry.Gather()
	require.NoError(t, err)
	result := gcMetricSnapshot{gauges: make(map[string]float64), failures: make(map[string]float64)}
	for _, family := range families {
		for _, metric := range family.GetMetric() {
			if family.GetName() == gcFailuresMetric {
				require.Len(t, metric.GetLabel(), 1)
				require.Equal(t, "stage", metric.GetLabel()[0].GetName())
				result.failures[metric.GetLabel()[0].GetValue()] = metric.GetCounter().GetValue()
			} else {
				require.Empty(t, metric.GetLabel(), "backlog metrics must not have revision or template labels")
				require.NotNil(t, metric.Gauge)
				result.gauges[family.GetName()] = metric.GetGauge().GetValue()
			}
		}
	}
	require.Len(t, result.failures, 7, "failure stages must have bounded cardinality")
	return result
}

func TestRuntimeRevisionGCMetricsObservationAndRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &fakeGCStore{
			revisions:    []database.RuntimeRevision{{Revision: "failed", ActorTemplateName: "failed"}},
			pendingSince: map[string]time.Time{"failed": time.Now().Add(-2 * time.Hour)},
		}
		templates := &fakeGCTemplates{deleteErr: errors.New("Substrate unavailable")}
		collector, registry := newTestRuntimeRevisionGC(t, store, templates)
		require.Empty(t, gatherRuntimeRevisionGCMetrics(t, registry).gauges, "standbys do not report an empty backlog")

		collector.metrics.setActive(true)
		snapshot := gatherRuntimeRevisionGCMetrics(t, registry)
		require.Equal(t, map[string]float64{gcObservedMetric: 0}, snapshot.gauges)
		collector.sweep(t.Context())
		snapshot = gatherRuntimeRevisionGCMetrics(t, registry)
		require.Equal(t, float64(1), snapshot.gauges[gcPendingMetric])
		require.Equal(t, float64(7200), snapshot.gauges[gcAgeMetric])
		require.Positive(t, snapshot.gauges[gcObservedMetric])
		require.Equal(t, float64(1), snapshot.failures[string(gcStageDeleteActorTemplate)])

		time.Sleep(30 * time.Second)
		store.observationErr = errors.New("database unavailable")
		collector.observeBacklog(t.Context())
		stale := gatherRuntimeRevisionGCMetrics(t, registry)
		require.Equal(t, float64(1), stale.gauges[gcPendingMetric])
		require.Equal(t, float64(7230), stale.gauges[gcAgeMetric])
		require.Equal(t, snapshot.gauges[gcObservedMetric], stale.gauges[gcObservedMetric])
		require.Equal(t, float64(1), stale.failures[string(gcStageBacklogObservation)])

		collector.metrics.setActive(false)
		require.Empty(t, gatherRuntimeRevisionGCMetrics(t, registry).gauges)
		restarted, restartedRegistry := newTestRuntimeRevisionGC(t, store, templates)
		restarted.metrics.setActive(true)
		restarted.observeBacklog(t.Context())
		require.Equal(t, map[string]float64{gcObservedMetric: 0}, gatherRuntimeRevisionGCMetrics(t, restartedRegistry).gauges,
			"a failed first read is unknown, not a zero-sized backlog")
		store.observationErr = nil
		restarted.observeBacklog(t.Context())
		recovered := gatherRuntimeRevisionGCMetrics(t, restartedRegistry)
		require.Equal(t, float64(1), recovered.gauges[gcPendingMetric])
		require.Equal(t, stale.gauges[gcAgeMetric], recovered.gauges[gcAgeMetric])
		require.Zero(t, recovered.failures[string(gcStageDeleteActorTemplate)], "failure counters reset in a new process")

		templates.deleteErr = nil
		restarted.sweep(t.Context())
		empty := gatherRuntimeRevisionGCMetrics(t, restartedRegistry)
		require.Contains(t, empty.gauges, gcPendingMetric)
		require.Contains(t, empty.gauges, gcAgeMetric)
		require.Zero(t, empty.gauges[gcPendingMetric])
		require.Zero(t, empty.gauges[gcAgeMetric])
	})
}

func TestRuntimeRevisionGCMetricsFailureStages(t *testing.T) {
	failure := errors.New("operation failed")
	for _, test := range []struct {
		name             string
		beginErr         error
		getErr           error
		deleteErr        error
		finalizeErr      error
		changedUID       bool
		skipClaim        bool
		skipFinalization bool
		cancelParent     bool
		wantStage        runtimeRevisionGCStage
		wantPending      float64
	}{
		{name: "begin deletion", beginErr: failure, wantStage: gcStageBeginDeletion, wantPending: 1},
		{name: "read ActorTemplate", getErr: failure, wantStage: gcStageGetActorTemplate, wantPending: 1},
		{name: "UID changed", changedUID: true, wantStage: gcStageUIDCheck, wantPending: 1},
		{name: "delete ActorTemplate", deleteErr: failure, wantStage: gcStageDeleteActorTemplate, wantPending: 1},
		{name: "finalize", finalizeErr: failure, wantStage: gcStageFinalize, wantPending: 1},
		{name: "nil claim is not a failure", skipClaim: true, wantPending: 1},
		{name: "nil finalization does not imply removal", skipFinalization: true, wantPending: 1},
		{name: "already absent compute", wantPending: 0},
		{name: "parent cancellation", beginErr: context.Canceled, cancelParent: true, wantPending: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeGCStore{
				revisions: []database.RuntimeRevision{{
					Revision: "failed", ActorTemplateName: "failed", ActorTemplateUID: "expected",
				}},
				beginErr: test.beginErr, finalizeErr: test.finalizeErr,
				skipClaim: test.skipClaim, skipFinalization: test.skipFinalization,
			}
			templates := &fakeGCTemplates{deleteErr: test.deleteErr}
			var client runtimeRevisionGCClient = templates
			if test.getErr != nil {
				client = &failingGCRead{err: test.getErr}
			}
			if test.changedUID {
				templates.template = &ateapipb.ActorTemplate{
					Metadata: &ateapipb.ResourceMetadata{Uid: "replacement"},
				}
			}
			collector, registry := newTestRuntimeRevisionGC(t, store, client)
			collector.metrics.setActive(true)
			collector.observeBacklog(t.Context())
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancelParent {
				cancel()
			}
			err := collector.collect(ctx, "failed")
			if test.wantStage != "" || test.cancelParent {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			collector.observeBacklog(t.Context())
			snapshot := gatherRuntimeRevisionGCMetrics(t, registry)
			require.Equal(t, test.wantPending, snapshot.gauges[gcPendingMetric])
			for stage, count := range snapshot.failures {
				if stage == string(test.wantStage) {
					require.Equal(t, float64(1), count)
				} else {
					require.Zero(t, count, "unexpected failure stage %s", stage)
				}
			}
		})
	}
}

func TestRuntimeRevisionGCMetricsObservationFailureDoesNotBlockCleanup(t *testing.T) {
	store := &fakeGCStore{
		revisions:      []database.RuntimeRevision{{Revision: "healthy"}},
		observationErr: errors.New("observation unavailable"),
	}
	collector, registry := newTestRuntimeRevisionGC(t, store, &fakeGCTemplates{})
	collector.metrics.setActive(true)
	collector.sweep(t.Context())
	require.Equal(t, []string{"healthy"}, store.deleted)
	snapshot := gatherRuntimeRevisionGCMetrics(t, registry)
	require.Equal(t, map[string]float64{gcObservedMetric: 0}, snapshot.gauges)
	require.Equal(t, float64(2), snapshot.failures[string(gcStageBacklogObservation)])
	require.Zero(t, snapshot.failures[string(gcStageDiscovery)])
	store.observationErr = nil
	collector.sweep(t.Context())
	require.Zero(t, gatherRuntimeRevisionGCMetrics(t, registry).gauges[gcPendingMetric])
}

func TestRuntimeRevisionGCMetricsRejectInvalidObservations(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		collector, registry := newTestRuntimeRevisionGC(t, &fakeGCStore{}, &fakeGCTemplates{})
		collector.metrics.setActive(true)
		since := time.Now().Add(-time.Hour)
		require.NoError(t, collector.metrics.observe(database.RuntimeRevisionCleanupBacklog{Pending: 1, OldestPendingSince: &since}))
		before := gatherRuntimeRevisionGCMetrics(t, registry)
		zero := time.Time{}
		for _, backlog := range []database.RuntimeRevisionCleanupBacklog{
			{Pending: -1},
			{Pending: 1},
			{OldestPendingSince: &since},
			{Pending: 1, OldestPendingSince: &zero},
		} {
			require.Error(t, collector.metrics.observe(backlog))
			require.Equal(t, before.gauges, gatherRuntimeRevisionGCMetrics(t, registry).gauges)
		}
		future := time.Now().Add(time.Minute)
		require.NoError(t, collector.metrics.observe(database.RuntimeRevisionCleanupBacklog{Pending: 1, OldestPendingSince: &future}))
		require.Zero(t, gatherRuntimeRevisionGCMetrics(t, registry).gauges[gcAgeMetric], "clock skew must not produce negative age")
	})
}

func TestRuntimeRevisionGCMetricsRegistrationAndScrape(t *testing.T) {
	store, templates := &fakeGCStore{}, &fakeGCTemplates{}
	collector, registry := newTestRuntimeRevisionGC(t, store, templates)
	_, err := NewRuntimeRevisionGC(store, templates, registry)
	require.Error(t, err, "a duplicate registration must not silently update detached metrics")
	_, err = NewRuntimeRevisionGC(store, templates, nil)
	require.Error(t, err)
	collector.metrics.setActive(true)
	collector.sweep(t.Context())
	response := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "# TYPE "+gcPendingMetric+" gauge")
	require.Contains(t, response.Body.String(), "# TYPE "+gcAgeMetric+" gauge")
	require.Contains(t, response.Body.String(), "# TYPE "+gcFailuresMetric+" counter")
	require.Contains(t, response.Body.String(), gcPendingMetric+" 0\n")
}

func TestRuntimeRevisionGCFailureLogsRetainIdentity(t *testing.T) {
	store := &fakeGCStore{revisions: []database.RuntimeRevision{{
		Revision: "revision-digest", ActorTemplateAtespace: "team-a", ActorTemplateName: "failed",
	}}}
	collector, registry := newTestRuntimeRevisionGC(t, store, &fakeGCTemplates{deleteErr: errors.New("backend unavailable")})
	var output bytes.Buffer
	ctx := logging.IntoContext(t.Context(), slog.New(slog.NewJSONHandler(&output, nil)))
	collector.sweep(ctx)
	var entry struct {
		Revision              string `json:"revision"`
		ActorTemplateAtespace string `json:"actor_template_atespace"`
		ActorTemplateName     string `json:"actor_template_name"`
		Error                 string `json:"error"`
	}
	require.NoError(t, json.NewDecoder(&output).Decode(&entry))
	require.Equal(t, "revision-digest", entry.Revision)
	require.Equal(t, "team-a", entry.ActorTemplateAtespace)
	require.Equal(t, "failed", entry.ActorTemplateName)
	require.Contains(t, entry.Error, "backend unavailable")
	require.Equal(t, float64(1), gatherRuntimeRevisionGCMetrics(t, registry).failures[string(gcStageDeleteActorTemplate)])
}

type failingGCRead struct {
	fakeActorTemplates
	err error
}

func (f *failingGCRead) GetActorTemplate(context.Context, string, string) (*ateapipb.ActorTemplate, error) {
	return nil, f.err
}
