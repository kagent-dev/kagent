package session

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
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

func TestDisableSessionSharingPreservesOwnerAdmission(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors)
	session, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)
	owner := serviceTestContext("alice")
	agent := types.NamespacedName{Namespace: session.Agent.Namespace, Name: session.Agent.Name}
	disabled := NewService(store, auth.NoopAuthorizer{}, workflow)
	_, token, err := disabled.CreateShare(owner, session.Id, apiv1alpha1.SessionSharePermission_SESSION_SHARE_PERMISSION_READ_WRITE, 0)
	require.NoError(t, err)
	share, err := ResolveShare(t.Context(), store, token)
	require.NoError(t, err)
	visitor := auth.ShareContextTo(serviceTestContext("bob"), share)
	interactions := NewInteractionService(store, nil, NewService(store, auth.NoopAuthorizer{}, workflow, WithDisableSessionSharing(true)))
	input := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hello"))
	input.ContextID = session.ContextId
	request := &a2atype.SendMessageRequest{Message: input}
	_, err = interactions.PrepareSend(visitor, agent, request)
	require.ErrorIs(t, err, a2atype.ErrUnauthorized)
	_, err = interactions.PrepareSend(serviceTestContext("bob"), agent, request)
	require.ErrorIs(t, err, a2atype.ErrUnauthorized)

	observer, disconnect := context.WithCancel(owner)
	t.Cleanup(disconnect)
	prepared, err := interactions.PrepareSend(observer, agent, request)
	require.NoError(t, err)
	task := a2atype.NewSubmittedTask(input, input)
	digest := sha256.Sum256([]byte("accepted"))
	version, err := store.CreateRuntimeTask(t.Context(), session.Id, digest[:], task, prepared.DispatchID.String())
	require.NoError(t, err)
	disconnect()
	revoked, err := interactions.RevokeSend(context.WithoutCancel(observer), agent, input, prepared.DispatchID)
	require.NoError(t, err)
	require.False(t, revoked, "observer cleanup must not revoke accepted execution")
	next := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("next"))
	next.ContextID = session.ContextId
	_, err = interactions.PrepareSend(owner, agent, &a2atype.SendMessageRequest{Message: next})
	require.ErrorIs(t, err, a2atype.ErrUnsupportedOperation, "accepted execution must still block another input")

	task.Status.State = a2atype.TaskStateInputRequired
	digest = sha256.Sum256([]byte("waiting"))
	version, err = store.UpdateSessionTask(t.Context(), session.Id, version, digest[:], task, task, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
	reply := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("approved"))
	reply.ContextID, reply.TaskID = session.ContextId, task.ID
	prepared, err = interactions.PrepareSend(owner, agent, &a2atype.SendMessageRequest{Message: reply})
	require.NoError(t, err, "the owner can continue without a checkpoint")
	task.History = append(task.History, reply)
	task.Status.State = a2atype.TaskStateWorking
	digest = sha256.Sum256([]byte("continued"))
	version, err = store.UpdateSessionTask(t.Context(), session.Id, version, digest[:], task, task, prepared.DispatchID.String())
	require.NoError(t, err)
	task.Status.State = a2atype.TaskStateCompleted
	digest = sha256.Sum256([]byte("completed"))
	version, err = store.UpdateSessionTask(t.Context(), session.Id, version, digest[:], task, task, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))

	_, err = interactions.PrepareSend(visitor, agent, &a2atype.SendMessageRequest{Message: next})
	require.ErrorIs(t, err, a2atype.ErrUnauthorized, "completion must not change the session owner")
	prepared, err = interactions.PrepareSend(owner, agent, &a2atype.SendMessageRequest{Message: next})
	require.NoError(t, err, "the owner can start a sequential task without a checkpoint")
	revoked, err = interactions.RevokeSend(owner, agent, next, prepared.DispatchID)
	require.NoError(t, err)
	require.True(t, revoked)

	interactions = NewInteractionService(store, nil, disabled)
	prepared, err = interactions.PrepareSend(visitor, agent, &a2atype.SendMessageRequest{Message: next})
	require.NoError(t, err, "enabling sharing must restore the existing share")
	revoked, err = interactions.RevokeSend(visitor, agent, next, prepared.DispatchID)
	require.NoError(t, err)
	require.True(t, revoked)
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
			name           string
			ctx            context.Context
			agent          types.NamespacedName
			want           error
			disableSharing bool
		}{
			{name: "unauthenticated", ctx: context.Background(), agent: agent, want: a2atype.ErrUnauthenticated},
			{name: "missing Agent", ctx: serviceTestContext("visitor"), want: a2atype.ErrInvalidRequest},
			{name: "denied caller", ctx: serviceTestContext("visitor"), agent: agent, want: a2atype.ErrUnauthorized},
			{name: "read-only share", ctx: auth.ShareContextTo(serviceTestContext("visitor"), &auth.ShareContext{SessionID: id, UserID: "owner", ReadOnly: true}), agent: agent},
			{name: "other Agent", ctx: auth.ShareContextTo(serviceTestContext("visitor"), &auth.ShareContext{SessionID: id, UserID: "owner"}), agent: types.NamespacedName{Namespace: "team-a", Name: "other"}, want: a2atype.ErrUnauthorized},
			{name: "sharing disabled shared reader", ctx: auth.ShareContextTo(serviceTestContext("visitor"), &auth.ShareContext{SessionID: id, UserID: "owner", ReadOnly: true}), agent: agent, disableSharing: true, want: a2atype.ErrUnauthorized},
			{name: "sharing disabled shared writer", ctx: auth.ShareContextTo(serviceTestContext("visitor"), &auth.ShareContext{SessionID: id, UserID: "owner"}), agent: agent, disableSharing: true, want: a2atype.ErrUnauthorized},
		} {
			t.Run(operation.name+"/"+test.name, func(t *testing.T) {
				stored := &apiv1alpha1.Session{Id: id, ContextId: id, State: apiv1alpha1.RuntimeState_RUNTIME_STATE_SUSPENDED,
					Agent: &apiv1alpha1.ResourceReference{Namespace: agent.Namespace, Name: agent.Name}}
				sessionStore := &serviceTestStore{getResult: stored}
				authorizer := &recordingAuthorizer{denied: map[string]bool{id: true}}
				tasks := &interactionTestStore{task: &a2atype.Task{ID: "task", ContextID: id, Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}}
				service := NewInteractionService(tasks, nil, NewService(sessionStore, authorizer, nil, WithDisableSessionSharing(test.disableSharing)))
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
