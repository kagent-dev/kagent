package controller

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestRuntimeRevisionGCStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		store := &fakeGCStore{revisions: []database.RuntimeRevision{
			{Revision: "failed", ActorTemplateName: "failed"},
			{Revision: "healthy", ActorTemplateName: "healthy"},
		}, listErr: errors.New("database unavailable")}
		templates := &fakeGCTemplates{deleteErr: errors.New("Substrate unavailable")}
		collector, registry := newTestRuntimeRevisionGC(t, store, templates)
		require.True(t, collector.NeedLeaderElection())
		done := make(chan error, 1)
		go func() { done <- collector.Start(ctx) }()
		synctest.Wait()
		store.mu.Lock()
		require.Equal(t, 1, store.lists, "startup must sweep immediately")

		store.listErr = nil
		store.mu.Unlock()
		time.Sleep(runtimeRevisionGCInterval)
		synctest.Wait()
		store.mu.Lock()
		require.Equal(t, []string{"healthy"}, store.deleted, "a failed candidate must not block later candidates")
		require.Len(t, store.revisions, 1)
		store.mu.Unlock()
		snapshot := gatherRuntimeRevisionGCMetrics(t, registry)
		require.Equal(t, int64(1), snapshot.failures[string(gcStageDiscovery)])
		require.Equal(t, int64(1), snapshot.failures[string(gcStageCollection)])
		require.Equal(t, int64(1), snapshot.gauges[gcPendingMetric])
		templates.mu.Lock()
		templates.deleteErr = nil
		templates.mu.Unlock()
		time.Sleep(runtimeRevisionGCInterval)
		synctest.Wait()
		store.mu.Lock()
		require.Equal(t, []string{"healthy", "failed"}, store.deleted, "periodic sweeps must retry without template events")
		store.mu.Unlock()
		snapshot = gatherRuntimeRevisionGCMetrics(t, registry)
		require.Equal(t, map[string]int64{gcPendingMetric: 0}, snapshot.gauges)
		require.Equal(t, int64(1), snapshot.failures[string(gcStageCollection)])
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
	})
}

func TestRuntimeRevisionGCDeadlineAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		store := &fakeGCStore{revisions: []database.RuntimeRevision{
			{Revision: "failed", ActorTemplateName: "failed"},
			{Revision: "healthy", ActorTemplateName: "healthy"},
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
		require.Equal(t, int64(1), gatherRuntimeRevisionGCMetrics(t, registry).failures[string(gcStageCollection)])
		cancel() // The next sweep is blocked in the backend again.
		require.NoError(t, <-done, "shutdown must cancel in-flight cleanup")
		require.Len(t, store.revisions, 1, "failed deletion must retain its durable revision")
		require.Equal(t, int64(1), gatherRuntimeRevisionGCMetrics(t, registry).failures[string(gcStageCollection)],
			"parent cancellation must not count as another backend failure")
	})
}

type fakeGCStore struct {
	mu               sync.Mutex
	revisions        []database.RuntimeRevision
	listErr          error
	lists            int
	deleted          []string
	listFunc         func(context.Context, int) ([]database.RuntimeRevision, error)
	begun            []string
	beginErr         error
	finalizeErr      error
	skipClaim        bool
	skipFinalization bool
}

func newTestRuntimeRevisionGC(t *testing.T, store runtimeRevisionGCStore, templates runtimeRevisionGCClient) (*RuntimeRevisionGC, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	collector, err := NewRuntimeRevisionGC(store, templates, newGCTestMeterProvider(t, reader))
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

func (s *fakeGCStore) ListUnreferencedRuntimeRevisions(ctx context.Context) ([]database.RuntimeRevision, error) {
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

func (s *fakeGCStore) BeginRuntimeRevisionDeletion(_ context.Context, id string) (*database.RuntimeRevision, error) {
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
	deleteErr error
	block     bool
}

func (f *fakeGCTemplates) DeleteActorTemplate(ctx context.Context, _, name string) error {
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
