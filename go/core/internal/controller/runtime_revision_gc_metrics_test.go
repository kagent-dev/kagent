package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"
)

const (
	gcPendingMetric  = "kagent_runtime_revision_gc_pending"
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
	require.Len(t, families, 2)
	result := gcMetricSnapshot{gauges: make(map[string]float64), failures: make(map[string]float64)}
	for _, family := range families {
		for _, metric := range family.GetMetric() {
			if family.GetName() == gcFailuresMetric {
				require.NotNil(t, metric.Counter)
				require.Len(t, metric.GetLabel(), 1)
				require.Equal(t, "stage", metric.GetLabel()[0].GetName())
				result.failures[metric.GetLabel()[0].GetValue()] = metric.GetCounter().GetValue()
			} else {
				require.Equal(t, gcPendingMetric, family.GetName())
				require.Empty(t, metric.GetLabel())
				require.NotNil(t, metric.Gauge)
				result.gauges[family.GetName()] = metric.GetGauge().GetValue()
			}
		}
	}
	require.Len(t, result.gauges, 1)
	require.Len(t, result.failures, 2)
	require.Contains(t, result.failures, string(gcStageDiscovery))
	require.Contains(t, result.failures, string(gcStageCollection))
	return result
}

func TestRuntimeRevisionGCMetricsDiscoveryAndRestart(t *testing.T) {
	store := &fakeGCStore{revisions: []database.RuntimeRevision{{Revision: "failed", ActorTemplateName: "failed"}}}
	templates := &fakeGCTemplates{deleteErr: errors.New("Substrate unavailable")}
	collector, registry := newTestRuntimeRevisionGC(t, store, templates)
	require.True(t, math.IsNaN(gatherRuntimeRevisionGCMetrics(t, registry).gauges[gcPendingMetric]))
	require.Zero(t, store.lists, "standbys and scrapes must not discover")
	for range 2 {
		collector.sweep(t.Context())
	}
	snapshot := gatherRuntimeRevisionGCMetrics(t, registry)
	require.Equal(t, float64(1), snapshot.gauges[gcPendingMetric])
	require.Equal(t, float64(2), snapshot.failures[string(gcStageCollection)])

	restarted, restartedRegistry := newTestRuntimeRevisionGC(t, store, templates)
	store.listErr = errors.New("database unavailable")
	restarted.sweep(t.Context())
	require.True(t, math.IsNaN(gatherRuntimeRevisionGCMetrics(t, restartedRegistry).gauges[gcPendingMetric]),
		"a failed initial read is unknown, not an empty backlog")
	store.listErr = nil
	_, err := restarted.discover(t.Context())
	require.NoError(t, err)
	recovered := gatherRuntimeRevisionGCMetrics(t, restartedRegistry)
	require.Equal(t, float64(1), recovered.gauges[gcPendingMetric])
	require.Zero(t, recovered.failures[string(gcStageCollection)], "process counters reset")
	templates.deleteErr = nil
	restarted.sweep(t.Context())
	require.Zero(t, gatherRuntimeRevisionGCMetrics(t, restartedRegistry).gauges[gcPendingMetric])
}

func TestRuntimeRevisionGCMetricsBoundedDiscovery(t *testing.T) {
	for _, count := range []int{0, 1, 100} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			store := &fakeGCStore{}
			for i := range count {
				store.revisions = append(store.revisions, database.RuntimeRevision{Revision: fmt.Sprint(i)})
			}
			collector, registry := newTestRuntimeRevisionGC(t, store, &fakeGCTemplates{})
			collector.sweep(t.Context())
			require.Equal(t, 2, store.lists, "discovery must not scale with candidate count, even for an empty sweep")
			require.Len(t, store.deleted, count)
			require.Zero(t, gatherRuntimeRevisionGCMetrics(t, registry).gauges[gcPendingMetric])
			require.Equal(t, 2, store.lists, "scrapes must not access the store")
		})
	}
}

