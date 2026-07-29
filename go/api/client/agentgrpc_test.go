package client

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type recordingAgentService struct {
	apiv1alpha1.UnimplementedAgentServiceServer

	mu            sync.Mutex
	observations  []callObservation
	listRequests  []*apiv1alpha1.ListAgentsRequest
	getAgent      *apiv1alpha1.GetAgentRequest
	getSandbox    *apiv1alpha1.GetSandboxAgentRequest
	getHarness    *apiv1alpha1.GetAgentHarnessRequest
	createRequest *apiv1alpha1.CreateAgentRequest
	updateRequest *apiv1alpha1.UpdateAgentRequest
	deleteRequest *apiv1alpha1.DeleteAgentRequest
	agent         *apiv1alpha1.Agent
	sandboxAgent  *apiv1alpha1.Agent
	agentHarness  *apiv1alpha1.Agent
}

func (s *recordingAgentService) observe(ctx context.Context) {
	metadataValues, _ := metadata.FromIncomingContext(ctx)
	_, hasDeadline := ctx.Deadline()
	s.observations = append(s.observations, callObservation{
		userID:      first(metadataValues.Get("x-user-id")),
		hasDeadline: hasDeadline,
	})
}

func (s *recordingAgentService) ListAgents(ctx context.Context, request *apiv1alpha1.ListAgentsRequest) (*apiv1alpha1.ListAgentsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observe(ctx)
	s.listRequests = append(s.listRequests, request)
	return &apiv1alpha1.ListAgentsResponse{Agents: []*apiv1alpha1.Agent{s.agent, s.sandboxAgent, s.agentHarness}}, nil
}

func (s *recordingAgentService) GetAgent(ctx context.Context, request *apiv1alpha1.GetAgentRequest) (*apiv1alpha1.GetAgentResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observe(ctx)
	s.getAgent = request
	return &apiv1alpha1.GetAgentResponse{Agent: s.agent}, nil
}

func (s *recordingAgentService) GetSandboxAgent(ctx context.Context, request *apiv1alpha1.GetSandboxAgentRequest) (*apiv1alpha1.GetSandboxAgentResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observe(ctx)
	s.getSandbox = request
	return &apiv1alpha1.GetSandboxAgentResponse{Agent: s.sandboxAgent}, nil
}

func (s *recordingAgentService) GetAgentHarness(ctx context.Context, request *apiv1alpha1.GetAgentHarnessRequest) (*apiv1alpha1.GetAgentHarnessResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observe(ctx)
	s.getHarness = request
	return &apiv1alpha1.GetAgentHarnessResponse{Agent: s.agentHarness}, nil
}

func (s *recordingAgentService) CreateAgent(ctx context.Context, request *apiv1alpha1.CreateAgentRequest) (*apiv1alpha1.CreateAgentResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observe(ctx)
	s.createRequest = request
	return &apiv1alpha1.CreateAgentResponse{Agent: s.agent}, nil
}

func (s *recordingAgentService) UpdateAgent(ctx context.Context, request *apiv1alpha1.UpdateAgentRequest) (*apiv1alpha1.UpdateAgentResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observe(ctx)
	s.updateRequest = request
	return &apiv1alpha1.UpdateAgentResponse{Agent: s.agent}, nil
}

func (s *recordingAgentService) DeleteAgent(ctx context.Context, request *apiv1alpha1.DeleteAgentRequest) (*apiv1alpha1.DeleteAgentResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observe(ctx)
	s.deleteRequest = request
	return &apiv1alpha1.DeleteAgentResponse{}, nil
}

