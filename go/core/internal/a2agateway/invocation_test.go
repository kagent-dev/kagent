package a2agateway

import (
	"context"
	"errors"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
)

type emptyUserSession struct{}

func (emptyUserSession) Principal() auth.Principal { return auth.Principal{} }

func TestGatewayControlPlaneAccessStillRequiresAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name     string
		session  auth.Session
		denied   bool
		unscoped bool
	}{
		{"controller", auth.ControlPlaneSession{}, false, true},
		{"denied controller", auth.ControlPlaneSession{}, true, false},
		{"empty human identity", emptyUserSession{}, false, false},
		{"human owner", gatewayTestSession{}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &gatewayTestStore{instance: gatewayTestInstance()}
			authorizer := &gatewayTestAuthorizer{}
			if tc.denied {
				authorizer.err = errors.New("denied")
			}
			gateway := New(store, authorizer, nil, nil, gatewayTestURL)
			ctx := auth.AuthSessionTo(gatewayTestContext(), tc.session)
			_, err := gateway.ListTasks(ctx, &a2atype.ListTasksRequest{})
			if tc.denied {
				require.ErrorIs(t, err, a2atype.ErrUnauthorized)
				require.Empty(t, store.id, "denial must precede database lookup")
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.session.Principal().User.ID, store.userID)
			}
			require.Equal(t, tc.unscoped, store.unscoped)
			require.Equal(t, tc.session.Principal(), authorizer.principal)
			require.Equal(t, auth.VerbGet, authorizer.verb)
			require.Equal(t, auth.Resource{Type: "AgentInstance", Name: gatewayTestID}, authorizer.resource)
		})
	}
}

type invocationTestRuntime struct {
	*gatewayTestRuntime
	sendErr error
}

func (r *invocationTestRuntime) SendMessage(_ context.Context, _ a2aclient.ServiceParams, req *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	r.sendCalls++
	r.task = &a2atype.Task{ID: req.Message.TaskID, ContextID: req.Message.ContextID, Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}}
	return r.task, r.sendErr
}

func TestTaskInvocationRecoversWithoutRepeatingSend(t *testing.T) {
	for _, lostResponse := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "lost response"}[lostResponse], func(t *testing.T) {
			store := &gatewayTestStore{instance: gatewayTestInstance()}
			runtime := &invocationTestRuntime{gatewayTestRuntime: &gatewayTestRuntime{subscribeErr: a2atype.ErrTaskNotFound}}
			if lostResponse {
				runtime.sendErr = errors.New("response lost")
			}
			workflow := &gatewayTestWorkflow{}
			newGateway := func() a2asrv.RequestHandler {
				return New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, workflow, gatewayTestURL)
			}
			request := invocationTestRequest()
			result, err := newGateway().SendMessage(gatewayTestContext(), request)
			task := result.(*a2atype.Task)
			taskID := task.ID
			if lostResponse {
				require.ErrorIs(t, err, runtime.sendErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, taskID, task.ID)
			require.Equal(t, "message-1", store.created.History[0].ID)
			require.Equal(t, store.instance.Id, store.created.ContextID)
			// Replace the gateway to simulate restart with only durable state.
			completed := *runtime.task
			completed.Status.State = a2atype.TaskStateCompleted
			runtime.task = &completed
			stored, err := newGateway().GetTask(gatewayTestContext(), &a2atype.GetTaskRequest{ID: taskID})
			require.NoError(t, err)
			require.False(t, stored.Status.State.Terminal(), "reads must not refresh runtime state")
			require.Zero(t, runtime.getTaskCalls)
			workflow.err = errors.New("snapshot unavailable")
			err = consumeTaskSubscription(newGateway(), taskID)
			require.Error(t, err)
			require.False(t, store.task.Status.State.Terminal(), "completion waits for cleanup")
			workflow.err = nil
			require.NoError(t, consumeTaskSubscription(newGateway(), taskID))
			task, err = newGateway().GetTask(gatewayTestContext(), &a2atype.GetTaskRequest{ID: taskID})
			require.NoError(t, err)
			require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
			require.Equal(t, "message-1", task.History[0].ID, "subscription recovery must retain the accepted message")
			require.NotNil(t, store.snapshot)
			// Protocol clients (including MCP) see the same persisted invocation.
			visible, err := newGateway().GetTask(gatewayTestContext(), &a2atype.GetTaskRequest{ID: taskID})
			require.NoError(t, err)
			require.Equal(t, task.ID, visible.ID)
			require.Equal(t, a2atype.TaskStateCompleted, visible.Status.State)
			require.Equal(t, store.instance.Id, visible.ContextID)
			_, err = newGateway().GetTask(gatewayTestContext(), &a2atype.GetTaskRequest{ID: taskID})
			require.NoError(t, err)
			require.Equal(t, 1, runtime.sendCalls)
		})
	}
}

