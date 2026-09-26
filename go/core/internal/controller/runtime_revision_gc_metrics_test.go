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
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/telemetrytest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	gcPendingMetric  = "kagent.runtime_revision.gc.pending"
	gcDurationMetric = "kagent.runtime_revision.gc.duration"
)

type gcMetricOutcome struct {
	stage     string
	errorType string
}

type gcMetricSnapshot struct {
	gauges   map[string]int64
	failures map[string]int64
	attempts map[string]uint64
	duration map[gcMetricOutcome]metricdata.HistogramDataPoint[float64]
}

func gatherRuntimeRevisionGCMetrics(t *testing.T, reader *sdkmetric.ManualReader) gcMetricSnapshot {
	t.Helper()
	data := telemetrytest.Collect(t, reader)
	result := gcMetricSnapshot{
		gauges: make(map[string]int64), failures: make(map[string]int64),
		attempts: make(map[string]uint64), duration: make(map[gcMetricOutcome]metricdata.HistogramDataPoint[float64]),
	}
	for _, scope := range data.ScopeMetrics {
		require.Equal(t, "github.com/kagent-dev/kagent/go/core/internal/controller", scope.Scope.Name)
		require.Equal(t, version.Version, scope.Scope.Version)
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
			case gcDurationMetric:
				histogram, ok := sample.Data.(metricdata.Histogram[float64])
				require.True(t, ok, "duration must be a floating-point histogram")
				require.Equal(t, metricdata.CumulativeTemporality, histogram.Temporality)
				keys := []string{"kagent.gc.stage"}
				for _, point := range histogram.DataPoints {
					stage, found := point.Attributes.Value(attribute.Key("kagent.gc.stage"))
					require.True(t, found)
					require.Contains(t, []string{"discovery", "collection"}, stage.AsString())
					outcome := gcMetricOutcome{stage: stage.AsString()}
					if errorType, failed := point.Attributes.Value(attribute.Key("error.type")); failed {
						keys = []string{"error.type", "kagent.gc.stage"}
						require.Equal(t, 2, point.Attributes.Len())
						require.Contains(t, []string{"_OTHER", "Canceled", "Unknown", "InvalidArgument", "DeadlineExceeded",
							"NotFound", "AlreadyExists", "PermissionDenied", "ResourceExhausted", "FailedPrecondition",
							"Aborted", "OutOfRange", "Unimplemented", "Internal", "Unavailable", "DataLoss", "Unauthenticated"}, errorType.AsString())
						outcome.errorType = errorType.AsString()
						result.failures[outcome.stage] += int64(point.Count)
					} else {
						require.Equal(t, 1, point.Attributes.Len(), "success must omit error.type")
					}
					require.NotContains(t, result.duration, outcome, "one series per stage and outcome")
					require.Equal(t, []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}, point.Bounds)
					require.Len(t, point.BucketCounts, len(point.Bounds)+1)
					var count uint64
					for _, bucket := range point.BucketCounts {
						count += bucket
					}
					require.Equal(t, point.Count, count)
					require.GreaterOrEqual(t, point.Sum, float64(0))
					result.attempts[outcome.stage] += point.Count
					result.duration[outcome] = point
				}
				require.Equal(t, telemetrytest.MetricShape{
					Name: gcDurationMetric, Kind: "histogram", Unit: "s", AttributeKeys: keys,
				}, shape)
			default:
				t.Fatalf("unexpected GC metric %q", sample.Name)
			}
		}
	}
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
			snapshot := gatherRuntimeRevisionGCMetrics(t, registry)
			require.Equal(t, map[string]int64{gcPendingMetric: 0}, snapshot.gauges)
			require.Equal(t, uint64(2), snapshot.attempts["discovery"])
			require.Equal(t, uint64(count), snapshot.attempts["collection"])
			require.Empty(t, snapshot.failures)
			require.Equal(t, 2, store.lists, "scrapes must not access the store")
		})
	}
}

