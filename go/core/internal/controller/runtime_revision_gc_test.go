package controller

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

func TestRuntimeRevisionGCRejectsInvalidInterval(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second} {
		t.Run(interval.String(), func(t *testing.T) {
			store := &fakeGCStore{}
			reader := sdkmetric.NewManualReader()
			collector, err := NewRuntimeRevisionGC(store, &fakeGCTemplates{}, interval, newGCTestMeterProvider(t, reader))
			require.NoError(t, err)
			collector.metrics.recordPending(1)
			require.ErrorContains(t, collector.Start(t.Context()), "interval must be positive")
			require.Zero(t, store.lists)
			require.Empty(t, store.begun)
			snapshot := gatherRuntimeRevisionGCMetrics(t, reader)
			require.NotContains(t, snapshot.gauges, gcPendingMetric)
			require.Empty(t, snapshot.attempts)
		})
	}
}

func TestRuntimeRevisionGCStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		store := &fakeGCStore{revisions: []database.RuntimeArtifact{
			gcRevision("failed"), gcRevision("healthy"),
		}, listErr: errors.New("database unavailable")}
		templates := &fakeGCTemplates{deleteErr: errors.New("Substrate unavailable")}
		registry := sdkmetric.NewManualReader()
		collector, err := NewRuntimeRevisionGC(store, templates, 20*time.Minute, newGCTestMeterProvider(t, registry))
		require.NoError(t, err)
		require.True(t, collector.NeedLeaderElection())
		done := make(chan error, 1)
		go func() { done <- collector.Start(ctx) }()
		synctest.Wait()
		store.mu.Lock()
		require.Equal(t, 1, store.lists, "startup must sweep immediately")

		store.mu.Unlock()
		time.Sleep(19 * time.Minute)
		synctest.Wait()
		store.mu.Lock()
		require.Equal(t, 1, store.lists, "must not query before the configured interval")

		store.listErr = nil
		store.mu.Unlock()
		time.Sleep(time.Minute)
		synctest.Wait()
		store.mu.Lock()
		require.Equal(t, []string{"healthy"}, store.deleted, "a failed candidate must not block later candidates")
		require.Len(t, store.revisions, 1)
		store.mu.Unlock()
		snapshot := gatherRuntimeRevisionGCMetrics(t, registry)
		require.Equal(t, int64(1), snapshot.failures[string(gcStageDiscovery)])
		require.Equal(t, int64(1), snapshot.failures[string(gcStageCollection)])
		require.Equal(t, map[string]uint64{"discovery": 3, "collection": 2}, snapshot.attempts)
		require.Equal(t, int64(1), snapshot.gauges[gcPendingMetric])
		templates.mu.Lock()
		templates.deleteErr = nil
		templates.mu.Unlock()
		time.Sleep(20 * time.Minute)
		synctest.Wait()
		store.mu.Lock()
		require.Equal(t, []string{"healthy", "failed"}, store.deleted, "periodic sweeps must retry without template events")
		store.mu.Unlock()
		snapshot = gatherRuntimeRevisionGCMetrics(t, registry)
		require.Equal(t, map[string]int64{gcPendingMetric: 0}, snapshot.gauges)
		require.Equal(t, int64(1), snapshot.failures[string(gcStageCollection)])
		require.Equal(t, map[string]uint64{"discovery": 5, "collection": 3}, snapshot.attempts)
		cancel()
		require.NoError(t, <-done)
		require.NotContains(t, gatherRuntimeRevisionGCMetrics(t, registry).gauges, gcPendingMetric,
			"stopped collectors must not advertise a known backlog")
	})
}

func TestRuntimeRevisionGCStartCanceledContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		store := &fakeGCStore{}
		collector, registry := newTestRuntimeRevisionGC(t, store, &fakeGCTemplates{})
		started := time.Now()
		require.NoError(t, collector.Start(ctx))
		require.Equal(t, started, time.Now(), "canceled startup must not wait for the ticker")
		require.Zero(t, store.lists)
		require.Empty(t, store.begun)
		snapshot := gatherRuntimeRevisionGCMetrics(t, registry)
		require.NotContains(t, snapshot.gauges, gcPendingMetric)
		require.Zero(t, snapshot.failures[string(gcStageDiscovery)])
		require.Zero(t, snapshot.failures[string(gcStageCollection)])
		require.Empty(t, snapshot.attempts)
	})
}

func TestRuntimeRevisionGCDeadlineAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		store := &fakeGCStore{revisions: []database.RuntimeArtifact{
			gcRevision("failed"), gcRevision("healthy"),
		}}
		templates := &fakeGCTemplates{block: true}
		collector, registry := newTestRuntimeRevisionGC(t, store, templates)
		done := make(chan error, 1)
		go func() { done <- collector.Start(ctx) }()
		synctest.Wait()
		require.Equal(t, int64(2), gatherRuntimeRevisionGCMetrics(t, registry).gauges[gcPendingMetric],
			"publish discovery before waiting for a slow candidate")
		time.Sleep(time.Minute)
		synctest.Wait()
		store.mu.Lock()
		require.Equal(t, []string{"healthy"}, store.deleted, "deadline must release the sweep to process healthy candidates")
		store.mu.Unlock()
		snapshot := gatherRuntimeRevisionGCMetrics(t, registry)
		require.Equal(t, int64(1), snapshot.failures[string(gcStageCollection)])
		require.Equal(t, float64(60), snapshot.duration[gcMetricOutcome{"collection", "_OTHER"}].Sum)
		attempts := snapshot.attempts["collection"]
		cancel() // The next sweep is blocked in the backend again.
		require.NoError(t, <-done, "shutdown must cancel in-flight cleanup")
		require.Len(t, store.revisions, 1, "failed deletion must retain its durable revision")
		require.Equal(t, int64(1), gatherRuntimeRevisionGCMetrics(t, registry).failures[string(gcStageCollection)],
			"parent cancellation must not count as another backend failure")
		require.Equal(t, attempts, gatherRuntimeRevisionGCMetrics(t, registry).attempts["collection"])
	})
}

type fakeGCStore struct {
	mu               sync.Mutex
	revisions        []database.RuntimeArtifact
	listErr          error
	lists            int
	deleted          []string
	listFunc         func(context.Context, int) ([]database.RuntimeArtifact, error)
	begun            []string
	beginErr         error
	finalizeErr      error
	skipClaim        bool
	skipFinalization bool
	listDelay        time.Duration
	beginDelay       time.Duration
	finalizeDelay    time.Duration
}

func newTestRuntimeRevisionGC(t *testing.T, store runtimeRevisionGCStore, templates runtimeRevisionGCClient) (*RuntimeRevisionGC, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	collector, err := NewRuntimeRevisionGC(store, templates, time.Minute, newGCTestMeterProvider(t, reader))
	require.NoError(t, err)
	return collector, reader
}

func newGCTestMeterProvider(t *testing.T, readers ...sdkmetric.Reader) *sdkmetric.MeterProvider {
	t.Helper()
	options := make([]sdkmetric.Option, 0, len(readers))
	for _, reader := range readers {
		options = append(options, sdkmetric.WithReader(reader))
	}
	provider := sdkmetric.NewMeterProvider(options...)
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	return provider
}

func (s *fakeGCStore) ListUnreferencedRuntimeRevisions(ctx context.Context) ([]database.RuntimeArtifact, error) {
	time.Sleep(s.listDelay)
	s.mu.Lock()
	s.lists++
	call, listFunc := s.lists, s.listFunc
	revisions, err := slices.Clone(s.revisions), s.listErr
	s.mu.Unlock()
	if listFunc != nil {
		return listFunc(ctx, call)
	}
	return revisions, err
}

func (s *fakeGCStore) BeginRuntimeRevisionDeletion(_ context.Context, id string) (*database.RuntimeArtifact, error) {
	time.Sleep(s.beginDelay)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.begun = append(s.begun, id)
	if s.beginErr != nil {
		return nil, s.beginErr
	}
	if s.skipClaim {
		return nil, nil
	}
	for _, revision := range s.revisions {
		if revision.Revision == id {
			return &revision, nil
		}
	}
	return nil, nil
}

