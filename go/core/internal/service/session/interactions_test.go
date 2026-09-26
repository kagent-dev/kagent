package session

import (
	"context"
	"errors"
	"iter"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"k8s.io/apimachinery/pkg/types"
)

// Only task reads are implemented. Any dispatch or runtime access in these
// permission tests fails immediately through the nil dependencies.
type interactionTestStore struct {
	*database.Client
	task        *a2atype.Task
	taskLookups int
	taskReads   int
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

func firstInteractionError(events iter.Seq2[a2atype.Event, error]) error {
	for _, err := range events {
		return err
	}
	return errors.New("operation returned no result")
}

func TestInteractionsEnforcePermissionsWithoutGateway(t *testing.T) {
	id := uuid.NewString()
	agent := types.NamespacedName{Namespace: "team-a", Name: "assistant"}
	for _, operation := range []struct {
		name string
		verb auth.Verb
		call func(context.Context, *InteractionService, types.NamespacedName) error
	}{
		{name: "get", verb: auth.VerbGet, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, err := s.GetTask(ctx, agent, &a2atype.GetTaskRequest{ID: "task"})
			return err
		}},
		{name: "list", verb: auth.VerbGet, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, err := s.ListTasks(ctx, agent, &a2atype.ListTasksRequest{ContextID: id})
			return err
		}},
		{name: "subscribe", verb: auth.VerbGet, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			return firstInteractionError(s.SubscribeToTask(ctx, agent, &a2atype.SubscribeToTaskRequest{ID: "task"}))
		}},
		{name: "cancel", verb: auth.VerbUpdate, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, err := s.CancelTask(ctx, agent, &a2atype.CancelTaskRequest{ID: "task"})
			return err
		}},
		{name: "send", verb: auth.VerbCreate, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, err := s.SendMessage(ctx, agent, &a2atype.SendMessageRequest{Message: &a2atype.Message{ID: "input", ContextID: id}})
			return err
		}},
		{name: "reply", verb: auth.VerbUpdate, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			_, err := s.SendMessage(ctx, agent, &a2atype.SendMessageRequest{Message: &a2atype.Message{ID: "input", TaskID: "task"}})
			return err
		}},
		{name: "stream", verb: auth.VerbCreate, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			return firstInteractionError(s.SendStreamingMessage(ctx, agent, &a2atype.SendMessageRequest{Message: &a2atype.Message{ID: "input", ContextID: id}}))
		}},
		{name: "stream reply", verb: auth.VerbUpdate, call: func(ctx context.Context, s *InteractionService, agent types.NamespacedName) error {
			return firstInteractionError(s.SendStreamingMessage(ctx, agent, &a2atype.SendMessageRequest{Message: &a2atype.Message{ID: "input", TaskID: "task"}}))
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
				stored := &apiv1alpha1.Session{Id: id, ContextId: id, State: apiv1alpha1.SessionState_SESSION_STATE_SUSPENDED,
					Agent: &apiv1alpha1.ResourceReference{Namespace: agent.Namespace, Name: agent.Name}}
				sessionStore := &serviceTestStore{getResult: stored}
				authorizer := &recordingAuthorizer{denied: map[string]bool{id: true}}
				tasks := &interactionTestStore{task: &a2atype.Task{ID: "task", ContextID: id, Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}}
				service := NewInteractionService(tasks, nil, nil, NewService(sessionStore, authorizer, nil))
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
	service := NewInteractionService(nil, nil, nil, nil)
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

func TestQuiescentTaskStates(t *testing.T) {
	for _, state := range []a2atype.TaskState{
		a2atype.TaskStateCompleted,
		a2atype.TaskStateCanceled,
		a2atype.TaskStateFailed,
		a2atype.TaskStateRejected,
		a2atype.TaskStateInputRequired,
		a2atype.TaskStateAuthRequired,
	} {
		if !isQuiescent(state) {
			t.Errorf("isQuiescent(%s) = false", state)
		}
	}
	if isQuiescent(a2atype.TaskStateWorking) {
		t.Error("working task is quiescent")
	}
}

// A denying authorizer, so "the share is what let this through" is provable rather
// than merely consistent with the result.
