package controller

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
)

func TestRuntimeRevisionGCStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		opts := krt.NewOptionsBuilder(ctx.Done(), "test", nil)
		store := &fakeGCStore{revisions: []database.RuntimeRevision{
			{Revision: "failed", ActorTemplateName: "failed"},
			{Revision: "healthy", ActorTemplateName: "healthy"},
		}, listErr: errors.New("database unavailable")}
		templates := &fakeGCTemplates{deleteErr: errors.New("Substrate unavailable")}
		collector := NewRuntimeRevisionGC(krt.NewStaticCollection[ObservedActorTemplate](nil, nil, opts.WithName("ActorTemplates")...), store, templates)
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
		templates.mu.Lock()
		templates.deleteErr = nil
		templates.mu.Unlock()
		time.Sleep(runtimeRevisionGCInterval)
		synctest.Wait()
		store.mu.Lock()
		require.Equal(t, []string{"healthy", "failed"}, store.deleted, "periodic sweeps must retry without template events")
		store.mu.Unlock()
		cancel()
		require.NoError(t, <-done)
	})
}

func TestRuntimeRevisionGCDeadlineAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		opts := krt.NewOptionsBuilder(ctx.Done(), "test", nil)
		store := &fakeGCStore{revisions: []database.RuntimeRevision{
			{Revision: "failed", ActorTemplateName: "failed"},
			{Revision: "healthy", ActorTemplateName: "healthy"},
		}}
		templates := &fakeGCTemplates{block: true}
		collector := NewRuntimeRevisionGC(krt.NewStaticCollection[ObservedActorTemplate](nil, nil, opts.WithName("ActorTemplates")...), store, templates)
		done := make(chan error, 1)
		go func() { done <- collector.Start(ctx) }()
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()
		store.mu.Lock()
		require.Equal(t, []string{"healthy"}, store.deleted, "deadline must release the sweep to process healthy candidates")
		store.mu.Unlock()
		cancel() // The next sweep is blocked in the backend again.
		require.NoError(t, <-done, "shutdown must cancel in-flight cleanup")
		require.Len(t, store.revisions, 1, "failed deletion must retain its durable revision")
	})
}

type fakeGCStore struct {
	mu        sync.Mutex
	revisions []database.RuntimeRevision
	listErr   error
	lists     int
	deleted   []string
}

func (s *fakeGCStore) ListUnreferencedRuntimeRevisions(context.Context) ([]database.RuntimeRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lists++
	return s.revisions, s.listErr
}

func (s *fakeGCStore) BeginRuntimeRevisionDeletion(_ context.Context, id string) (*database.RuntimeRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, revision := range s.revisions {
		if revision.Revision == id {
			return &revision, nil
		}
	}
	return nil, nil
}

func (s *fakeGCStore) DeleteUnreferencedRuntimeRevision(_ context.Context, id, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
