package a2agateway

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestTaskInvocationDoesNotReplayIssuedTurn(t *testing.T) {
	store := &gatewayTestStore{instance: gatewayTestInstance()}
	runtime := &invocationTestRuntime{gatewayTestRuntime: &gatewayTestRuntime{}, sendErr: errors.New("response lost")}
	workflow := &gatewayTestWorkflow{}
	gateway := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, workflow, gatewayTestURL)
	request := invocationTestRequest()
	result, err := gateway.SendMessage(gatewayTestContext(), request)
	require.ErrorIs(t, err, runtime.sendErr)
	taskID := request.Message.TaskID
	require.Equal(t, taskID, result.(*a2atype.Task).ID)
	require.Equal(t, "ISSUED", store.turnPhase)
	store.replay = store.task
	retry := invocationTestRequest()
	result, err = gateway.SendMessage(gatewayTestContext(), retry)
	require.NoError(t, err)
	require.Equal(t, taskID, result.(*a2atype.Task).ID)
	require.Equal(t, 1, runtime.sendCalls)
	err = consumeTaskSubscription(gateway, taskID)
	require.ErrorIs(t, err, a2atype.ErrInternalError)
	ctx, cancel := context.WithTimeout(gatewayTestContext(), 10*time.Millisecond)
	defer cancel()
	_, err = gateway.CancelTask(ctx, &a2atype.CancelTaskRequest{ID: taskID})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.True(t, store.cancelRequested)
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
	require.ErrorIs(t, streamErr, a2atype.ErrInternalError)
	require.Zero(t, runtime.subscribeCalls)
	require.Equal(t, a2atype.TaskStateWorking, store.task.Status.State)
	require.Empty(t, store.stored)
}
