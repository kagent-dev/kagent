package a2agateway

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/agentinstance"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const gatewayTestAgent = "team-a/assistant"

type gatewayTestAgents struct {
	store      *gatewayTestStore
	authorizer auth.Authorizer
}

func (s gatewayTestAgents) Get(ctx context.Context, ref types.NamespacedName) (*v1alpha3.Agent, error) {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok {
		return nil, serviceerrors.NewUnauthenticated("authentication required", nil)
	}
	if err := s.authorizer.Check(ctx, session.Principal(), auth.VerbGet, auth.Resource{Type: "Agent", Name: ref.String()}); err != nil {
		return nil, serviceerrors.NewPermissionDenied("not authorized", err)
	}
	if s.store.err != nil {
		if s.store.err == database.ErrNotFound {
			return nil, serviceerrors.NewNotFound("Agent not found", s.store.err)
		}
		return nil, s.store.err
	}
	return &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: ref.Namespace, Name: ref.Name}, Status: v1alpha3.AgentStatus{LatestSuccessfulRevision: "revision-1"}}, nil
}

type gatewayTestInstances struct{ store *gatewayTestStore }

func (s gatewayTestInstances) Create(context.Context, *apiv1alpha1.ResourceReference, string, string) (*apiv1alpha1.AgentInstance, error) {
	return s.store.instance, s.store.err
}

func (s gatewayTestInstances) List(context.Context, agentinstance.ListRequest) (agentinstance.ListResult, error) {
	return agentinstance.ListResult{Instances: []*apiv1alpha1.AgentInstance{s.store.instance}}, s.store.err
}

func newTestGateway(store *gatewayTestStore, authorizer auth.Authorizer, dialer runtimeDialer, url string) a2asrv.RequestHandler {
	return New(Config{Store: store, Authorizer: authorizer, Dialer: dialer, GatewayURL: url,
		Agents: gatewayTestAgents{store, authorizer}, Instances: gatewayTestInstances{store}})
}

func (s *gatewayTestStore) AgentInstanceForTask(context.Context, string) (string, error) {
	if s.taskErr != nil {
		return "", s.taskErr
	}
	if s.instance == nil {
		return "", database.ErrNotFound
	}
	return s.instance.Id, nil
}

// Creation idempotency belongs to the instance service; the gateway supplies a
// stable request key scoped to the Agent and initial message.
type creatingTestInstances struct {
	gatewayTestInstances
	created map[string]*apiv1alpha1.AgentInstance
}

func (s *creatingTestInstances) Create(_ context.Context, ref *apiv1alpha1.ResourceReference, requestID, _ string) (*apiv1alpha1.AgentInstance, error) {
	if existing := s.created[requestID]; existing != nil {
		return existing, nil
	}
	instance := gatewayTestInstance()
	instance.Id = uuid.NewString()
	instance.ContextId, instance.Agent = instance.Id, ref
	s.created[requestID] = instance
	s.store.instance = instance
	return instance, nil
}

func TestGatewayCreatesConversationAndReusesInitialMessage(t *testing.T) {
	store := &gatewayTestStore{}
	instances := &creatingTestInstances{gatewayTestInstances{store}, map[string]*apiv1alpha1.AgentInstance{}}
	runtime := &gatewayTestRuntime{}
	authorizer := &gatewayTestAuthorizer{}
	gateway := New(Config{Store: store, Authorizer: authorizer,
		Agents: gatewayTestAgents{store, authorizer}, Instances: instances,
		Dialer: &gatewayTestDialer{client: gatewayTestClient(t, runtime)}})
	request := func(id string) *a2a.SendMessageRequest {
		message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
		message.ID = id
		return &a2a.SendMessageRequest{Tenant: gatewayTestAgent, Message: message, Config: &a2a.SendMessageConfig{ReturnImmediately: true}}
	}
	first, err := gateway.SendMessage(gatewayTestContext(), request("first"))
	require.NoError(t, err)
	task := first.(*a2a.Task)
	require.Equal(t, store.instance.Id, task.ContextID)
	require.Equal(t, "first", store.initialID)
	store.reserveErr, store.replay = database.ErrMessageAccepted, task
	retry, err := gateway.SendMessage(gatewayTestContext(), request("first"))
	require.NoError(t, err)
	require.Equal(t, first, retry)
	require.Equal(t, 1, runtime.sendCalls)
	require.Len(t, instances.created, 1)
	store.reserveErr = nil
	second, err := gateway.SendMessage(gatewayTestContext(), request("second"))
	require.NoError(t, err)
	require.NotEqual(t, task.ContextID, second.(*a2a.Task).ContextID)
	require.Len(t, instances.created, 2)
}

