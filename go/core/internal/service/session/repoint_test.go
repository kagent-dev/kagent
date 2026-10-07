package session

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/stretchr/testify/require"
)

// repointTestStore pages a sorted listing the way the store does: by id after
// afterID, up to limit.
type repointTestStore struct {
	sessions []*apiv1alpha1.Session
	err      error
	listings int
}

func (s *repointTestStore) ListSessionsOnSupersededRevisions(_ context.Context, afterID string, limit int) ([]*apiv1alpha1.Session, error) {
	s.listings++
	if s.err != nil {
		return nil, s.err
	}
	var page []*apiv1alpha1.Session
	for _, session := range s.sessions {
		if session.GetId() > afterID {
			page = append(page, session)
		}
		if len(page) == limit {
			break
		}
	}
	return page, nil
}

type repointTestWorkflow struct {
	visited []string
	failing map[string]error
}

func (w *repointTestWorkflow) RepointQuiesced(_ context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	w.visited = append(w.visited, session.GetId())
	if err := w.failing[session.GetId()]; err != nil {
		return nil, err
	}
	moved := &apiv1alpha1.Session{Id: session.GetId(), PreparedRevision: "revision-2", State: session.GetState()}
	return moved, nil
}

func TestRevisionRepointSweepVisitsEverySessionOnce(t *testing.T) {
	store := &repointTestStore{}
	for i := range revisionRepointPage + 1 {
		store.sessions = append(store.sessions, &apiv1alpha1.Session{Id: fmt.Sprintf("%04d", i), PreparedRevision: "revision-1", State: apiv1alpha1.RuntimeState_RUNTIME_STATE_READY})
	}
	sort.Slice(store.sessions, func(i, j int) bool { return store.sessions[i].Id < store.sessions[j].Id })
	workflow := &repointTestWorkflow{failing: map[string]error{
		"0003": fmt.Errorf("claimed: %w", database.ErrConflict),
		"0007": errors.New("substrate unavailable"),
	}}
	worker, err := NewRevisionRepointWorker(store, workflow, 0)
	require.NoError(t, err)

	worker.sweep(t.Context())

	require.Len(t, workflow.visited, revisionRepointPage+1, "every session of every page is visited, a failure stops nothing")
	require.Equal(t, 2, store.listings, "a full page is followed by the next one")
	seen := map[string]bool{}
	for _, id := range workflow.visited {
		require.False(t, seen[id], "session %s visited twice", id)
		seen[id] = true
	}
}

func TestRevisionRepointSweepStopsOnAListingError(t *testing.T) {
	store := &repointTestStore{err: errors.New("database down")}
	workflow := &repointTestWorkflow{}
	worker, err := NewRevisionRepointWorker(store, workflow, 0)
	require.NoError(t, err)
	worker.sweep(t.Context())
	require.Empty(t, workflow.visited)
	require.Equal(t, 1, store.listings)
}

func TestRevisionRepointWorkerRefusesANegativeInterval(t *testing.T) {
	_, err := NewRevisionRepointWorker(&repointTestStore{}, &repointTestWorkflow{}, -1)
	require.Error(t, err)
}
