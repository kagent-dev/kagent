package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/telemetrytest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const (
	gcPendingMetric  = "kagent.runtime_revision.gc.pending"
	gcFailuresMetric = "kagent.runtime_revision.gc.failures"
)

type gcMetricSnapshot struct {
	gauges   map[string]int64
	failures map[string]int64
}

func gatherRuntimeRevisionGCMetrics(t *testing.T, reader *sdkmetric.ManualReader) gcMetricSnapshot {
	t.Helper()
	data := telemetrytest.Collect(t, reader)
	result := gcMetricSnapshot{gauges: make(map[string]int64), failures: make(map[string]int64)}
	for _, scope := range data.ScopeMetrics {
		for _, sample := range scope.Metrics {
			shape, found := telemetrytest.FindMetric(data, sample.Name)
			require.True(t, found)
			switch sample.Name {
			case gcPendingMetric:
				require.Equal(t, telemetrytest.MetricShape{Name: gcPendingMetric, Kind: "gauge", Unit: "{revision}"}, shape)
				gauge, ok := sample.Data.(metricdata.Gauge[int64])
				require.True(t, ok, "pending must be an integer gauge")
				require.Len(t, gauge.DataPoints, 1)
				require.Zero(t, gauge.DataPoints[0].Attributes.Len())
				result.gauges[sample.Name] = gauge.DataPoints[0].Value
			case gcFailuresMetric:
				require.Equal(t, telemetrytest.MetricShape{
					Name: gcFailuresMetric, Kind: "sum", Unit: "{failure}", AttributeKeys: []string{"kagent.gc.stage"},
				}, shape)
				sum, ok := sample.Data.(metricdata.Sum[int64])
				require.True(t, ok, "failures must be an integer counter")
				require.True(t, sum.IsMonotonic)
				require.Equal(t, metricdata.CumulativeTemporality, sum.Temporality)
				require.Len(t, sum.DataPoints, 2)
				for _, point := range sum.DataPoints {
					require.Equal(t, 1, point.Attributes.Len())
					stage, found := point.Attributes.Value(attribute.Key("kagent.gc.stage"))
					require.True(t, found)
					result.failures[stage.AsString()] = point.Value
				}
			default:
				t.Fatalf("unexpected GC metric %q", sample.Name)
			}
		}
	}
	require.Len(t, result.failures, 2)
	require.Contains(t, result.failures, string(gcStageDiscovery))
	require.Contains(t, result.failures, string(gcStageCollection))
	return result
}

func TestRuntimeRevisionGCMetricsDiscoveryAndRestart(t *testing.T) {
	store := &fakeGCStore{revisions: []database.RuntimeRevision{{Revision: "failed", ActorTemplateName: "failed"}}}
	templates := &fakeGCTemplates{deleteErr: errors.New("Substrate unavailable")}
	collector, registry := newTestRuntimeRevisionGC(t, store, templates)
	require.NotContains(t, gatherRuntimeRevisionGCMetrics(t, registry).gauges, gcPendingMetric)
	require.Zero(t, store.lists, "standbys and scrapes must not discover")
	for range 2 {
		collector.sweep(t.Context())
	}
	snapshot := gatherRuntimeRevisionGCMetrics(t, registry)
	require.Equal(t, int64(1), snapshot.gauges[gcPendingMetric])
	require.Equal(t, int64(2), snapshot.failures[string(gcStageCollection)])

	store.listErr = errors.New("database unavailable")
	for range 3 {
		collector.sweep(t.Context())
		snapshot = gatherRuntimeRevisionGCMetrics(t, registry)
		require.Equal(t, int64(1), snapshot.gauges[gcPendingMetric], "failed discovery must retain the last successful count")
	}
	require.Equal(t, int64(3), snapshot.failures[string(gcStageDiscovery)])
	require.Equal(t, int64(2), snapshot.failures[string(gcStageCollection)])

	restarted, restartedRegistry := newTestRuntimeRevisionGC(t, store, templates)
	restarted.sweep(t.Context())
	require.NotContains(t, gatherRuntimeRevisionGCMetrics(t, restartedRegistry).gauges, gcPendingMetric,
		"a failed initial read is unknown, not an empty backlog")
	store.listErr = nil
	_, err := restarted.discover(t.Context())
	require.NoError(t, err)
	recovered := gatherRuntimeRevisionGCMetrics(t, restartedRegistry)
	require.Equal(t, int64(1), recovered.gauges[gcPendingMetric])
	require.Zero(t, recovered.failures[string(gcStageCollection)], "process counters reset")
	templates.deleteErr = nil
	restarted.sweep(t.Context())
	require.Equal(t, map[string]int64{gcPendingMetric: 0}, gatherRuntimeRevisionGCMetrics(t, restartedRegistry).gauges)
}

