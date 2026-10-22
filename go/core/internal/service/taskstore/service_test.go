package taskstore

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type settlementStore struct {
	Store
	err       error
	commit    <-chan struct{}
	settled   *database.SettledTask
	committed bool
}

var _ Store = (*settlementStore)(nil)

func (s *settlementStore) GetSessionForRuntime(_ context.Context, id, _ string) (*apiv1.Session, error) {
	return &apiv1.Session{Id: id, Agent: &apiv1.AgentProvenance{Ref: &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"}}, A2AAuthority: substrate.ActorHost("team-a", substrate.ActorName(id), "")}, nil
}

func (s *settlementStore) SettleSessionTask(context.Context, string, string, int64) (*database.SettledTask, error) {
	<-s.commit
	s.committed = s.err == nil
	return s.settled, s.err
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
				service := NewService(store, wakes, pushWakes, nil)
				ctx, cancel := context.WithCancel(runtimeContext(t))
				cancel() // A disconnected caller must not suppress the post-commit hint.
				done := make(chan error, 1)
				go func() {
					_, err := service.SettleTask(ctx, &runtimev1.TaskStoreServiceSettleTaskRequest{SessionId: "00000000-0000-0000-0000-000000000001", TaskId: "task", Version: 1})
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

type testActorVerifier struct{}

func (testActorVerifier) Verify(context.Context, string) (substrate.ActorIdentity, error) {
	return substrate.ActorIdentity{ActorName: "session-00000000-0000-0000-0000-000000000001", Atespace: "team-a", ActorUID: "actor"}, nil
}

func runtimeContext(t *testing.T) context.Context {
	t.Helper()
	session, err := runtimeauth.NewAuthenticator(testActorVerifier{}).Authenticate(t.Context(), http.Header{"Authorization": {"Bearer test"}}, nil)
	require.NoError(t, err)
	return auth.AuthSessionTo(t.Context(), session)
}

type pagedTaskStore struct {
	Store
	tasks []*a2atype.Task
}

func (s *pagedTaskStore) GetSessionForRuntime(_ context.Context, id, _ string) (*apiv1.Session, error) {
	return &apiv1.Session{Id: id, A2AAuthority: substrate.ActorHost("team-a", substrate.ActorName(id), "")}, nil
}

func (s *pagedTaskStore) ListSessionTasks(_ context.Context, _ string, afterID string, _ a2atype.TaskState, _ *time.Time, limit int, _ *int) ([]*a2atype.Task, int, error) {
	var page []*a2atype.Task
	for _, task := range s.tasks {
		if string(task.ID) > afterID && len(page) < limit {
			page = append(page, task)
		}
	}
	return page, len(s.tasks), nil
}

func TestListTasksEndsPageBySize(t *testing.T) {
	// Three 5 MiB tasks: two fit under MaxTaskPageBytes, the third does not.
	big := strings.Repeat("x", 5<<20)
	var tasks []*a2atype.Task
	for _, id := range []a2atype.TaskID{"task-1", "task-2", "task-3"} {
		tasks = append(tasks, &a2atype.Task{ID: id, Artifacts: []*a2atype.Artifact{{ID: "a", Parts: a2atype.ContentParts{a2atype.NewTextPart(big)}}}})
	}
	service := NewService(&pagedTaskStore{tasks: tasks}, nil, nil, nil)
	sessionID := "00000000-0000-0000-0000-000000000001"
	list := func(token string) *a2atype.ListTasksResponse {
		request, err := pbconv.ToProtoListTasksRequest(&a2atype.ListTasksRequest{IncludeArtifacts: true, PageToken: token})
		require.NoError(t, err)
		response, err := service.ListTasks(runtimeContext(t), &runtimev1.TaskStoreServiceListTasksRequest{SessionId: sessionID, Request: request})
		require.NoError(t, err)
		result, err := pbconv.FromProtoListTasksResponse(response.Result)
		require.NoError(t, err)
		return result
	}

	first := list("")
	require.Len(t, first.Tasks, 2)
	require.Equal(t, base64.RawURLEncoding.EncodeToString([]byte("task-2")), first.NextPageToken)

	second := list(first.NextPageToken)
	require.Len(t, second.Tasks, 1)
	require.Equal(t, a2atype.TaskID("task-3"), second.Tasks[0].ID)
	require.Empty(t, second.NextPageToken)
}
