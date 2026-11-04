package session

import (
	"context"
	"errors"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
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
	parent := &apiv1.Session{Id: uuid.NewString(), Creator: "owner", Agent: &apiv1.AgentProvenance{Ref: &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"}}}
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

type delegationAgents struct{}

func (delegationAgents) Get(context.Context, types.NamespacedName) (*v1alpha3.Agent, error) {
	return &v1alpha3.Agent{}, nil
}

// delegationStore records admission writes in order.
type delegationStore struct {
	*interactionTestStore
	events      []string
	registerErr error
}

func (s *delegationStore) RegisterTaskContinuation(_ context.Context, parentID, childID, messageID string) error {
	s.events = append(s.events, "register "+parentID+" "+childID+" "+messageID)
	return s.registerErr
}

func (s *delegationStore) ReserveSessionDispatch(_ context.Context, sessionID string, _ uuid.UUID, _ string) error {
	s.events = append(s.events, "dispatch "+sessionID)
	return nil
}

type delegationFixture struct {
	t          *testing.T
	agent      types.NamespacedName
	existing   *apiv1.Session
	store      *delegationStore
	sessions   *serviceTestStore
	authorizer *recordingAuthorizer
	ctx        context.Context
	prepared   *PreparedSend
	err        error
}

func newDelegationFixture(t *testing.T) *delegationFixture {
	agent := types.NamespacedName{Namespace: "team-a", Name: "assistant"}
	id := uuid.NewString()
	existing := &apiv1.Session{Id: id, ContextId: id, State: apiv1.RuntimeState_RUNTIME_STATE_READY,
		Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE,
		Agent:     &apiv1.AgentProvenance{Ref: &apiv1.ResourceReference{Namespace: agent.Namespace, Name: agent.Name}}}
	return &delegationFixture{
		t: t, agent: agent, existing: existing,
		store:      &delegationStore{interactionTestStore: &interactionTestStore{}},
		sessions:   &serviceTestStore{},
		authorizer: &recordingAuthorizer{denied: map[string]bool{}},
		ctx:        serviceTestContext("owner"),
	}
}

func (f *delegationFixture) GivenDelegatingSession(id string) *delegationFixture {
	f.ctx = WithDelegatingSession(f.ctx, id)
	return f
}

func (f *delegationFixture) GivenDeniedSession(id string) *delegationFixture {
	f.authorizer.denied[id] = true
	return f
}

func (f *delegationFixture) WhenSendStartsSession(messageID string) *delegationFixture {
	return f.send(&a2a.Message{ID: messageID, Role: a2a.MessageRoleUser, Parts: []*a2a.Part{a2a.NewTextPart("hello")}})
}

func (f *delegationFixture) WhenSendContinuesSession(messageID string) *delegationFixture {
	f.sessions.getResult = f.existing
	return f.send(&a2a.Message{ID: messageID, ContextID: f.existing.Id, Role: a2a.MessageRoleUser, Parts: []*a2a.Part{a2a.NewTextPart("hello")}})
}

func (f *delegationFixture) send(message *a2a.Message) *delegationFixture {
	sessions := NewService(f.sessions, f.authorizer, serviceTestWorkflow{})
	service := NewInteractionService(f.store, delegationAgents{}, sessions, false)
	f.prepared, f.err = service.PrepareSend(f.ctx, f.agent, &a2a.SendMessageRequest{Message: message})
	return f
}

func (f *delegationFixture) AssertThatDependencyPrecedesDispatch(parentID, messageID string) {
	f.t.Helper()
	require.NoError(f.t, f.err)
	child := f.prepared.Session.Id
	require.Equal(f.t, []string{"register " + parentID + " " + child + " " + messageID, "dispatch " + child}, f.store.events)
}

func (f *delegationFixture) AssertThatNoDependencyIsRecorded() {
	f.t.Helper()
	require.NoError(f.t, f.err)
	require.Equal(f.t, []string{"dispatch " + f.prepared.Session.Id}, f.store.events)
}

func (f *delegationFixture) AssertThatSendIsRejectedBeforeDispatch(want error) {
	f.t.Helper()
	require.ErrorIs(f.t, f.err, want)
	require.Empty(f.t, f.store.events)
}

func TestAdmissionRecordsDelegation(t *testing.T) {
	parent := uuid.NewString()
	t.Run("Given a delegating Session, when a send starts a Session for an Agent, assert that the dependency on the new Session precedes dispatch", func(t *testing.T) {
		newDelegationFixture(t).GivenDelegatingSession(parent).WhenSendStartsSession("first").AssertThatDependencyPrecedesDispatch(parent, "first")
	})
	t.Run("Given a delegating Session, when a send continues a Session, assert that the dependency on that Session precedes dispatch", func(t *testing.T) {
		newDelegationFixture(t).GivenDelegatingSession(parent).WhenSendContinuesSession("next").AssertThatDependencyPrecedesDispatch(parent, "next")
	})
	t.Run("Given no delegating Session, when a send starts a Session, assert that no dependency is recorded", func(t *testing.T) {
		newDelegationFixture(t).WhenSendStartsSession("first").AssertThatNoDependencyIsRecorded()
	})
	t.Run("Given a delegating Session the caller cannot read, when a send starts a Session, assert that admission rejects it before dispatch", func(t *testing.T) {
		newDelegationFixture(t).GivenDelegatingSession(parent).GivenDeniedSession(parent).WhenSendStartsSession("first").AssertThatSendIsRejectedBeforeDispatch(a2a.ErrUnauthorized)
	})
	t.Run("Given a delegating Session without a working task, when a send starts a Session, assert that admission rejects it before dispatch", func(t *testing.T) {
		fixture := newDelegationFixture(t).GivenDelegatingSession(parent)
		fixture.store.registerErr = database.ErrNotFound
		fixture.WhenSendStartsSession("first")
		require.ErrorIs(t, fixture.err, a2a.ErrTaskNotFound)
		require.Len(t, fixture.store.events, 1, "no dispatch follows a failed registration")
	})
}

func TestContinuationMessageIDPrefixIsReserved(t *testing.T) {
	id := continuationMessageIDPrefix + uuid.NewString()
	t.Run("Given a user caller, when a send starts a Session with a continuation message ID, assert that admission rejects it", func(t *testing.T) {
		fixture := newDelegationFixture(t).WhenSendStartsSession(id)
		fixture.AssertThatSendIsRejectedBeforeDispatch(a2a.ErrInvalidParams)
		require.Nil(t, fixture.sessions.createInput)
	})
	t.Run("Given a user caller, when a send continues a Session with a continuation message ID, assert that admission rejects it", func(t *testing.T) {
		newDelegationFixture(t).WhenSendContinuesSession(id).AssertThatSendIsRejectedBeforeDispatch(a2a.ErrInvalidParams)
	})
}