func TestAgentClientUsesGeneratedGRPC(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	service := &recordingAgentService{
		agent:        testAgentMessage(t),
		sandboxAgent: testSandboxAgentMessage(t),
		agentHarness: testAgentHarnessMessage(t),
	}
	server := grpc.NewServer()
	apiv1alpha1.RegisterAgentServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	var dialCount atomic.Int32
	clientSet := New(
		"http://rest-must-not-be-used.invalid",
		WithUserID("test-user"),
		WithGRPCTarget("passthrough:///bufnet"),
		WithGRPCTimeout(5*time.Second),
		WithGRPCDialOptions(grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			dialCount.Add(1)
			return listener.Dial()
		})),
	)
	t.Cleanup(func() { require.NoError(t, clientSet.Close()) })

	listed, err := clientSet.Agent.ListAgents(t.Context(), ListAgentsOptions{Namespace: "default"})
	require.NoError(t, err)
	require.Len(t, listed.Data, 3)
	assert.Equal(t, "Successfully listed agents", listed.Message)
	assert.Equal(t, "Agent", listed.Data[0].Agent.Kind)
	assert.Equal(t, "regular", listed.Data[0].Agent.Spec.Description)
	assert.Equal(t, "SandboxAgent", listed.Data[1].Agent.Kind)
	assert.Equal(t, v1alpha2.WorkloadModeSandbox, listed.Data[1].WorkloadMode)
	assert.Equal(t, "AgentHarness", listed.Data[2].Agent.Kind)
	assert.Equal(t, "harness", listed.Data[2].Agent.Spec.Description)
	require.NotNil(t, listed.Data[2].SubstrateAgentHarness)
	assert.Equal(t, "actor-1", listed.Data[2].SubstrateAgentHarness.ActorID)
	assert.Equal(t, "/api/agentharnesses/default/harness/acp", listed.Data[2].SubstrateAgentHarness.AcpPath)

	gotAgent, err := clientSet.Agent.GetAgent(t.Context(), "default/regular")
	require.NoError(t, err)
	assert.Equal(t, "Agent", gotAgent.Data.Agent.Kind)
	gotSandbox, err := clientSet.Agent.GetAgent(t.Context(), "default/sandbox")
	require.NoError(t, err)
	assert.Equal(t, "SandboxAgent", gotSandbox.Data.Agent.Kind)
	gotHarness, err := clientSet.Agent.GetAgent(t.Context(), "default/harness")
	require.NoError(t, err)
	assert.Equal(t, "AgentHarness", gotHarness.Data.Agent.Kind)

	request := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "regular", Namespace: "default"},
		Spec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_BYO,
			BYO:         &v1alpha2.BYOAgentSpec{},
			Description: "request",
		},
	}
	created, err := clientSet.Agent.CreateAgent(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, "Successfully created agent", created.Message)
	updated, err := clientSet.Agent.UpdateAgent(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, "Successfully updated agent", updated.Message)
	require.NoError(t, clientSet.Agent.DeleteAgent(t.Context(), "default/regular"))

	service.mu.Lock()
	defer service.mu.Unlock()
	require.Len(t, service.listRequests, 4)
	assert.Equal(t, "default", service.listRequests[0].GetNamespace())
	assert.Equal(t, "default", service.getAgent.GetRef().GetNamespace())
	assert.Equal(t, "regular", service.getAgent.GetRef().GetName())
	assert.Equal(t, "sandbox", service.getSandbox.GetRef().GetName())
	assert.Equal(t, "harness", service.getHarness.GetRef().GetName())
	assert.Equal(t, "regular", service.createRequest.GetRef().GetName())
	assert.Equal(t, "default", service.createRequest.GetRef().GetNamespace())
	assertRequestAgentDescription(t, service.createRequest.GetResource(), "request")
	assertRequestAgentDescription(t, service.updateRequest.GetResource(), "request")
	assert.Equal(t, "regular", service.deleteRequest.GetRef().GetName())
	require.Len(t, service.observations, 10)
	for _, observation := range service.observations {
		assert.Equal(t, "test-user", observation.userID)
		assert.True(t, observation.hasDeadline)
	}
	assert.Equal(t, int32(1), dialCount.Load())
}