func TestRuntimeRevisionGCMetricsDiscoveryErrors(t *testing.T) {
	for _, atEnd := range []bool{false, true} {
		for _, mode := range []string{"error with partial results", "deadline", "parent cancellation", "canceled successful read"} {
			t.Run(fmt.Sprintf("end=%t/%s", atEnd, mode), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					failAt := 1
					if atEnd {
						failAt = 2
					}
					revisions := []database.RuntimeRevision{{Revision: "candidate"}}
					store := &fakeGCStore{revisions: revisions}
					store.listFunc = func(listCtx context.Context, call int) ([]database.RuntimeRevision, error) {
						if call != failAt {
							return revisions, nil
						}
						switch mode {
						case "deadline":
							<-listCtx.Done()
							return nil, listCtx.Err()
						case "parent cancellation":
							cancel()
							return nil, listCtx.Err()
						case "canceled successful read":
							cancel()
							return nil, nil
						default:
							return revisions, errors.New("incomplete discovery")
						}
					}
					collector, registry := newTestRuntimeRevisionGC(t, store, &fakeGCTemplates{})
					collector.metrics.pending.Set(9)
					collector.sweep(ctx)
					snapshot := gatherRuntimeRevisionGCMetrics(t, registry)
					wantPending, wantCollected := float64(9), 0
					if atEnd {
						wantPending, wantCollected = 1, 1
					}
					require.Equal(t, failAt, store.lists)
					require.Len(t, store.begun, wantCollected)
					require.Equal(t, wantPending, snapshot.gauges[gcPendingMetric])
					wantFailures := float64(1)
					if ctx.Err() != nil {
						wantFailures = 0
					}
					require.Equal(t, wantFailures, snapshot.failures[string(gcStageDiscovery)])
					require.Zero(t, snapshot.failures[string(gcStageCollection)])
				})
			})
		}
	}
}

func TestRuntimeRevisionGCMetricsCanceledSweep(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	store := &fakeGCStore{}
	collector, registry := newTestRuntimeRevisionGC(t, store, &fakeGCTemplates{})
	collector.sweep(ctx)
	require.Zero(t, store.lists)
	require.True(t, math.IsNaN(gatherRuntimeRevisionGCMetrics(t, registry).gauges[gcPendingMetric]))
}

func TestRuntimeRevisionGCMetricsCancellationStopsDispatch(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "successful deletion"},
		{name: "failed deletion", err: errors.New("deletion failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			store := &fakeGCStore{revisions: []database.RuntimeRevision{
				{Revision: "first"},
				{Revision: "next"},
			}}
			templates := &cancelingGCDeletion{cancel: cancel, err: test.err}
			collector, registry := newTestRuntimeRevisionGC(t, store, templates)
			collector.sweep(ctx)
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.Equal(t, []string{"first"}, store.begun, "cancellation must stop later candidate dispatch")
			require.Equal(t, 1, store.lists, "cancellation must prevent the end-of-sweep query")
			snapshot := gatherRuntimeRevisionGCMetrics(t, registry)
			require.Equal(t, float64(2), snapshot.gauges[gcPendingMetric], "retain the last successful discovery")
			require.Zero(t, snapshot.failures[string(gcStageDiscovery)])
			require.Zero(t, snapshot.failures[string(gcStageCollection)])
		})
	}
}

func TestRuntimeRevisionGCMetricsRefreshDoesNotCollectNewCandidates(t *testing.T) {
	store := &fakeGCStore{
		revisions: []database.RuntimeRevision{{Revision: "old"}, {Revision: "new"}},
		listFunc: func(_ context.Context, call int) ([]database.RuntimeRevision, error) {
			if call == 1 {
				return []database.RuntimeRevision{{Revision: "old"}}, nil
			}
			return []database.RuntimeRevision{{Revision: "new"}}, nil
		},
	}
	collector, registry := newTestRuntimeRevisionGC(t, store, &fakeGCTemplates{})
	collector.sweep(t.Context())
	require.Equal(t, []string{"old"}, store.begun)
	require.Equal(t, float64(1), gatherRuntimeRevisionGCMetrics(t, registry).gauges[gcPendingMetric])
}

