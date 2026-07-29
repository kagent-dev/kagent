package grpcserver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"testing"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	agentservice "github.com/kagent-dev/kagent/go/core/internal/service/agent"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type agentTestAuthenticator struct{}

func (*agentTestAuthenticator) Authenticate(_ context.Context, headers http.Header, _ url.Values) (pkgauth.Session, error) {
	if headers.Get("X-User-Id") != "caller" {
		return nil, errors.New("missing caller")
	}
	return &authimpl.SimpleSession{P: pkgauth.Principal{User: pkgauth.User{ID: "caller"}}}, nil
}

func (*agentTestAuthenticator) UpstreamAuth(*http.Request, pkgauth.Session, pkgauth.Principal) error {
	return nil
}

type agentTestActorLifecycle struct {
	state     substrate.SessionActorState
	sessionID string
}

func (l *agentTestActorLifecycle) EnsureSessionActor(_ context.Context, _ *v1alpha2.AgentHarness, sessionID string) (sandboxbackend.EnsureResult, error) {
	l.sessionID = sessionID
	l.state = substrate.SessionActorStateRunning
	return sandboxbackend.EnsureResult{Handle: sandboxbackend.Handle{ID: "actor-1"}}, nil
}

func (l *agentTestActorLifecycle) SuspendSessionActor(_ context.Context, _ *v1alpha2.AgentHarness, sessionID string) error {
	l.sessionID = sessionID
	l.state = substrate.SessionActorStateSuspended
	return nil
}

func (l *agentTestActorLifecycle) GetSessionActorState(_ context.Context, _ *v1alpha2.AgentHarness, sessionID string) (substrate.SessionActorState, error) {
	l.sessionID = sessionID
	return l.state, nil
}