func TestAgentClientValidatesRequestsBeforeCallingServer(t *testing.T) {
	clientSet := New("http://unused.invalid", WithGRPCTarget(""), WithUserID("test-user"))
	t.Cleanup(func() { _ = clientSet.Close() })

	_, err := clientSet.Agent.CreateAgent(t.Context(), nil)
	assert.Equal(t, "InvalidArgument", grpcCodeName(err))
	_, err = clientSet.Agent.UpdateAgent(t.Context(), nil)
	assert.Equal(t, "InvalidArgument", grpcCodeName(err))
	err = clientSet.Agent.DeleteAgent(t.Context(), "name-only")
	assert.Equal(t, "InvalidArgument", grpcCodeName(err))
	_, err = clientSet.Agent.ListAgents(t.Context(), ListAgentsOptions{}, ListAgentsOptions{})
	require.Error(t, err)
}

func testAgentMessage(t *testing.T) *apiv1alpha1.Agent {
	t.Helper()
	resource := agentClientTestResource(t, "Agent", &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "regular", Namespace: "default"},
		Spec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_BYO,
			BYO:         &v1alpha2.BYOAgentSpec{},
			Description: "regular",
		},
	})
	tool := agentClientTestResource(t, agentToolKind, &v1alpha2.Tool{Type: v1alpha2.ToolProviderType_Agent})
	return &apiv1alpha1.Agent{
		Ref:          &apiv1alpha1.ResourceReference{Namespace: "default", Name: "regular"},
		Kind:         apiv1alpha1.AgentKind_AGENT_KIND_AGENT,
		Resource:     resource,
		Id:           "default__NS__regular",
		Tools:        []*apiv1alpha1.StructuredObject{tool},
		WorkloadMode: apiv1alpha1.WorkloadMode_WORKLOAD_MODE_DEPLOYMENT,
	}
}

func testSandboxAgentMessage(t *testing.T) *apiv1alpha1.Agent {
	t.Helper()
	resource := agentClientTestResource(t, "SandboxAgent", &v1alpha2.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "sandbox", Namespace: "default"},
		Spec: v1alpha2.SandboxAgentSpec{AgentSpec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{ModelConfig: "model"},
			Description: "sandbox",
		}},
	})
	return &apiv1alpha1.Agent{
		Ref:            &apiv1alpha1.ResourceReference{Namespace: "default", Name: "sandbox"},
		Kind:           apiv1alpha1.AgentKind_AGENT_KIND_SANDBOX_AGENT,
		Resource:       resource,
		Model:          "gpt-test",
		ModelConfigRef: &apiv1alpha1.ResourceReference{Namespace: "default", Name: "model"},
		WorkloadMode:   apiv1alpha1.WorkloadMode_WORKLOAD_MODE_SANDBOX,
	}
}

func testAgentHarnessMessage(t *testing.T) *apiv1alpha1.Agent {
	t.Helper()
	resource := agentClientTestResource(t, "AgentHarness", &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "harness", Namespace: "default"},
		Spec: v1alpha2.AgentHarnessSpec{
			Backend:     v1alpha2.AgentHarnessBackendOpenClaw,
			Substrate:   &v1alpha2.AgentHarnessSubstrateSpec{},
			Description: "harness",
		},
	})
	return &apiv1alpha1.Agent{
		Ref:      &apiv1alpha1.ResourceReference{Namespace: "default", Name: "harness"},
		Kind:     apiv1alpha1.AgentKind_AGENT_KIND_AGENT_HARNESS,
		Resource: resource,
		AgentHarness: &apiv1alpha1.AgentHarnessDetails{
			Backend: "openclaw",
			ActorId: "actor-1",
			AcpPath: "/api/agentharnesses/default/harness/acp",
		},
	}
}

func agentClientTestResource(t *testing.T, kind string, object any) *apiv1alpha1.StructuredObject {
	t.Helper()
	resource, err := structuredobject.FromGo(object, v1alpha2.GroupVersion.String(), kind, defaultGRPCMaxMessageSize)
	require.NoError(t, err)
	return resource
}

func assertRequestAgentDescription(t *testing.T, resource *apiv1alpha1.StructuredObject, description string) {
	t.Helper()
	decoded := &v1alpha2.Agent{}
	require.NoError(t, structuredobject.ToGo(resource, "Agent", decoded, defaultGRPCMaxMessageSize))
	assert.Equal(t, description, decoded.Spec.Description)
}

func grpcCodeName(err error) string {
	return grpc.Code(err).String()
}
