package taskstore

import (
	"context"
	"testing"
	"testing/synctest"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type settlementStore struct {
	Store
	err       error
	commit    <-chan struct{}
	committed bool
}

var _ Store = (*settlementStore)(nil)

func (s *settlementStore) GetSessionForRuntime(_ context.Context, id, _ string) (*apiv1alpha1.Session, error) {
	return &apiv1alpha1.Session{Id: id, A2AAuthority: substrate.ActorHost("team-a", substrate.ActorName(id), "")}, nil
}

func (s *settlementStore) SettleSessionTask(context.Context, string, string, int64) error {
	<-s.commit
	s.committed = s.err == nil
	return s.err
}

func TestSettleTaskWakesOnlyAfterCommit(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		queued bool
		want   codes.Code
	}{
		{name: "committed"},
		{name: "failed", err: database.ErrConflict, want: codes.Aborted},
		{name: "coalesced", queued: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				commit := make(chan struct{})
				defer close(commit)
				store := &settlementStore{err: test.err, commit: commit}
				wakes := make(chan struct{}, 1)
				if test.queued {
					wakes <- struct{}{}
				}
				pushWakes := make(chan struct{}, 1)
				if test.queued {
					pushWakes <- struct{}{}
				}
				before := len(wakes)
				service := NewService(store, wakes, pushWakes)
				ctx, cancel := context.WithCancel(auth.AuthSessionTo(t.Context(), runtimeSession{sessionID: "session", atespace: "team-a", actorUID: "actor"}))
				cancel() // A disconnected caller must not suppress the post-commit hint.
				done := make(chan error, 1)
				go func() {
					_, err := service.SettleTask(ctx, &apiv1alpha1.TaskStoreServiceSettleTaskRequest{SessionId: "session", TaskId: "task", Version: 1})
					done <- err
				}()
				synctest.Wait()
				require.False(t, store.committed)
				require.Len(t, wakes, before, "must not signal before the commit")
				require.Len(t, pushWakes, before, "must not signal push before the commit")
				commit <- struct{}{}
				require.Equal(t, test.want, status.Code(<-done), "a queued hint must not block settlement")
				if test.err == nil {
					require.True(t, store.committed)
					require.Len(t, wakes, 1)
					require.Len(t, pushWakes, 1)
				} else {
					require.False(t, store.committed)
					require.Empty(t, wakes)
					require.Empty(t, pushWakes)
				}
			})
		})
	}
}
