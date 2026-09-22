package agentinstancetask

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
)

type testSession string

func (s testSession) Principal() auth.Principal {
	return auth.Principal{User: auth.User{ID: string(s)}}
}

type testStore struct {
	instance  *apiv1alpha1.AgentInstance
	tasks     []*a2a.Task
	taskReads int
	unscoped  bool
}

func (s *testStore) GetAgentInstance(_ context.Context, id, userID string) (*apiv1alpha1.AgentInstance, error) {
	if id != s.instance.Id || userID != s.instance.Creator {
		return nil, database.ErrNotFound
	}
	return s.instance, nil
}

func (s *testStore) GetAgentInstanceByID(_ context.Context, id string) (*apiv1alpha1.AgentInstance, error) {
	s.unscoped = true
	if id != s.instance.Id {
		return nil, database.ErrNotFound
	}
	return s.instance, nil
}

func (s *testStore) GetAgentInstanceTask(_ context.Context, instanceID, taskID string, _ *int) (*a2a.Task, error) {
	s.taskReads++
	if instanceID == s.instance.Id {
		for _, task := range s.tasks {
			if string(task.ID) == taskID {
				return task, nil
			}
		}
	}
	return nil, database.ErrNotFound
}

func (s *testStore) ListAgentInstanceTasks(_ context.Context, instanceID, _ string, _ a2a.TaskState, _ *time.Time, _ int, _ *int) ([]*a2a.Task, int, error) {
	s.taskReads++
	if instanceID != s.instance.Id {
		return nil, 0, database.ErrNotFound
	}
	return s.tasks, len(s.tasks), nil
}

type testAuthorizer struct {
	deny  bool
	calls int
}

func (a *testAuthorizer) Check(context.Context, auth.Principal, auth.Verb, auth.Resource) error {
	a.calls++
	if a.deny {
		return errors.New("denied")
	}
	return nil
}

func TestTaskReadsAuthorizeWithoutGatewayMetadata(t *testing.T) {
	id := uuid.NewString()
	for _, test := range []struct {
		name     string
		session  auth.Session
		share    *auth.ShareContext
		deny     bool
		wantErr  error
		unscoped bool
	}{
		{name: "owner", session: testSession("alice")},
		{name: "unauthenticated", wantErr: a2a.ErrUnauthenticated},
		{name: "other owner", session: testSession("bob"), wantErr: a2a.ErrUnauthorized},
		{name: "policy denied", session: testSession("alice"), deny: true, wantErr: a2a.ErrUnauthorized},
		{name: "read share", session: testSession("bob"), deny: true, share: &auth.ShareContext{AgentInstanceID: id, UserID: "alice", ReadOnly: true}},
		{name: "share without login", share: &auth.ShareContext{AgentInstanceID: id, UserID: "alice"}, wantErr: a2a.ErrUnauthenticated},
		{name: "other instance share", session: testSession("bob"), share: &auth.ShareContext{AgentInstanceID: uuid.NewString(), UserID: "alice"}, wantErr: a2a.ErrUnauthorized},
		{name: "legacy session share", session: testSession("bob"), share: &auth.ShareContext{SessionID: id, UserID: "alice"}, wantErr: a2a.ErrUnauthorized},
		{name: "internal controller", session: auth.ControlPlaneSession{}, unscoped: true},
		{name: "internal controller denied", session: auth.ControlPlaneSession{}, deny: true, wantErr: a2a.ErrUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &testStore{instance: &apiv1alpha1.AgentInstance{Id: id, Creator: "alice", ContextId: id, State: apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED}, tasks: []*a2a.Task{{ID: "task", ContextID: id, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}}}
			authorizer := &testAuthorizer{deny: test.deny}
			service := NewService(store, authorizer)
			ctx := t.Context()
			if test.session != nil {
				ctx = auth.AuthSessionTo(ctx, test.session)
			}
			if test.share != nil {
				ctx = auth.ShareContextTo(ctx, test.share)
			}
			// The service takes an explicit ID; no gateway routing headers are present.
			_, getErr := service.GetTask(ctx, id, &a2a.GetTaskRequest{ID: "task"})
			_, listErr := service.ListTasks(ctx, id, nil)
			var subscribeErr error
			for _, err := range service.SubscribeToTask(ctx, id, &a2a.SubscribeToTaskRequest{ID: "task"}, nil) {
				subscribeErr = err
			}
			if test.wantErr != nil {
				require.ErrorIs(t, getErr, test.wantErr)
				require.ErrorIs(t, listErr, test.wantErr)
				require.ErrorIs(t, subscribeErr, test.wantErr)
				require.Zero(t, store.taskReads)
			} else {
				require.NoError(t, getErr)
				require.NoError(t, listErr)
				require.NoError(t, subscribeErr)
				require.Equal(t, 3, store.taskReads)
			}
			require.Equal(t, test.unscoped, store.unscoped)
			if test.session != nil && test.share == nil {
				require.Equal(t, 3, authorizer.calls)
			}
		})
	}
}