func TestRuntimeRevisionGCMetricsConcurrentReaders(t *testing.T) {
	first, second := sdkmetric.NewManualReader(), sdkmetric.NewManualReader()
	collector, err := NewRuntimeRevisionGC(&fakeGCStore{}, &fakeGCTemplates{}, newGCTestMeterProvider(t, first, second))
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for count := range int64(100) {
			collector.metrics.recordPending(count)
		}
	}()
	for range 100 {
		gatherRuntimeRevisionGCMetrics(t, first)
		gatherRuntimeRevisionGCMetrics(t, second)
	}
	<-done
	for _, reader := range []*sdkmetric.ManualReader{first, second} {
		require.Equal(t, int64(99), gatherRuntimeRevisionGCMetrics(t, reader).gauges[gcPendingMetric],
			"collection by one reader must not consume another reader's cached count")
	}
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
			require.Equal(t, map[string]int64{gcPendingMetric: 0}, gatherRuntimeRevisionGCMetrics(t, registry).gauges)
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
					collector.metrics.recordPending(9)
					collector.sweep(ctx)
					snapshot := gatherRuntimeRevisionGCMetrics(t, registry)
					wantPending, wantCollected := int64(9), 0
					if atEnd {
						wantPending, wantCollected = 1, 1
					}
					require.Equal(t, failAt, store.lists)
					require.Len(t, store.begun, wantCollected)
					require.Equal(t, wantPending, snapshot.gauges[gcPendingMetric])
					wantFailures := int64(1)
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
	require.NotContains(t, gatherRuntimeRevisionGCMetrics(t, registry).gauges, gcPendingMetric)
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
			require.Equal(t, int64(2), snapshot.gauges[gcPendingMetric], "retain the last successful discovery")
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
	require.Equal(t, int64(1), gatherRuntimeRevisionGCMetrics(t, registry).gauges[gcPendingMetric])
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
		wantPending      int64
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
			require.Contains(t, snapshot.gauges, gcPendingMetric)
			require.Equal(t, test.wantPending, snapshot.gauges[gcPendingMetric])
			wantFailures := int64(0)
			if test.wantFailure {
				wantFailures = 1
			}
			require.Equal(t, wantFailures, snapshot.failures[string(gcStageCollection)])
			require.Zero(t, snapshot.failures[string(gcStageDiscovery)])
		})
	}
}

func TestRuntimeRevisionGCMetricsShapeAndScrape(t *testing.T) {
	store, templates := &fakeGCStore{}, &fakeGCTemplates{}
	registry := prometheus.NewRegistry()
	reader := sdkmetric.NewManualReader()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	require.NoError(t, err)
	provider := newGCTestMeterProvider(t, reader, exporter)
	collector, err := NewRuntimeRevisionGC(store, templates, provider)
	require.NoError(t, err)
	require.NotContains(t, gatherRuntimeRevisionGCMetrics(t, reader).gauges, gcPendingMetric)
	collector.sweep(t.Context())
	require.Equal(t, map[string]int64{gcPendingMetric: 0}, gatherRuntimeRevisionGCMetrics(t, reader).gauges)
	collector.metrics.recordFailure(t.Context(), gcStageDiscovery)
	response := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "# TYPE kagent_runtime_revision_gc_pending gauge")
	require.Contains(t, response.Body.String(), "# TYPE kagent_runtime_revision_gc_failures_total counter")
	families, err := registry.Gather()
	require.NoError(t, err)
	var pendingFound, failuresFound bool
	for _, family := range families {
		switch family.GetName() {
		case "kagent_runtime_revision_gc_pending":
			pendingFound = true
			require.Len(t, family.Metric, 1)
			require.Zero(t, family.Metric[0].GetGauge().GetValue())
		case "kagent_runtime_revision_gc_failures_total":
			failuresFound = true
			require.Len(t, family.Metric, 2)
			stages := make(map[string]float64)
			for _, point := range family.Metric {
				for _, label := range point.Label {
					require.NotEqual(t, "stage", label.GetName())
					if label.GetName() == "kagent_gc_stage" {
						stages[label.GetValue()] = point.GetCounter().GetValue()
					}
				}
			}
			require.Equal(t, map[string]float64{"discovery": 1, "collection": 0}, stages)
		}
	}
	require.True(t, pendingFound)
	require.True(t, failuresFound)
	require.Equal(t, 2, store.lists)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.NoError(t, collector.Start(ctx))
	require.NotContains(t, gatherRuntimeRevisionGCMetrics(t, reader).gauges, gcPendingMetric)
	families, err = registry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		require.NotEqual(t, "kagent_runtime_revision_gc_pending", family.GetName(), "stopped GC must withdraw its last count")
	}
	require.Equal(t, 2, store.lists, "metric readers and canceled startup must not discover")
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
	require.Equal(t, int64(1), gatherRuntimeRevisionGCMetrics(t, registry).failures[string(gcStageCollection)])
}

