package session

import (
	"context"
	"errors"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

// Only task reads are implemented. Any dispatch or runtime access in these
// permission tests fails immediately through the nil dependencies.
type interactionTestStore struct {
	*database.Client
	task        *a2atype.Task
	taskLookups int
	taskReads   int
	dispatchErr error
	// ownerGroups are the groups the owner's turns recorded.
	ownerGroups []string
}

func (s *interactionTestStore) SetSessionOwnerGroups(_ context.Context, _ string, groups []string) error {
	s.ownerGroups = groups
	return nil
}

func (s *interactionTestStore) GetSessionOwnerGroups(context.Context, string) ([]string, error) {
	return s.ownerGroups, nil
}

func (s *interactionTestStore) ReserveSessionDispatch(context.Context, string, uuid.UUID, string) error {
	return s.dispatchErr
}

func TestPrepareSendStampsInboundTimeline(t *testing.T) {
	for _, test := range []struct {
		name     string
		taskID   a2atype.TaskID
		accepted bool
	}{
		{name: "opening"},
		{name: "reply", taskID: "task"},
		{name: "accepted retry", accepted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			id := uuid.NewString()
			agent := types.NamespacedName{Namespace: "team-a", Name: "assistant"}
			stored := &apiv1alpha1.Session{Id: id, ContextId: id, State: apiv1alpha1.RuntimeState_RUNTIME_STATE_READY,
				Operation: apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE,
				Agent:     &apiv1alpha1.ResourceReference{Namespace: agent.Namespace, Name: agent.Name}}
			original := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Approved"))
			originalPosition := time.Now().Add(-time.Hour).UTC()
			apia2a.SetTimelinePosition(original, originalPosition)
			tasks := &interactionTestStore{task: &a2atype.Task{ID: "task", ContextID: id,
				Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired}, History: []*a2atype.Message{original}}}
			if test.accepted {
				tasks.dispatchErr = database.ErrMessageAccepted
			}
			service := NewInteractionService(tasks, &turnTestAgents{}, NewService(&serviceTestStore{getResult: stored}, serviceTestAuthorizer{}, nil))
			message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Approved"))
			message.ID, message.ContextID, message.TaskID = original.ID, id, test.taskID
			apia2a.SetTimelinePosition(message, time.Unix(1, 0))
			before := time.Now().UTC()
			prepared, err := service.PrepareSend(serviceTestContext("alice"), agent, &a2atype.SendMessageRequest{Message: message})
			if err != nil {
				t.Fatal(err)
			}
			if test.accepted {
				position, ok := apia2a.TimelinePosition(prepared.AcceptedTask.History[0])
				if !ok || !position.Equal(originalPosition) || prepared.DispatchID != uuid.Nil {
					t.Fatal("accepted retry changed the original timeline or granted dispatch")
				}
				return
			}
			position, ok := apia2a.TimelinePosition(message)
			if !ok || position.Before(before) || position.After(time.Now().UTC()) || prepared.DispatchID == uuid.Nil {
				t.Fatalf("dispatched input position = %v, want server time after %v", position, before)
			}
		})
	}
}

func (s *interactionTestStore) SessionForTask(context.Context, string) (string, error) {
	s.taskLookups++
	return s.task.ContextID, nil
}

func (s *interactionTestStore) GetSessionTask(context.Context, string, string, *int) (*a2atype.Task, error) {
	s.taskReads++
	return s.task, nil
}

func (s *interactionTestStore) ListAgentTasks(context.Context, []string, string, a2atype.TaskState, *time.Time, int, *int) ([]*a2atype.Task, int, error) {
	s.taskReads++
	return []*a2atype.Task{s.task}, 1, nil
}

func (s *interactionTestStore) GetSettledSessionTask(context.Context, string, string, *int) (*a2atype.Task, error) {
	s.taskReads++
	return s.task, nil
}

func (s *interactionTestStore) GetSessionTaskByMessage(context.Context, string, string, string) (*a2atype.Task, error) {
	s.taskReads++
	return s.task, nil
}

func (s *interactionTestStore) GetTaskPushConfig(context.Context, string, string, string) (*database.TaskPushConfig, error) {
	s.taskReads++
	return &database.TaskPushConfig{ID: "callback", URL: "https://receiver.example/callback"}, nil
}