func TestAgentServiceGeneratedClient(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "unsupported", Namespace: "default"},
		Spec:       v1alpha2.AgentHarnessSpec{Backend: v1alpha2.AgentHarnessBackendType("unsupported")},
	}, &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "model", Namespace: "default"},
		Spec: v1alpha2.ModelConfigSpec{
			Provider: v1alpha2.ModelProviderOpenAI,
			Model:    "gpt-4.1",
		},
	}).Build()
	lifecycle := &agentTestActorLifecycle{state: substrate.SessionActorStateMissing}
	service := agentservice.NewService(
		kubeClient,
		&authimpl.NoopAuthorizer{},
		"default",
		agentservice.WithValidator(func(context.Context, v1alpha2.AgentObject) error { return nil }),
		agentservice.WithActorLifecycle(lifecycle),
	)

	listener := bufconn.Listen(1024 * 1024)
	server, err := New(Config{
		Listener:      listener,
		Registerer:    prometheus.NewRegistry(),
		Authenticator: &agentTestAuthenticator{},
		AgentService:  service,
	})
	require.NoError(t, err)
	serverContext, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Start(serverContext) }()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done)
	})

	connection, err := grpc.NewClient(
		"passthrough:///agent-service",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	client := apiv1alpha1.NewAgentServiceClient(connection)
	authenticatedContext := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "caller"))
	sharedRef := &apiv1alpha1.ResourceReference{Namespace: "default", Name: "shared"}

	_, err = client.ListAgents(t.Context(), &apiv1alpha1.ListAgentsRequest{})
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	_, err = client.ListAgents(authenticatedContext, &apiv1alpha1.ListAgentsRequest{Namespace: " bad "})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	wrongKind := agentTestResource(t, "SandboxAgent", &v1alpha2.SandboxAgent{})
	_, err = client.CreateAgent(authenticatedContext, &apiv1alpha1.CreateAgentRequest{Ref: sharedRef, Resource: wrongKind})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	createdAgent, err := client.CreateAgent(authenticatedContext, &apiv1alpha1.CreateAgentRequest{
		Ref: sharedRef,
		Resource: agentTestResource(t, "Agent", &v1alpha2.Agent{Spec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_BYO,
			BYO:         &v1alpha2.BYOAgentSpec{},
			Description: "regular",
		}}),
	})
	require.NoError(t, err)
	assert.Equal(t, apiv1alpha1.AgentKind_AGENT_KIND_AGENT, createdAgent.GetAgent().GetKind())
	assert.Equal(t, apiv1alpha1.WorkloadMode_WORKLOAD_MODE_DEPLOYMENT, createdAgent.GetAgent().GetWorkloadMode())
	assertAgentResourceDescription(t, createdAgent.GetAgent(), "Agent", "regular")
	_, err = client.CreateAgent(authenticatedContext, &apiv1alpha1.CreateAgentRequest{
		Ref: sharedRef,
		Resource: agentTestResource(t, "Agent", &v1alpha2.Agent{Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_BYO,
			BYO:  &v1alpha2.BYOAgentSpec{},
		}}),
	})
	require.Equal(t, codes.AlreadyExists, status.Code(err))

	createdSandbox, err := client.CreateSandboxAgent(authenticatedContext, &apiv1alpha1.CreateSandboxAgentRequest{
		Ref: sharedRef,
		Resource: agentTestResource(t, "SandboxAgent", &v1alpha2.SandboxAgent{Spec: v1alpha2.SandboxAgentSpec{
			AgentSpec: v1alpha2.AgentSpec{
				Type:        v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{ModelConfig: "model"},
				Description: "sandbox",
			},
		}}),
	})
	require.NoError(t, err)
	assert.Equal(t, apiv1alpha1.AgentKind_AGENT_KIND_SANDBOX_AGENT, createdSandbox.GetAgent().GetKind())
	assert.Equal(t, apiv1alpha1.WorkloadMode_WORKLOAD_MODE_SANDBOX, createdSandbox.GetAgent().GetWorkloadMode())
	assert.Equal(t, "gpt-4.1", createdSandbox.GetAgent().GetModel())
	assert.Equal(t, "model", createdSandbox.GetAgent().GetModelConfigRef().GetName())
	assertSandboxAgentResourceDescription(t, createdSandbox.GetAgent(), "sandbox")

	createdHarness, err := client.CreateAgentHarness(authenticatedContext, &apiv1alpha1.CreateAgentHarnessRequest{
		Ref: sharedRef,
		Resource: agentTestResource(t, "AgentHarness", &v1alpha2.AgentHarness{Spec: v1alpha2.AgentHarnessSpec{
			Backend:     v1alpha2.AgentHarnessBackendOpenClaw,
			Substrate:   &v1alpha2.AgentHarnessSubstrateSpec{},
			Description: "harness",
		}}),
	})
	require.NoError(t, err)
	assert.Equal(t, apiv1alpha1.AgentKind_AGENT_KIND_AGENT_HARNESS, createdHarness.GetAgent().GetKind())
	assert.Equal(t, "openclaw", createdHarness.GetAgent().GetAgentHarness().GetBackend())

	listed, err := client.ListAgents(authenticatedContext, &apiv1alpha1.ListAgentsRequest{Namespace: "default"})
	require.NoError(t, err)
	require.Len(t, listed.GetAgents(), 3)
	kinds := make(map[apiv1alpha1.AgentKind]bool, len(listed.GetAgents()))
	for _, listedAgent := range listed.GetAgents() {
		kinds[listedAgent.GetKind()] = true
		assert.Equal(t, "shared", listedAgent.GetRef().GetName())
	}
	assert.True(t, kinds[apiv1alpha1.AgentKind_AGENT_KIND_AGENT])
	assert.True(t, kinds[apiv1alpha1.AgentKind_AGENT_KIND_SANDBOX_AGENT])
	assert.True(t, kinds[apiv1alpha1.AgentKind_AGENT_KIND_AGENT_HARNESS])

	gotAgent, err := client.GetAgent(authenticatedContext, &apiv1alpha1.GetAgentRequest{Ref: sharedRef})
	require.NoError(t, err)
	assert.Equal(t, apiv1alpha1.AgentKind_AGENT_KIND_AGENT, gotAgent.GetAgent().GetKind())
	gotSandbox, err := client.GetSandboxAgent(authenticatedContext, &apiv1alpha1.GetSandboxAgentRequest{Ref: sharedRef})
	require.NoError(t, err)
	assert.Equal(t, apiv1alpha1.AgentKind_AGENT_KIND_SANDBOX_AGENT, gotSandbox.GetAgent().GetKind())
	gotHarness, err := client.GetAgentHarness(authenticatedContext, &apiv1alpha1.GetAgentHarnessRequest{Ref: sharedRef})
	require.NoError(t, err)
	assert.Equal(t, apiv1alpha1.AgentKind_AGENT_KIND_AGENT_HARNESS, gotHarness.GetAgent().GetKind())

	_, err = client.GetAgentHarness(authenticatedContext, &apiv1alpha1.GetAgentHarnessRequest{
		Ref: &apiv1alpha1.ResourceReference{Namespace: "default", Name: "unsupported"},
	})
	require.Equal(t, codes.NotFound, status.Code(err))

	updatedAgent, err := client.UpdateAgent(authenticatedContext, &apiv1alpha1.UpdateAgentRequest{
		Ref: sharedRef,
		Resource: agentTestResource(t, "Agent", &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "default"},
			Spec: v1alpha2.AgentSpec{
				Type:        v1alpha2.AgentType_BYO,
				BYO:         &v1alpha2.BYOAgentSpec{},
				Description: "regular-updated",
			},
		}),
	})
	require.NoError(t, err)
	assertAgentResourceDescription(t, updatedAgent.GetAgent(), "Agent", "regular-updated")

	updatedSandbox, err := client.UpdateSandboxAgent(authenticatedContext, &apiv1alpha1.UpdateSandboxAgentRequest{
		Ref: sharedRef,
		Resource: agentTestResource(t, "SandboxAgent", &v1alpha2.SandboxAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "default"},
			Spec: v1alpha2.SandboxAgentSpec{AgentSpec: v1alpha2.AgentSpec{
				Type:        v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{ModelConfig: "model"},
				Description: "sandbox-updated",
			}},
		}),
	})
	require.NoError(t, err)
	assertSandboxAgentResourceDescription(t, updatedSandbox.GetAgent(), "sandbox-updated")

	ensured, err := client.EnsureAgentHarnessSessionActor(authenticatedContext, &apiv1alpha1.EnsureAgentHarnessSessionActorRequest{
		Ref: sharedRef, SessionId: " session-1 ",
	})
	require.NoError(t, err)
	assert.Equal(t, "session-1", ensured.GetActor().GetSessionId())
	assert.Equal(t, "session-1", lifecycle.sessionID)
	assert.Equal(t, "actor-1", ensured.GetActor().GetActorId())
	assert.Equal(t, apiv1alpha1.AgentHarnessActorState_AGENT_HARNESS_ACTOR_STATE_RUNNING, ensured.GetActor().GetState())

	actorState, err := client.GetAgentHarnessSessionActor(authenticatedContext, &apiv1alpha1.GetAgentHarnessSessionActorRequest{
		Ref: sharedRef, SessionId: "session-1",
	})
	require.NoError(t, err)
	assert.Equal(t, apiv1alpha1.AgentHarnessActorState_AGENT_HARNESS_ACTOR_STATE_RUNNING, actorState.GetActor().GetState())

	suspended, err := client.SuspendAgentHarnessSessionActor(authenticatedContext, &apiv1alpha1.SuspendAgentHarnessSessionActorRequest{
		Ref: sharedRef, SessionId: "session-1",
	})
	require.NoError(t, err)
	assert.Equal(t, apiv1alpha1.AgentHarnessActorState_AGENT_HARNESS_ACTOR_STATE_SUSPENDED, suspended.GetActor().GetState())

	_, err = client.DeleteAgent(authenticatedContext, &apiv1alpha1.DeleteAgentRequest{Ref: sharedRef})
	require.NoError(t, err)
	_, err = client.GetAgent(authenticatedContext, &apiv1alpha1.GetAgentRequest{Ref: sharedRef})
	require.Equal(t, codes.NotFound, status.Code(err))
	_, err = client.GetSandboxAgent(authenticatedContext, &apiv1alpha1.GetSandboxAgentRequest{Ref: sharedRef})
	require.NoError(t, err)
	_, err = client.GetAgentHarness(authenticatedContext, &apiv1alpha1.GetAgentHarnessRequest{Ref: sharedRef})
	require.NoError(t, err)

	_, err = client.DeleteSandboxAgent(authenticatedContext, &apiv1alpha1.DeleteSandboxAgentRequest{Ref: sharedRef})
	require.NoError(t, err)
	_, err = client.DeleteAgentHarness(authenticatedContext, &apiv1alpha1.DeleteAgentHarnessRequest{Ref: sharedRef})
	require.NoError(t, err)
	listed, err = client.ListAgents(authenticatedContext, &apiv1alpha1.ListAgentsRequest{Namespace: "default"})
	require.NoError(t, err)
	assert.Empty(t, listed.GetAgents())
}

func agentTestResource(t *testing.T, kind string, object any) *apiv1alpha1.StructuredObject {
	t.Helper()
	resource, err := structuredobject.FromGo(object, v1alpha2.GroupVersion.String(), kind, DefaultMaxMessageSize)
	require.NoError(t, err)
	return resource
}

func assertAgentResourceDescription(t *testing.T, response *apiv1alpha1.Agent, kind, description string) {
	t.Helper()
	resource := &v1alpha2.Agent{}
	require.NoError(t, structuredobject.ToGo(response.GetResource(), kind, resource, DefaultMaxMessageSize))
	assert.Equal(t, "default", resource.Namespace)
	assert.Equal(t, "shared", resource.Name)
	assert.Equal(t, description, resource.Spec.Description)
}

func assertSandboxAgentResourceDescription(t *testing.T, response *apiv1alpha1.Agent, description string) {
	t.Helper()
	resource := &v1alpha2.SandboxAgent{}
	require.NoError(t, structuredobject.ToGo(response.GetResource(), "SandboxAgent", resource, DefaultMaxMessageSize))
	assert.Equal(t, "default", resource.Namespace)
	assert.Equal(t, "shared", resource.Name)
	assert.Equal(t, description, resource.Spec.Description)
}