func TestRuntimeRevisionGCMetricsConstructionErrors(t *testing.T) {
	_, err := NewRuntimeRevisionGC(&fakeGCStore{}, &fakeGCTemplates{}, nil)
	require.ErrorContains(t, err, "meter provider")
	for _, stage := range []string{"pending", "failures", "callback"} {
		t.Run(stage, func(t *testing.T) {
			reader := sdkmetric.NewManualReader()
			failure := errors.New("instrument creation failed")
			provider := failingGCMeterProvider{
				MeterProvider: newGCTestMeterProvider(t, reader), stage: stage, err: failure,
			}
			_, err := NewRuntimeRevisionGC(&fakeGCStore{}, &fakeGCTemplates{}, provider)
			require.ErrorIs(t, err, failure)
			data := telemetrytest.Collect(t, reader)
			for _, name := range []string{gcPendingMetric, gcFailuresMetric} {
				_, found := telemetrytest.FindMetric(data, name)
				require.False(t, found, "failed construction must not publish measurements")
			}
		})
	}
}

func TestRuntimeRevisionGCMetricsDisabled(t *testing.T) {
	store := &fakeGCStore{revisions: []database.RuntimeRevision{{Revision: "candidate"}}}
	collector, err := NewRuntimeRevisionGC(store, &fakeGCTemplates{}, noop.NewMeterProvider())
	require.NoError(t, err)
	collector.sweep(t.Context())
	require.Equal(t, 2, store.lists)
	require.Equal(t, []string{"candidate"}, store.deleted)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.NoError(t, collector.Start(ctx))
}

func TestRuntimeRevisionGCMetricsUnregisterFailure(t *testing.T) {
	collector, reader := newTestRuntimeRevisionGC(t, &fakeGCStore{}, &fakeGCTemplates{})
	collector.metrics.recordPending(7)
	failure := errors.New("unregister failed")
	collector.metrics.registration = failingGCRegistration{Registration: collector.metrics.registration, err: failure}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, collector.Start(ctx), failure)
	snapshot := gatherRuntimeRevisionGCMetrics(t, reader)
	require.NotContains(t, snapshot.gauges, gcPendingMetric)
	require.Zero(t, snapshot.failures[string(gcStageDiscovery)])
	require.Zero(t, snapshot.failures[string(gcStageCollection)])
}

type failingGCMeterProvider struct {
	metric.MeterProvider
	stage string
	err   error
}

func (p failingGCMeterProvider) Meter(name string, options ...metric.MeterOption) metric.Meter {
	return failingGCMeter{Meter: p.MeterProvider.Meter(name, options...), stage: p.stage, err: p.err}
}

type failingGCMeter struct {
	metric.Meter
	stage string
	err   error
}

func (m failingGCMeter) Int64ObservableGauge(name string, options ...metric.Int64ObservableGaugeOption) (metric.Int64ObservableGauge, error) {
	if m.stage == "pending" {
		return nil, m.err
	}
	return m.Meter.Int64ObservableGauge(name, options...)
}

func (m failingGCMeter) Int64Counter(name string, options ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if m.stage == "failures" {
		return nil, m.err
	}
	return m.Meter.Int64Counter(name, options...)
}

func (m failingGCMeter) RegisterCallback(callback metric.Callback, instruments ...metric.Observable) (metric.Registration, error) {
	if m.stage == "callback" {
		return nil, m.err
	}
	return m.Meter.RegisterCallback(callback, instruments...)
}

type failingGCRegistration struct {
	metric.Registration
	err error
}

func (r failingGCRegistration) Unregister() error { return r.err }

var (
	_ metric.MeterProvider = failingGCMeterProvider{}
	_ metric.Meter         = failingGCMeter{}
	_ metric.Registration  = failingGCRegistration{}
)

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

func (c *cancelingGCDeletion) DeleteActorTemplate(context.Context, string, string) error {
	c.cancel()
	return c.err
}