func (s *interactionTestStore) ListTaskPushConfigs(context.Context, string, string, string, int) ([]database.TaskPushConfig, error) {
	s.taskReads++
	return []database.TaskPushConfig{{ID: "callback", URL: "https://receiver.example/callback"}}, nil
}

// turnTestAgents stands in for the Agent service, which reviews get on the
// Agent for the caller; it refuses every caller once denied.
type turnTestAgents struct {
	denied bool
	asked  []types.NamespacedName
}

func (a *turnTestAgents) Get(_ context.Context, ref types.NamespacedName) (*kagentv1alpha3.Agent, error) {
	a.asked = append(a.asked, ref)
	if a.denied {
		return nil, serviceerrors.NewPermissionDenied("Not authorized", errors.New("may not get the Agent"))
	}
	return &kagentv1alpha3.Agent{}, nil
}

// Every turn of an existing conversation is checked for the person who sends
// it, the owner and a share's visitor alike: a sender who may no longer get
// the Agent runs no further turn.
func TestPrepareSendChecksTheAgentOnEveryTurn(t *testing.T) {
	id := uuid.NewString()
	agent := types.NamespacedName{Namespace: "team-a", Name: "assistant"}
	for _, test := range []struct {
		name    string
		ctx     context.Context
		taskID  a2atype.TaskID
		denied  bool
		wantErr error
	}{
		{name: "the owner's next turn", ctx: serviceTestContext("alice")},
		{name: "the owner's reply to a running turn", ctx: serviceTestContext("alice"), taskID: "task"},
		{name: "the owner after a revoked grant", ctx: serviceTestContext("alice"), denied: true, wantErr: a2atype.ErrUnauthorized},
		{name: "a share's visitor after a revoked grant", ctx: auth.ShareContextTo(serviceTestContext("visitor"), &auth.ShareContext{SessionID: id, UserID: "alice"}),
			denied: true, wantErr: a2atype.ErrUnauthorized},
		{name: "a reply after a revoked grant", ctx: serviceTestContext("alice"), taskID: "task", denied: true, wantErr: a2atype.ErrUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			stored := &apiv1alpha1.Session{Id: id, ContextId: id, State: apiv1alpha1.RuntimeState_RUNTIME_STATE_READY,
				Operation: apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE,
				Agent:     &apiv1alpha1.ResourceReference{Namespace: agent.Namespace, Name: agent.Name}}
			tasks := &interactionTestStore{task: &a2atype.Task{ID: "task", ContextID: id, Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired}}}
			agents := &turnTestAgents{denied: test.denied}
			service := NewInteractionService(tasks, agents, NewService(&serviceTestStore{getResult: stored}, serviceTestAuthorizer{}, nil))
			message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("next"))
			message.ContextID, message.TaskID = id, test.taskID
			prepared, err := service.PrepareSend(test.ctx, agent, &a2atype.SendMessageRequest{Message: message})
			require.Equal(t, []types.NamespacedName{agent}, agents.asked, "the sender's access to the Agent is checked")
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
				require.Nil(t, prepared, "a refused turn is not dispatched")
				return
			}
			require.NoError(t, err)
			require.NotEqual(t, uuid.Nil, prepared.DispatchID)
		})
	}
}

func TestTaskPageTokenRejectsInvalidUTF8(t *testing.T) {
	_, err := decodeTaskPageToken("invalid")
	if err == nil {
		t.Fatal("invalid UTF-8 page token was accepted")
	}
}

