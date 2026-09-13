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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
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
		require.Equal(t, float64(1), snapshot.failures[string(gcStageDiscovery)])
		require.Equal(t, float64(1), snapshot.failures[string(gcStageDeleteActorTemplate)])
		require.Equal(t, float64(1), snapshot.gauges[gcPendingMetric])
		require.Positive(t, snapshot.gauges[gcAgeMetric])
		templates.mu.Lock()
		templates.deleteErr = nil
		templates.mu.Unlock()
		time.Sleep(runtimeRevisionGCInterval)
		synctest.Wait()
		store.mu.Lock()
		require.Equal(t, []string{"healthy", "failed"}, store.deleted, "periodic sweeps must retry without template events")
		store.mu.Unlock()
		snapshot = gatherRuntimeRevisionGCMetrics(t, registry)
		require.Zero(t, snapshot.gauges[gcPendingMetric])
		require.Zero(t, snapshot.gauges[gcAgeMetric])
		require.Equal(t, float64(1), snapshot.failures[string(gcStageDeleteActorTemplate)])
		cancel()
		require.NoError(t, <-done)
		require.Empty(t, gatherRuntimeRevisionGCMetrics(t, registry).gauges, "stopped collectors must not advertise backlog")
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
		time.Sleep(time.Minute)
		synctest.Wait()
		store.mu.Lock()
		require.Equal(t, []string{"healthy"}, store.deleted, "deadline must release the sweep to process healthy candidates")
		store.mu.Unlock()
		require.Equal(t, float64(1), gatherRuntimeRevisionGCMetrics(t, registry).failures[string(gcStageDeleteActorTemplate)])
		cancel() // The next sweep is blocked in the backend again.
		require.NoError(t, <-done, "shutdown must cancel in-flight cleanup")
		require.Len(t, store.revisions, 1, "failed deletion must retain its durable revision")
		require.Equal(t, float64(1), gatherRuntimeRevisionGCMetrics(t, registry).failures[string(gcStageDeleteActorTemplate)],
			"parent cancellation must not count as another backend failure")
	})
}

type fakeGCStore struct {
	mu               sync.Mutex
	revisions        []database.RuntimeRevision
	listErr          error
	lists            int
	deleted          []string
	pendingSince     map[string]time.Time
	observationErr   error
	beginErr         error
	finalizeErr      error
	skipClaim        bool
	skipFinalization bool
}

func newTestRuntimeRevisionGC(t *testing.T, store runtimeRevisionGCStore, templates runtimeRevisionGCClient) (*RuntimeRevisionGC, *prometheus.Registry) {
	t.Helper()
	registry := prometheus.NewRegistry()
	collector, err := NewRuntimeRevisionGC(store, templates, registry)
	require.NoError(t, err)
	return collector, registry
}

func (s *fakeGCStore) ObserveRuntimeRevisionCleanupBacklog(context.Context) (database.RuntimeRevisionCleanupBacklog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.observationErr != nil {
		return database.RuntimeRevisionCleanupBacklog{}, s.observationErr
	}
	if s.pendingSince == nil {
		s.pendingSince = make(map[string]time.Time)
	}
	backlog := database.RuntimeRevisionCleanupBacklog{Pending: int64(len(s.revisions))}
	for _, revision := range s.revisions {
		since, exists := s.pendingSince[revision.Revision]
		if !exists {
			since = time.Now()
			s.pendingSince[revision.Revision] = since
		}
		if backlog.OldestPendingSince == nil || since.Before(*backlog.OldestPendingSince) {
			backlog.OldestPendingSince = &since
		}
	}
	return backlog, nil
}

func (s *fakeGCStore) ListUnreferencedRuntimeRevisions(context.Context) ([]database.RuntimeRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lists++
	return slices.Clone(s.revisions), s.listErr
}

func (s *fakeGCStore) BeginRuntimeRevisionDeletion(_ context.Context, id string) (*database.RuntimeRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
			delete(s.pendingSince, id)
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

func (f *fakeGCTemplates) DeleteActorTemplate(ctx context.Context, _, name, _ string) error {
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