func TestTaskInvocationDoesNotResendAnUncertainTask(t *testing.T) {
	store := &gatewayTestStore{instance: gatewayTestInstance()}
	runtime := &invocationTestRuntime{gatewayTestRuntime: &gatewayTestRuntime{cancelErr: a2atype.ErrTaskNotFound, subscribeErr: a2atype.ErrTaskNotFound}, sendErr: errors.New("response lost")}
	workflow := &gatewayTestWorkflow{}
	gateway := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, workflow, gatewayTestURL)
	request := invocationTestRequest()
	_, err := gateway.SendMessage(gatewayTestContext(), request)
	require.Error(t, err)
	taskID := request.Message.TaskID
	runtime.task, runtime.taskErr = nil, a2atype.ErrTaskNotFound
	_, err = gateway.GetTask(gatewayTestContext(), &a2atype.GetTaskRequest{ID: taskID})
	require.NoError(t, err)
	err = consumeTaskSubscription(gateway, taskID)
	require.ErrorIs(t, err, a2atype.ErrTaskNotFound)
	store.replay = store.task
	retry := invocationTestRequest()
	result, err := gateway.SendMessage(gatewayTestContext(), retry)
	require.NoError(t, err)
	require.Equal(t, taskID, result.(*a2atype.Task).ID)
	require.Equal(t, 1, runtime.sendCalls)
	workflow.err = errors.New("suspend unavailable")
	_, err = gateway.CancelTask(gatewayTestContext(), &a2atype.CancelTaskRequest{ID: taskID})
	require.Error(t, err)
	require.Equal(t, a2atype.TaskStateSubmitted, store.task.Status.State)
	workflow.err = nil
	canceled, err := gateway.CancelTask(gatewayTestContext(), &a2atype.CancelTaskRequest{ID: taskID})
	require.NoError(t, err)
	require.Equal(t, a2atype.TaskStateCanceled, canceled.Status.State)
	require.NotNil(t, store.snapshot)
}

func consumeTaskSubscription(gateway a2asrv.RequestHandler, taskID a2atype.TaskID) error {
	for _, err := range gateway.SubscribeToTask(gatewayTestContext(), &a2atype.SubscribeToTaskRequest{ID: taskID}) {
		if err != nil {
			return err
		}
	}
	return nil
}

func invocationTestRequest() *a2atype.SendMessageRequest {
	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("run once"))
	message.ID = "message-1"
	return &a2atype.SendMessageRequest{Message: message, Config: &a2atype.SendMessageConfig{ReturnImmediately: true}}
}

func TestTaskResultCannotOverwriteCancellation(t *testing.T) {
	submitted := &a2atype.Task{ID: gatewayTestID, ContextID: gatewayTestID, Status: a2atype.TaskStatus{State: a2atype.TaskStateSubmitted}}
	canceled := *submitted
	canceled.Status.State = a2atype.TaskStateCanceled
	store := &gatewayTestStore{instance: gatewayTestInstance(), task: &canceled}
	gateway := &Gateway{store: store, coordinator: &memoryRuntimeCoordinator{}}
	result, err := gateway.recordResult(t.Context(), store.instance, submitted, submitted, nil)
	require.NoError(t, err)
	require.Equal(t, a2atype.TaskStateCanceled, result.Status.State)
	require.Empty(t, store.stored)
}

func TestStreamFailurePreservesUncertainTask(t *testing.T) {
	task := &a2atype.Task{ID: gatewayTestID, ContextID: gatewayTestID, Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}}
	store := &gatewayTestStore{instance: gatewayTestInstance(), task: task}
	runtime := &gatewayTestRuntime{subscribeErr: errors.New("stream response lost")}
	gateway := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, &gatewayTestWorkflow{}, gatewayTestURL)
	var streamErr error
	for _, err := range gateway.SubscribeToTask(gatewayTestContext(), &a2atype.SubscribeToTaskRequest{ID: task.ID}) {
		if err != nil {
			streamErr = err
		}
	}
	require.ErrorIs(t, streamErr, runtime.subscribeErr)
	require.Equal(t, a2atype.TaskStateWorking, store.task.Status.State)
	require.Empty(t, store.stored)
}