func TestInteractionsEnforcePermissionsWithoutGateway(t *testing.T) {
	id := uuid.NewString()
	agent := types.NamespacedName{Namespace: "team-a", Name: "assistant"}
	for _, operation := range []struct {
		name string
		verb auth.Verb
		call func(context.Context, *InteractionService, types.NamespacedName) error
	}{
		{name: "settled task", verb: auth.VerbGet, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, err := s.GetSettledTask(ctx, agent, id, "task", nil)
			return err
		}},
		{name: "send result", verb: auth.VerbCreate, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, err := s.GetSendResult(ctx, agent, &a2atype.Message{ID: "input", ContextID: id}, "task", nil)
			return err
		}},
		{name: "cancel result", verb: auth.VerbUpdate, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, err := s.GetCancelResult(ctx, agent, "task")
			return err
		}},
		{name: "accepted message", verb: auth.VerbCreate, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, err := s.GetTaskByMessage(ctx, agent, &a2atype.Message{ID: "input", ContextID: id})
			return err
		}},
		{name: "revoke send", verb: auth.VerbCreate, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, err := s.RevokeSend(ctx, agent, &a2atype.Message{ID: "input", ContextID: id}, uuid.New())
			return err
		}},
		{name: "get", verb: auth.VerbGet, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, err := s.GetTask(ctx, agent, &a2atype.GetTaskRequest{ID: "task"})
			return err
		}},
		{name: "list", verb: auth.VerbGet, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, err := s.ListTasks(ctx, agent, &a2atype.ListTasksRequest{ContextID: id})
			return err
		}},
		{name: "push create", verb: auth.VerbCreate, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, err := s.CreateTaskPushConfig(ctx, agent, &a2atype.PushConfig{TaskID: "task", ID: "callback", URL: "https://receiver.example/callback", Auth: &a2atype.PushAuthInfo{Scheme: "Bearer"}})
			return err
		}},
		{name: "push get", verb: auth.VerbGet, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, err := s.GetTaskPushConfig(ctx, agent, &a2atype.GetTaskPushConfigRequest{TaskID: "task", ID: "callback"})
			return err
		}},
		{name: "push list", verb: auth.VerbGet, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, err := s.ListTaskPushConfigs(ctx, agent, &a2atype.ListTaskPushConfigRequest{TaskID: "task"})
			return err
		}},
		{name: "push delete", verb: auth.VerbDelete, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			return s.DeleteTaskPushConfig(ctx, agent, &a2atype.DeleteTaskPushConfigRequest{TaskID: "task", ID: "callback"})
		}},
		{name: "subscribe", verb: auth.VerbGet, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, _, err := s.PrepareTaskSubscription(ctx, agent, &a2atype.SubscribeToTaskRequest{ID: "task"})
			return err
		}},
		{name: "cancel", verb: auth.VerbUpdate, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, _, err := s.PrepareCancelTask(ctx, agent, &a2atype.CancelTaskRequest{ID: "task"})
			return err
		}},
		{name: "send", verb: auth.VerbCreate, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, err := s.PrepareSend(ctx, agent, &a2atype.SendMessageRequest{Message: &a2atype.Message{ID: "input", ContextID: id}})
			return err
		}},
		{name: "reply", verb: auth.VerbUpdate, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, err := s.PrepareSend(ctx, agent, &a2atype.SendMessageRequest{Message: &a2atype.Message{ID: "input", TaskID: "task"}})
			return err
		}},
	} {
		for _, test := range []struct {
			name  string
			ctx   context.Context
			agent types.NamespacedName
			want  error
		}{
			{name: "unauthenticated", ctx: context.Background(), agent: agent, want: a2atype.ErrUnauthenticated},
			{name: "missing Agent", ctx: serviceTestContext("visitor"), want: a2atype.ErrInvalidRequest},
			{name: "denied caller", ctx: serviceTestContext("visitor"), agent: agent, want: a2atype.ErrUnauthorized},
			{name: "read-only share", ctx: auth.ShareContextTo(serviceTestContext("visitor"), &auth.ShareContext{SessionID: id, UserID: "owner", ReadOnly: true}), agent: agent},
			{name: "other Agent", ctx: auth.ShareContextTo(serviceTestContext("visitor"), &auth.ShareContext{SessionID: id, UserID: "owner"}), agent: types.NamespacedName{Namespace: "team-a", Name: "other"}, want: a2atype.ErrUnauthorized},
		} {
			t.Run(operation.name+"/"+test.name, func(t *testing.T) {
				stored := &apiv1alpha1.Session{Id: id, ContextId: id, State: apiv1alpha1.RuntimeState_RUNTIME_STATE_SUSPENDED,
					Agent: &apiv1alpha1.ResourceReference{Namespace: agent.Namespace, Name: agent.Name}}
				sessionStore := &serviceTestStore{getResult: stored}
				authorizer := &recordingAuthorizer{denied: map[string]bool{id: true}}
				tasks := &interactionTestStore{task: &a2atype.Task{ID: "task", ContextID: id, Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}}
				service := NewInteractionService(tasks, &turnTestAgents{}, NewService(sessionStore, authorizer, nil))
				want := test.want
				if test.name == "read-only share" && operation.verb != auth.VerbGet {
					want = a2atype.ErrUnauthorized
				}
				// No A2A SDK tenant or interceptor is installed for this call.
				err := operation.call(test.ctx, service, test.agent)
				if !errors.Is(err, want) {
					t.Fatalf("operation error = %v, want %v", err, want)
				}
				if want != nil && tasks.taskReads != 0 {
					t.Fatal("unauthorized operation read task content")
				}
				if want == nil && (tasks.taskReads != 1 || sessionStore.getCreator != "owner") {
					t.Fatal("shared history was not read through its owner")
				}
				if test.name == "unauthenticated" && tasks.taskLookups != 0 {
					t.Fatal("unauthenticated operation looked up a task")
				}
				if test.name == "denied caller" && (authorizer.verb != operation.verb || authorizer.resource.Type != "Session" || authorizer.resource.Name != id) {
					t.Fatalf("operation used the wrong permission: %+v", authorizer)
				}
			})
		}
	}
}