func TestRuntimeRevisionGCMetricsAttemptDuration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &fakeGCStore{
			revisions: []database.RuntimeRevision{{Revision: "candidate"}},
			listDelay: 10 * time.Millisecond, beginDelay: 250 * time.Millisecond,
			finalizeDelay: 2 * time.Second,
		}
		templates := &fakeGCTemplates{getDelay: 500 * time.Millisecond, deleteDelay: time.Second}
		collector, reader := newTestRuntimeRevisionGC(t, store, templates)
		require.Empty(t, gatherRuntimeRevisionGCMetrics(t, reader).duration)
		collector.sweep(t.Context())
		snapshot := gatherRuntimeRevisionGCMetrics(t, reader)
		require.Equal(t, map[string]uint64{"discovery": 2, "collection": 1}, snapshot.attempts)
		require.Empty(t, snapshot.failures)
		discovery := snapshot.duration[gcMetricOutcome{"discovery", ""}]
		require.Equal(t, 0.02, discovery.Sum, "both discovery calls must record seconds")
		require.Equal(t, uint64(2), discovery.BucketCounts[1])
		collection := snapshot.duration[gcMetricOutcome{"collection", ""}]
		require.Equal(t, 3.75, collection.Sum, "include claim, compute read/delete, and database finalization exactly once")
		require.Equal(t, uint64(1), collection.BucketCounts[9])
	})
}

