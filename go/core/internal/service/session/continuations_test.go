package session

import (
	"context"
	"errors"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type continuationResultStore struct {
	continuationStore
	result *a2a.Task
}

func (s *continuationResultStore) LatestContinuationTask(context.Context, string, string) (*a2a.Task, error) {
	return s.result, nil
}

type continuationGateway struct {
	a2asrv.RequestHandler
	messages []*a2a.SendMessageRequest
}

func (g *continuationGateway) SendMessage(_ context.Context, request *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	g.messages = append(g.messages, request)
	return &a2a.Task{ID: "resumed-task"}, nil
}

func TestContinuationUsesStableNativeAdmissionAndOriginalTaskReferences(t *testing.T) {
	parent := &apiv1.Session{Id: uuid.NewString(), Creator: "owner", Agent: &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"}}
	store := &continuationResultStore{result: &a2a.Task{ID: "child-after-nested-resume", Status: a2a.TaskStatus{State: a2a.TaskStateFailed,
		Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Permission denied"))}}}
	gateway := &continuationGateway{}
	sessions := NewService(&serviceTestStore{getResult: parent}, serviceTestAuthorizer{}, nil)
	service := NewContinuations(store, gateway, sessions)
	childID := "original-child"
	value := database.TaskContinuation{ID: uuid.New(), ParentSessionID: parent.Id, ParentTaskID: "parent-task", ChildSessionID: uuid.NewString(), ChildTaskID: &childID}
	ctx := auth.AuthSessionTo(t.Context(), auth.ControlPlaneSession{})
	for range 2 {
		id, err := service.resume(ctx, value)
		require.NoError(t, err)
		require.Equal(t, "resumed-task", id)
	}
	require.Len(t, gateway.messages, 2)
	for _, request := range gateway.messages {
		require.Equal(t, "kagent-continuation-"+value.ID.String(), request.Message.ID)
		require.Equal(t, parent.Id, request.Message.ContextID)
		require.Empty(t, request.Message.TaskID)
		require.Equal(t, []a2a.TaskID{"parent-task", "original-child"}, request.Message.ReferenceTasks)
		require.Equal(t, "team-a/assistant", request.Tenant)
		require.True(t, request.Config.ReturnImmediately)
		require.Contains(t, request.Message.Parts[0].Text(), "Permission denied")
	}
	service.sessions = NewService(&serviceTestStore{getResult: parent}, serviceTestAuthorizer{err: errors.New("revoked")}, nil)
	_, err := service.resume(ctx, value)
	require.Error(t, err)
	require.Len(t, gateway.messages, 2, "authorization denial must not dispatch")
	policy := &recordingAuthorizer{denied: map[string]bool{value.ChildSessionID: true}}
	service.sessions = NewService(&serviceTestStore{getResult: parent}, policy, nil)
	_, err = service.resume(ctx, value)
	require.Error(t, err)
	require.Equal(t, value.ChildSessionID, policy.resource.Name)
	require.Len(t, gateway.messages, 2, "a revoked child must not dispatch its result")
}