func (s *fakeGCStore) DeleteRuntimeRevision(_ context.Context, id, _ string) error {
	time.Sleep(s.finalizeDelay)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finalizeErr != nil {
		return s.finalizeErr
	}
	if s.skipFinalization {
		return nil
	}
	s.deleted = append(s.deleted, id)
	for i, revision := range s.revisions {
		if revision.Revision == id {
			s.revisions = append(s.revisions[:i:i], s.revisions[i+1:]...)
			break
		}
	}
	return nil
}

type fakeGCTemplates struct {
	mu sync.Mutex
	fakeActorTemplates
	deleteErr   error
	block       bool
	getDelay    time.Duration
	deleteDelay time.Duration
}

// gcRevision is a revision whose recorded ActorTemplate fakeGCTemplates serves.
func gcRevision(name string) database.RuntimeArtifact {
	return database.RuntimeArtifact{Revision: name, ActorTemplateName: name, ActorTemplateUID: name}
}

// GetActorTemplate serves a set template, or else a live template for every
// named revision, with the name as its UID.
func (f *fakeGCTemplates) GetActorTemplate(ctx context.Context, atespace, name string) (*ateapipb.ActorTemplate, error) {
	time.Sleep(f.getDelay)
	if f.template != nil || name == "" {
		return f.fakeActorTemplates.GetActorTemplate(ctx, atespace, name)
	}
	return &ateapipb.ActorTemplate{Metadata: &ateapipb.ResourceMetadata{Atespace: atespace, Name: name, Uid: name}}, nil
}

func (f *fakeGCTemplates) DeleteActorTemplate(ctx context.Context, _, name, _ string) error {
	time.Sleep(f.deleteDelay)
	f.mu.Lock()
	defer f.mu.Unlock()
	if name == "failed" {
		if f.block {
			<-ctx.Done()
			return ctx.Err()
		}
		return f.deleteErr
	}
	return nil
}

func TestCollectableTemplate(t *testing.T) {
	rebuild := &ateapipb.ActorTemplate{Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "assistant", Uid: "uid-2"}}
	digest, err := substrate.ActorTemplateSpecDigest(rebuild)
	require.NoError(t, err)
	recorded := database.RuntimeArtifact{ActorTemplateAtespace: "team-a", ActorTemplateName: "assistant", ActorTemplateUID: "uid-1"}
	failed := recorded
	failed.GoldenFailureUID, failed.ActorTemplateSpecDigest = "uid-1", digest
	template := func(uid string) *ateapipb.ActorTemplate {
		return &ateapipb.ActorTemplate{Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "assistant", Uid: uid}}
	}
	for _, test := range []struct {
		name     string
		revision database.RuntimeArtifact
		template *ateapipb.ActorTemplate
		getErr   error
		uid      string
		gone     bool
		err      string
	}{
		{name: "recorded template", revision: recorded, template: template("uid-1"), uid: "uid-1"},
		{name: "already deleted", revision: recorded, getErr: status.Error(codes.NotFound, "not found"), gone: true},
		{name: "read failed", revision: recorded, getErr: status.Error(codes.Unavailable, "down"), err: "get unreferenced ActorTemplate team-a/assistant"},
		{name: "another template under the name", revision: recorded, template: template("uid-2"), err: "UID changed"},
		{name: "no recorded UID", revision: database.RuntimeArtifact{ActorTemplateAtespace: "team-a", ActorTemplateName: "assistant"}, template: template(""), err: "UID changed"},
		{name: "unrecorded golden rebuild", revision: failed, template: rebuild, uid: "uid-2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			uid, gone, err := collectableTemplate(test.revision, test.template, test.getErr)
			if test.err != "" {
				require.ErrorContains(t, err, test.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.uid, uid)
			require.Equal(t, test.gone, gone)
		})
	}
}