func TestReadOnlyShareCannotAuthorizeMutation(t *testing.T) {
	id := uuid.NewString()
	store := &testStore{instance: &apiv1alpha1.AgentInstance{Id: id, Creator: "alice"}}
	service := NewService(store, &testAuthorizer{})
	ctx := auth.AuthSessionTo(t.Context(), testSession("bob"))
	ctx = auth.ShareContextTo(ctx, &auth.ShareContext{AgentInstanceID: id, UserID: "alice", ReadOnly: true})
	for _, verb := range []auth.Verb{auth.VerbCreate, auth.VerbUpdate, auth.VerbDelete} {
		_, err := service.Instance(ctx, id, verb)
		require.ErrorIs(t, err, a2a.ErrUnauthorized)
	}
}

func TestTaskReadsPreserveShapingAndValidation(t *testing.T) {
	id := uuid.NewString()
	task := &a2a.Task{ID: "first", ContextID: id, History: []*a2a.Message{{ID: "old"}, {ID: "latest"}}, Artifacts: []*a2a.Artifact{{ID: "artifact"}}}
	store := &testStore{instance: &apiv1alpha1.AgentInstance{Id: id, Creator: "alice", ContextId: id}, tasks: []*a2a.Task{task, {ID: "second", ContextID: id}}}
	service := NewService(store, &testAuthorizer{})
	ctx := auth.AuthSessionTo(t.Context(), testSession("alice"))
	got, err := service.GetTask(ctx, id, &a2a.GetTaskRequest{ID: "first", HistoryLength: new(1)})
	require.NoError(t, err)
	require.Equal(t, task.History[1:], got.History)
	require.Equal(t, task.Artifacts, got.Artifacts)
	page, err := service.ListTasks(ctx, id, &a2a.ListTasksRequest{PageSize: 1, HistoryLength: new(0)})
	require.NoError(t, err)
	require.Equal(t, 2, page.TotalSize)
	require.Len(t, page.Tasks, 1)
	require.Empty(t, page.Tasks[0].History)
	require.Empty(t, page.Tasks[0].Artifacts)
	cursor, err := decodePageToken(page.NextPageToken)
	require.NoError(t, err)
	require.Equal(t, "first", cursor)
	require.Len(t, task.History, 2)
	require.Len(t, task.Artifacts, 1)

	for _, req := range []*a2a.GetTaskRequest{nil, {}} {
		_, err := service.GetTask(ctx, id, req)
		require.ErrorIs(t, err, a2a.ErrInvalidRequest)
	}
	_, err = service.GetTask(ctx, "invalid", &a2a.GetTaskRequest{ID: "first"})
	require.ErrorIs(t, err, a2a.ErrInvalidRequest)
	_, err = service.GetTask(ctx, id, &a2a.GetTaskRequest{ID: "absent"})
	require.ErrorIs(t, err, a2a.ErrTaskNotFound)
	for _, req := range []*a2a.ListTasksRequest{{PageSize: -1}, {PageSize: 101}, {PageToken: "!"}} {
		_, err := service.ListTasks(ctx, id, req)
		require.ErrorIs(t, err, a2a.ErrInvalidRequest)
	}
	before := store.taskReads
	page, err = service.ListTasks(ctx, id, &a2a.ListTasksRequest{ContextID: "other"})
	require.NoError(t, err)
	require.Empty(t, page.Tasks)
	require.Equal(t, before, store.taskReads)
}

func (s *testStore) GetAgentInstanceTaskObservation(ctx context.Context, instanceID, taskID string, history *int) (*database.TaskObservation, error) {
	task, err := s.GetAgentInstanceTask(ctx, instanceID, taskID, history)
	if err != nil {
		return nil, err
	}
	return &database.TaskObservation{Task: task}, nil
}

func (s *testStore) ListAgentInstanceTaskEvents(context.Context, string, string, int64, int) ([]database.TaskEvent, error) {
	return nil, nil
}