func TestRuntimeRevisionGCMetricsCollectionFailures(t *testing.T) {
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
		wantFailure      bool
		wantPending      float64
	}{
		{name: "begin deletion", beginErr: failure, wantFailure: true, wantPending: 1},
		{name: "read ActorTemplate", getErr: failure, wantFailure: true, wantPending: 1},
		{name: "UID changed", changedUID: true, wantFailure: true, wantPending: 1},
		{name: "delete ActorTemplate", deleteErr: failure, wantFailure: true, wantPending: 1},
		{name: "finalize", finalizeErr: failure, wantFailure: true, wantPending: 1},
		{name: "nil claim", skipClaim: true, wantPending: 1},
		{name: "nil finalization", skipFinalization: true, wantPending: 1},
		{name: "already absent compute", wantPending: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeGCStore{
				revisions: []database.RuntimeRevision{{Revision: "failed", ActorTemplateName: "failed", ActorTemplateUID: "expected"}},
				beginErr:  test.beginErr, finalizeErr: test.finalizeErr,
				skipClaim: test.skipClaim, skipFinalization: test.skipFinalization,
			}
			templates := &fakeGCTemplates{deleteErr: test.deleteErr}
			var client runtimeRevisionGCClient = templates
			if test.getErr != nil {
				client = &failingGCRead{err: test.getErr}
			}
			if test.changedUID {
				templates.template = &ateapipb.ActorTemplate{Metadata: &ateapipb.ResourceMetadata{Uid: "replacement"}}
			}
			collector, registry := newTestRuntimeRevisionGC(t, store, client)
			collector.sweep(t.Context())
			snapshot := gatherRuntimeRevisionGCMetrics(t, registry)
			require.Equal(t, test.wantPending, snapshot.gauges[gcPendingMetric])
			wantFailures := float64(0)
			if test.wantFailure {
				wantFailures = 1
			}
			require.Equal(t, wantFailures, snapshot.failures[string(gcStageCollection)])
			require.Zero(t, snapshot.failures[string(gcStageDiscovery)])
		})
	}
}

func TestRuntimeRevisionGCMetricsRegistrationAndScrape(t *testing.T) {
	store, templates := &fakeGCStore{}, &fakeGCTemplates{}
	collector, registry := newTestRuntimeRevisionGC(t, store, templates)
	_, err := NewRuntimeRevisionGC(store, templates, registry)
	require.Error(t, err)
	_, err = NewRuntimeRevisionGC(store, templates, nil)
	require.Error(t, err)
	collector.sweep(t.Context())
	response := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "# TYPE "+gcPendingMetric+" gauge")
	require.Contains(t, response.Body.String(), "# TYPE "+gcFailuresMetric+" counter")
	require.Contains(t, response.Body.String(), gcPendingMetric+" 0\n")
	require.Equal(t, 2, store.lists)

	conflict := prometheus.NewRegistry()
	require.NoError(t, conflict.Register(collector.metrics.failures))
	_, err = NewRuntimeRevisionGC(store, templates, conflict)
	require.Error(t, err)
	families, err := conflict.Gather()
	require.NoError(t, err)
	require.Len(t, families, 1, "a failed constructor must not leave its pending gauge registered")
	require.Equal(t, gcFailuresMetric, families[0].GetName())
	require.True(t, conflict.Unregister(collector.metrics.failures))
	_, err = NewRuntimeRevisionGC(store, templates, conflict)
	require.NoError(t, err)
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
	require.Equal(t, float64(1), gatherRuntimeRevisionGCMetrics(t, registry).failures[string(gcStageCollection)])
}

type failingGCRead struct {
	fakeActorTemplates
	err error
}

func (f *failingGCRead) GetActorTemplate(context.Context, string, string) (*ateapipb.ActorTemplate, error) {
	return nil, f.err
}

type cancelingGCDeletion struct {
	fakeActorTemplates
	cancel context.CancelFunc
	err    error
}

var _ runtimeRevisionGCClient = (*cancelingGCDeletion)(nil)

func (c *cancelingGCDeletion) DeleteActorTemplate(context.Context, string, string, string) error {
	c.cancel()
	return c.err
}