func TestInteractionsRequireAgentForUnfilteredTaskList(t *testing.T) {
	service := NewInteractionService(nil, nil, nil)
	for _, agent := range []types.NamespacedName{
		{},
		{Namespace: "team-a"},
		{Name: "assistant"},
		{Namespace: "team-a", Name: "INVALID"},
	} {
		_, err := service.ListTasks(serviceTestContext("alice"), agent, nil)
		if !errors.Is(err, a2atype.ErrInvalidRequest) {
			t.Fatalf("ListTasks(%v) = %v, want invalid Agent before storage", agent, err)
		}
	}
}

type principalTestSession struct{ principal auth.Principal }

func (s principalTestSession) Principal() auth.Principal { return s.principal }

// The owner's turn records the groups it carries; a turn a share admits gets
// the recorded ones for the calls the agent makes for the owner, and records
// nothing. An owner's turn without an identity of the person's own (no claims,
// no groups) keeps the recorded groups.
func TestPrepareSendCarriesTheOwnersGroups(t *testing.T) {
	id := uuid.NewString()
	agent := types.NamespacedName{Namespace: "team-a", Name: "assistant"}
	owner := func(principal auth.Principal) context.Context {
		return auth.AuthSessionTo(context.Background(), principalTestSession{principal: principal})
	}
	share := auth.ShareContextTo(owner(auth.Principal{User: auth.User{ID: "visitor", Groups: []string{"visitors"}}, Claims: map[string]any{}}),
		&auth.ShareContext{SessionID: id, UserID: "alice"})
	for _, test := range []struct {
		name       string
		ctx        context.Context
		wantStored []string
		wantSent   []string
	}{
		{name: "the owner's turn with claims", ctx: owner(auth.Principal{User: auth.User{ID: "alice", Groups: []string{"team-a"}}, Claims: map[string]any{}}), wantStored: []string{"team-a"}},
		{name: "the owner's turn with claims and no groups", ctx: owner(auth.Principal{User: auth.User{ID: "alice"}, Claims: map[string]any{}}), wantStored: nil},
		{name: "the owner's turn an agent propagates", ctx: owner(auth.Principal{User: auth.User{ID: "alice", Groups: []string{"team-b"}}}), wantStored: []string{"team-b"}},
		{name: "the owner's turn without an identity of their own", ctx: serviceTestContext("alice"), wantStored: []string{"recorded"}},
		{name: "a share's turn", ctx: share, wantStored: []string{"recorded"}, wantSent: []string{"recorded"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stored := &apiv1alpha1.Session{Id: id, ContextId: id, State: apiv1alpha1.RuntimeState_RUNTIME_STATE_READY,
				Operation: apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE,
				Agent:     &apiv1alpha1.ResourceReference{Namespace: agent.Namespace, Name: agent.Name}}
			tasks := &interactionTestStore{ownerGroups: []string{"recorded"}}
			service := NewInteractionService(tasks, &turnTestAgents{}, NewService(&serviceTestStore{getResult: stored}, serviceTestAuthorizer{}, nil))
			message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("next"))
			message.ContextID = id
			prepared, err := service.PrepareSend(test.ctx, agent, &a2atype.SendMessageRequest{Message: message})
			require.NoError(t, err)
			require.Equal(t, test.wantStored, tasks.ownerGroups)
			require.Equal(t, test.wantSent, prepared.OwnerGroups)
		})
	}
}