func TestRuntimeRevisionGCMetricsDiscoveryErrors(t *testing.T) {
	for _, atEnd := range []bool{false, true} {
		for _, mode := range []string{"error with partial results", "deadline", "parent deadline", "parent cancellation", "canceled successful read"} {
			t.Run(fmt.Sprintf("end=%t/%s", atEnd, mode), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					timeout := 2 * time.Minute
					if mode == "parent deadline" {
						timeout = 30 * time.Second
					}
					ctx, cancel := context.WithTimeout(t.Context(), timeout)
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
						case "deadline", "parent deadline":
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
					require.Equal(t, uint64(wantCollected)+uint64(wantFailures), snapshot.attempts["discovery"])
					require.Equal(t, uint64(wantCollected), snapshot.attempts["collection"])
					if mode == "deadline" {
						require.Equal(t, float64(60), snapshot.duration[gcMetricOutcome{"discovery", "_OTHER"}].Sum)
					}
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
	snapshot := gatherRuntimeRevisionGCMetrics(t, registry)
	require.NotContains(t, snapshot.gauges, gcPendingMetric)
	require.Empty(t, snapshot.attempts)
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
			require.Equal(t, map[string]uint64{"discovery": 1}, snapshot.attempts,
				"parent cancellation excludes the in-flight collection, even if it returned nil")
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
		wantErrorType    string
		wantPending      int64
	}{
		{name: "begin deletion", beginErr: failure, wantErrorType: "_OTHER", wantPending: 1},
		{name: "read ActorTemplate", getErr: failure, wantErrorType: "_OTHER", wantPending: 1},
		{name: "UID changed", changedUID: true, wantErrorType: "_OTHER", wantPending: 1},
		{name: "delete ActorTemplate", deleteErr: failure, wantErrorType: "_OTHER", wantPending: 1},
		{name: "finalize", finalizeErr: failure, wantErrorType: "_OTHER", wantPending: 1},
		{name: "wrapped gRPC read", getErr: fmt.Errorf("read: %w", status.Error(codes.PermissionDenied, "private details")), wantErrorType: "PermissionDenied", wantPending: 1},
		{name: "wrapped gRPC delete", deleteErr: fmt.Errorf("delete: %w", status.Error(codes.Unavailable, "private details")), wantErrorType: "Unavailable", wantPending: 1},
		{name: "joined gRPC delete", deleteErr: errors.Join(failure, status.Error(codes.ResourceExhausted, "private details")), wantErrorType: "ResourceExhausted", wantPending: 1},
		{name: "gRPC unknown", deleteErr: status.Error(codes.Unknown, "private details"), wantErrorType: "Unknown", wantPending: 1},
		{name: "gRPC deadline", deleteErr: status.Error(codes.DeadlineExceeded, "private details"), wantErrorType: "DeadlineExceeded", wantPending: 1},
		{name: "gRPC canceled with active parent", deleteErr: status.Error(codes.Canceled, "private details"), wantErrorType: "Canceled", wantPending: 1},
		{name: "plain canceled with active parent", deleteErr: context.Canceled, wantErrorType: "_OTHER", wantPending: 1},
		{name: "out of range gRPC code", deleteErr: status.Error(codes.Code(999), "private details"), wantErrorType: "_OTHER", wantPending: 1},
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
			if test.wantErrorType != "" {
				wantFailures = 1
			}
			require.Equal(t, wantFailures, snapshot.failures[string(gcStageCollection)])
			require.Zero(t, snapshot.failures[string(gcStageDiscovery)])
			require.Equal(t, map[string]uint64{"discovery": 2, "collection": 1}, snapshot.attempts)
			require.Equal(t, uint64(1), snapshot.duration[gcMetricOutcome{"collection", test.wantErrorType}].Count)
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
	store.listErr = errors.New("discovery failed")
	collector.sweep(t.Context())
	response := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "# TYPE kagent_runtime_revision_gc_pending gauge")
	require.Contains(t, response.Body.String(), "# TYPE kagent_runtime_revision_gc_duration_seconds histogram")
	require.Contains(t, response.Body.String(), "kagent_runtime_revision_gc_duration_seconds_bucket")
	require.Contains(t, response.Body.String(), "kagent_runtime_revision_gc_duration_seconds_count")
	require.Contains(t, response.Body.String(), "kagent_runtime_revision_gc_duration_seconds_sum")
	require.NotContains(t, response.Body.String(), "kagent_runtime_revision_gc_failures")
	families, err := registry.Gather()
	require.NoError(t, err)
	var pendingFound, durationFound bool
	for _, family := range families {
		switch family.GetName() {
		case "kagent_runtime_revision_gc_pending":
			pendingFound = true
			require.Len(t, family.Metric, 1)
			require.Zero(t, family.Metric[0].GetGauge().GetValue())
		case "kagent_runtime_revision_gc_duration_seconds":
			durationFound = true
			require.Len(t, family.Metric, 2)
			outcomes := make(map[string]uint64)
			for _, point := range family.Metric {
				errorType := ""
				stageFound := false
				for _, label := range point.Label {
					switch label.GetName() {
					case "kagent_gc_stage":
						stageFound = true
						require.Equal(t, "discovery", label.GetValue())
					case "error_type":
						errorType = label.GetValue()
					case "otel_scope_name":
						require.Equal(t, "github.com/kagent-dev/kagent/go/core/internal/controller", label.GetValue())
					case "otel_scope_version":
						require.Equal(t, version.Version, label.GetValue())
					case "otel_scope_schema_url":
						require.Empty(t, label.GetValue())
					default:
						t.Fatalf("unexpected duration label %q", label.GetName())
					}
				}
				require.True(t, stageFound)
				outcomes[errorType] = point.GetHistogram().GetSampleCount()
				var bounds []float64
				for _, bucket := range point.GetHistogram().Bucket {
					bounds = append(bounds, bucket.GetUpperBound())
				}
				require.Equal(t, []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}, bounds)
			}
			require.Equal(t, map[string]uint64{"": 2, "_OTHER": 1}, outcomes)
		}
	}
	require.True(t, pendingFound)
	require.True(t, durationFound)
	require.Equal(t, 3, store.lists)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.NoError(t, collector.Start(ctx))
	require.NotContains(t, gatherRuntimeRevisionGCMetrics(t, reader).gauges, gcPendingMetric)
	families, err = registry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		require.NotEqual(t, "kagent_runtime_revision_gc_pending", family.GetName(), "stopped GC must withdraw its last count")
	}
	require.Equal(t, 3, store.lists, "metric readers and canceled startup must not discover")
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