func TestGatewayTaskRoutingAndAgentIsolation(t *testing.T) {
	for _, tc := range []struct {
		name, tenant, contextID string
		want                    error
	}{
		{"task only", gatewayTestAgent, "", nil},
		{"matching context", gatewayTestAgent, gatewayTestID, nil},
		{"conflicting context", gatewayTestAgent, uuid.NewString(), a2a.ErrInvalidRequest},
		{"different Agent", "team-a/other", "", a2a.ErrUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &gatewayTestStore{instance: gatewayTestInstance()}
			runtime := &gatewayTestRuntime{}
			gateway := newTestGateway(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, "")
			req := gatewayTestRequest()
			req.Tenant, req.Message.ContextID, req.Message.TaskID = tc.tenant, tc.contextID, "task"
			result, err := gateway.SendMessage(gatewayTestContext(), req)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.False(t, runtime.sent)
				return
			}
			require.NoError(t, err)
			require.Equal(t, gatewayTestID, result.(*a2a.Task).ContextID)
			require.Equal(t, a2a.TaskID("task"), runtime.sentTaskID)
		})
	}
}

func TestRouteURLIsAuthoritative(t *testing.T) {
	ctx := context.WithValue(t.Context(), httpAgentKey{}, gatewayTestAgent)
	_, err := route(ctx, "team-a/other")
	require.ErrorIs(t, err, a2a.ErrInvalidRequest)
	ref, err := route(ctx, "")
	require.NoError(t, err)
	require.Equal(t, "assistant", ref.Name)
}

func TestShareCannotCreateConversation(t *testing.T) {
	ctx := auth.ShareContextTo(gatewayTestContext(), &auth.ShareContext{AgentInstanceID: gatewayTestID, UserID: "alice"})
	gateway := newTestGateway(&gatewayTestStore{instance: gatewayTestInstance()}, &gatewayTestAuthorizer{}, &gatewayTestDialer{}, "")
	req := gatewayTestRequest()
	req.Message.ContextID = ""
	_, err := gateway.SendMessage(ctx, req)
	require.ErrorIs(t, err, a2a.ErrUnauthorized)
}

func TestInitialStreamingRetrySubscribesWithoutRedispatch(t *testing.T) {
	task := &a2a.Task{ID: "accepted-task", ContextID: gatewayTestID, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}
	store := &gatewayTestStore{instance: gatewayTestInstance(), task: task, replay: task, reserveErr: database.ErrMessageAccepted}
	runtime := &gatewayTestRuntime{subscribeEvent: task}
	gateway := newTestGateway(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, "")
	req := gatewayTestRequest()
	req.Message.ContextID = ""
	events := gateway.SendStreamingMessage(gatewayTestContext(), req)
	for event, err := range events {
		require.NoError(t, err)
		require.Equal(t, task.ID, event.TaskInfo().TaskID)
		break
	}
	require.Equal(t, 1, runtime.subscribeCalls)
	require.False(t, runtime.sent)
}

func TestListWithoutContextRestrictsShareToOneConversation(t *testing.T) {
	store := &gatewayTestStore{instance: gatewayTestInstance()}
	gateway := newTestGateway(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{}, "")
	ctx := auth.ShareContextTo(gatewayTestContext(), &auth.ShareContext{AgentInstanceID: gatewayTestID, UserID: "alice", ReadOnly: true})
	_, err := gateway.ListTasks(ctx, &a2a.ListTasksRequest{Tenant: gatewayTestAgent})
	require.NoError(t, err)
	require.Equal(t, []string{gatewayTestID}, store.listedIDs)
}
