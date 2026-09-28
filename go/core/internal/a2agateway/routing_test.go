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
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
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
	authSession, ok := auth.AuthSessionFrom(ctx)
	if !ok {
		return nil, serviceerrors.NewUnauthenticated("authentication required", nil)
	}
	if err := s.authorizer.Check(ctx, authSession.Principal(), auth.VerbGet, auth.Resource{Type: "Agent", Name: ref.String()}); err != nil {
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

type gatewayTestWorkflow struct{ *sessionsvc.ActorWorkflow }

func (gatewayTestWorkflow) Create(_ context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	return session, nil
}

func newTestSessions(store *gatewayTestStore, authorizer auth.Authorizer) *sessionsvc.Service {
	return sessionsvc.NewService(store, authorizer, gatewayTestWorkflow{})
}

func newTestGateway(store *gatewayTestStore, authorizer auth.Authorizer, dialer *gatewayTestDialer, url string) a2asrv.RequestHandler {
	interactions := sessionsvc.NewInteractionService(store, gatewayTestAgents{store, authorizer}, newTestSessions(store, authorizer))
	return New(interactions, dialer, url)
}

func (s *gatewayTestStore) SessionForTask(context.Context, string) (string, error) {
	if s.taskErr != nil {
		return "", s.taskErr
	}
	if s.session == nil {
		return "", database.ErrNotFound
	}
	return s.session.Id, nil
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
			store := &gatewayTestStore{session: gatewayTestSession()}
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

func TestShareCannotCreateConversation(t *testing.T) {
	ctx := auth.ShareContextTo(gatewayTestContext(), &auth.ShareContext{SessionID: gatewayTestID, UserID: "alice"})
	gateway := newTestGateway(&gatewayTestStore{session: gatewayTestSession()}, &gatewayTestAuthorizer{}, &gatewayTestDialer{}, "")
	req := gatewayTestRequest()
	req.Message.ContextID = ""
	_, err := gateway.SendMessage(ctx, req)
	require.ErrorIs(t, err, a2a.ErrUnauthorized)
}

func TestInitialStreamingRetrySubscribesWithoutRedispatch(t *testing.T) {
	task := &a2a.Task{ID: "accepted-task", ContextID: gatewayTestID, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}
	store := &gatewayTestStore{session: gatewayTestSession(), task: task, replay: task, reserveErr: database.ErrMessageAccepted}
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
	store := &gatewayTestStore{session: gatewayTestSession()}
	gateway := newTestGateway(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{}, "")
	ctx := auth.ShareContextTo(gatewayTestContext(), &auth.ShareContext{SessionID: gatewayTestID, UserID: "alice", ReadOnly: true})
	_, err := gateway.ListTasks(ctx, &a2a.ListTasksRequest{Tenant: gatewayTestAgent})
	require.NoError(t, err)
	require.Equal(t, []string{gatewayTestID}, store.listedIDs)
}
