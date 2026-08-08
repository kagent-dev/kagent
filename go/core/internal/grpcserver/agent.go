package grpcserver

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	agentservice "github.com/kagent-dev/kagent/go/core/internal/service/agent"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const toolKind = "Tool"

type agentServer struct {
	apiv1alpha1.UnimplementedAgentServiceServer
	service         *agentservice.Service
	maxMessageBytes int
}

func newAgentServer(service *agentservice.Service, maxMessageBytes int) *agentServer {
	return &agentServer{service: service, maxMessageBytes: maxMessageBytes}
}

func (s *agentServer) ListAgents(ctx context.Context, request *apiv1alpha1.ListAgentsRequest) (*apiv1alpha1.ListAgentsResponse, error) {
	views, err := s.service.List(ctx, agentservice.ListRequest{Namespace: request.GetNamespace()})
	if err != nil {
		return nil, err
	}
	agents := make([]*apiv1alpha1.Agent, 0, len(views))
	for _, view := range views {
		agent, err := s.agent(view)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return &apiv1alpha1.ListAgentsResponse{Agents: agents}, nil
}

func (s *agentServer) GetAgent(ctx context.Context, request *apiv1alpha1.GetAgentRequest) (*apiv1alpha1.GetAgentResponse, error) {
	view, err := s.service.GetAgent(ctx, agentservice.GetRequest{Ref: requiredAgentRef(request.GetRef())})
	if err != nil {
		return nil, err
	}
	agent, err := s.agent(view)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.GetAgentResponse{Agent: agent}, nil
}

func (s *agentServer) CreateAgent(ctx context.Context, request *apiv1alpha1.CreateAgentRequest) (*apiv1alpha1.CreateAgentResponse, error) {
	agent := &v1alpha2.Agent{}
	if err := s.decodeCreateResource(request.GetRef(), request.GetResource(), agentservice.KindAgent, agent); err != nil {
		return nil, err
	}
	view, err := s.service.CreateAgent(ctx, agentservice.CreateAgentRequest{Agent: agent})
	if err != nil {
		return nil, err
	}
	response, err := s.agent(view)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.CreateAgentResponse{Agent: response}, nil
}

func (s *agentServer) UpdateAgent(ctx context.Context, request *apiv1alpha1.UpdateAgentRequest) (*apiv1alpha1.UpdateAgentResponse, error) {
	ref, err := validatedAgentRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	agent := &v1alpha2.Agent{}
	if err := s.decodeUpdateResource(ref, request.GetResource(), agentservice.KindAgent, agent); err != nil {
		return nil, err
	}
	view, err := s.service.UpdateAgent(ctx, agentservice.UpdateAgentRequest{Ref: ref, Agent: agent})
	if err != nil {
		return nil, err
	}
	response, err := s.agent(view)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.UpdateAgentResponse{Agent: response}, nil
}

func (s *agentServer) DeleteAgent(ctx context.Context, request *apiv1alpha1.DeleteAgentRequest) (*apiv1alpha1.DeleteAgentResponse, error) {
	ref, err := validatedAgentRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	if err := s.service.DeleteAgent(ctx, agentservice.DeleteRequest{Ref: ref}); err != nil {
		return nil, err
	}
	return &apiv1alpha1.DeleteAgentResponse{}, nil
}

func (s *agentServer) GetSandboxAgent(ctx context.Context, request *apiv1alpha1.GetSandboxAgentRequest) (*apiv1alpha1.GetSandboxAgentResponse, error) {
	view, err := s.service.GetSandboxAgent(ctx, agentservice.GetRequest{Ref: requiredAgentRef(request.GetRef())})
	if err != nil {
		return nil, err
	}
	agent, err := s.agent(view)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.GetSandboxAgentResponse{Agent: agent}, nil
}

func (s *agentServer) CreateSandboxAgent(ctx context.Context, request *apiv1alpha1.CreateSandboxAgentRequest) (*apiv1alpha1.CreateSandboxAgentResponse, error) {
	agent := &v1alpha2.SandboxAgent{}
	if err := s.decodeCreateResource(request.GetRef(), request.GetResource(), agentservice.KindSandboxAgent, agent); err != nil {
		return nil, err
	}
	view, err := s.service.CreateSandboxAgent(ctx, agentservice.CreateSandboxAgentRequest{Agent: agent})
	if err != nil {
		return nil, err
	}
	response, err := s.agent(view)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.CreateSandboxAgentResponse{Agent: response}, nil
}

func (s *agentServer) UpdateSandboxAgent(ctx context.Context, request *apiv1alpha1.UpdateSandboxAgentRequest) (*apiv1alpha1.UpdateSandboxAgentResponse, error) {
	ref, err := validatedAgentRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	agent := &v1alpha2.SandboxAgent{}
	if err := s.decodeUpdateResource(ref, request.GetResource(), agentservice.KindSandboxAgent, agent); err != nil {
		return nil, err
	}
	view, err := s.service.UpdateSandboxAgent(ctx, agentservice.UpdateSandboxAgentRequest{Ref: ref, Agent: agent})
	if err != nil {
		return nil, err
	}
	response, err := s.agent(view)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.UpdateSandboxAgentResponse{Agent: response}, nil
}

func (s *agentServer) DeleteSandboxAgent(ctx context.Context, request *apiv1alpha1.DeleteSandboxAgentRequest) (*apiv1alpha1.DeleteSandboxAgentResponse, error) {
	ref, err := validatedAgentRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	if err := s.service.DeleteSandboxAgent(ctx, agentservice.DeleteRequest{Ref: ref}); err != nil {
		return nil, err
	}
	return &apiv1alpha1.DeleteSandboxAgentResponse{}, nil
}

func (s *agentServer) GetAgentHarness(ctx context.Context, request *apiv1alpha1.GetAgentHarnessRequest) (*apiv1alpha1.GetAgentHarnessResponse, error) {
	view, err := s.service.GetAgentHarness(ctx, agentservice.GetRequest{Ref: requiredAgentRef(request.GetRef())})
	if err != nil {
		return nil, err
	}
	agent, err := s.agent(view)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.GetAgentHarnessResponse{Agent: agent}, nil
}

func (s *agentServer) CreateAgentHarness(ctx context.Context, request *apiv1alpha1.CreateAgentHarnessRequest) (*apiv1alpha1.CreateAgentHarnessResponse, error) {
	harness := &v1alpha2.AgentHarness{}
	if err := s.decodeCreateResource(request.GetRef(), request.GetResource(), agentservice.KindAgentHarness, harness); err != nil {
		return nil, err
	}
	view, err := s.service.CreateAgentHarness(ctx, agentservice.CreateAgentHarnessRequest{AgentHarness: harness})
	if err != nil {
		return nil, err
	}
	response, err := s.agent(view)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.CreateAgentHarnessResponse{Agent: response}, nil
}

func (s *agentServer) DeleteAgentHarness(ctx context.Context, request *apiv1alpha1.DeleteAgentHarnessRequest) (*apiv1alpha1.DeleteAgentHarnessResponse, error) {
	ref, err := validatedAgentRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	if err := s.service.DeleteAgentHarness(ctx, agentservice.DeleteRequest{Ref: ref}); err != nil {
		return nil, err
	}
	return &apiv1alpha1.DeleteAgentHarnessResponse{}, nil
}

func (s *agentServer) EnsureAgentHarnessSessionActor(ctx context.Context, request *apiv1alpha1.EnsureAgentHarnessSessionActorRequest) (*apiv1alpha1.EnsureAgentHarnessSessionActorResponse, error) {
	actor, err := s.service.EnsureAgentHarnessSessionActor(ctx, agentservice.ActorRequest{
		Ref:       requiredAgentRef(request.GetRef()),
		SessionID: request.GetSessionId(),
	})
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.EnsureAgentHarnessSessionActorResponse{Actor: agentActor(actor)}, nil
}

func (s *agentServer) SuspendAgentHarnessSessionActor(ctx context.Context, request *apiv1alpha1.SuspendAgentHarnessSessionActorRequest) (*apiv1alpha1.SuspendAgentHarnessSessionActorResponse, error) {
	actor, err := s.service.SuspendAgentHarnessSessionActor(ctx, agentservice.ActorRequest{
		Ref:       requiredAgentRef(request.GetRef()),
		SessionID: request.GetSessionId(),
	})
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.SuspendAgentHarnessSessionActorResponse{Actor: agentActor(actor)}, nil
}

func (s *agentServer) GetAgentHarnessSessionActor(ctx context.Context, request *apiv1alpha1.GetAgentHarnessSessionActorRequest) (*apiv1alpha1.GetAgentHarnessSessionActorResponse, error) {
	actor, err := s.service.GetAgentHarnessSessionActor(ctx, agentservice.ActorRequest{
		Ref:       requiredAgentRef(request.GetRef()),
		SessionID: request.GetSessionId(),
	})
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.GetAgentHarnessSessionActorResponse{Actor: agentActor(actor)}, nil
}

func (s *agentServer) agent(view agentservice.View) (*apiv1alpha1.Agent, error) {
	resource, err := structuredobject.FromGo(view.Resource, v1alpha2.GroupVersion.String(), string(view.Kind), s.maxMessageBytes)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to encode Agent resource", err)
	}
	tools := make([]*apiv1alpha1.StructuredObject, 0, len(view.Tools))
	for _, tool := range view.Tools {
		encoded, err := structuredobject.FromGo(tool, v1alpha2.GroupVersion.String(), toolKind, s.maxMessageBytes)
		if err != nil {
			return nil, serviceerrors.NewInternal("Failed to encode Agent tool", err)
		}
		tools = append(tools, encoded)
	}
	response := &apiv1alpha1.Agent{
		Ref:             resourceReference(view.Ref),
		Kind:            agentKind(view.Kind),
		Resource:        resource,
		Id:              view.ID,
		ModelProvider:   string(view.ModelProvider),
		Model:           view.Model,
		Tools:           tools,
		DeploymentReady: view.DeploymentReady,
		Accepted:        view.Accepted,
		WorkloadMode:    agentWorkloadMode(view.WorkloadMode),
		MemoryRefs:      view.MemoryRefs,
	}
	if view.ModelConfigRef.Name != "" {
		response.ModelConfigRef = resourceReference(view.ModelConfigRef)
	}
	if view.Harness != nil {
		response.AgentHarness = &apiv1alpha1.AgentHarnessDetails{
			Backend:      string(view.Harness.Backend),
			ActorId:      view.Harness.ActorID,
			BackendRefId: view.Harness.BackendRefID,
			Endpoint:     view.Harness.Endpoint,
			AcpPath:      view.Harness.ACPPath,
		}
	}
	return response, nil
}

func (s *agentServer) decodeCreateResource(ref *apiv1alpha1.ResourceReference, resource *apiv1alpha1.StructuredObject, kind agentservice.Kind, destination client.Object) error {
	if ref == nil || ref.GetName() == "" {
		return serviceerrors.NewInvalidArgument("Agent name is required", nil)
	}
	if err := structuredobject.ToGo(resource, string(kind), destination, s.maxMessageBytes); err != nil {
		return serviceerrors.NewInvalidArgument("Invalid Agent resource", err)
	}
	if destination.GetName() != "" && destination.GetName() != ref.GetName() {
		return serviceerrors.NewInvalidArgument("Agent reference does not match resource metadata", nil)
	}
	if ref.GetNamespace() != "" && destination.GetNamespace() != "" && destination.GetNamespace() != ref.GetNamespace() {
		return serviceerrors.NewInvalidArgument("Agent reference does not match resource metadata", nil)
	}
	destination.SetName(ref.GetName())
	if ref.GetNamespace() != "" {
		destination.SetNamespace(ref.GetNamespace())
	}
	return nil
}

func (s *agentServer) decodeUpdateResource(ref types.NamespacedName, resource *apiv1alpha1.StructuredObject, kind agentservice.Kind, destination client.Object) error {
	if err := structuredobject.ToGo(resource, string(kind), destination, s.maxMessageBytes); err != nil {
		return serviceerrors.NewInvalidArgument("Invalid Agent resource", err)
	}
	if destination.GetNamespace() != ref.Namespace || destination.GetName() != ref.Name {
		return serviceerrors.NewInvalidArgument("Agent reference does not match resource metadata", nil)
	}
	return nil
}

func validatedAgentRef(ref *apiv1alpha1.ResourceReference) (types.NamespacedName, error) {
	if ref == nil || ref.GetNamespace() == "" || ref.GetName() == "" {
		return types.NamespacedName{}, serviceerrors.NewInvalidArgument("Agent namespace and name are required", nil)
	}
	return types.NamespacedName{Namespace: ref.GetNamespace(), Name: ref.GetName()}, nil
}

func requiredAgentRef(ref *apiv1alpha1.ResourceReference) types.NamespacedName {
	if ref == nil {
		return types.NamespacedName{}
	}
	return types.NamespacedName{Namespace: ref.GetNamespace(), Name: ref.GetName()}
}

func resourceReference(ref types.NamespacedName) *apiv1alpha1.ResourceReference {
	return &apiv1alpha1.ResourceReference{Namespace: ref.Namespace, Name: ref.Name}
}

func agentKind(kind agentservice.Kind) apiv1alpha1.AgentKind {
	switch kind {
	case agentservice.KindAgent:
		return apiv1alpha1.AgentKind_AGENT_KIND_AGENT
	case agentservice.KindSandboxAgent:
		return apiv1alpha1.AgentKind_AGENT_KIND_SANDBOX_AGENT
	case agentservice.KindAgentHarness:
		return apiv1alpha1.AgentKind_AGENT_KIND_AGENT_HARNESS
	default:
		return apiv1alpha1.AgentKind_AGENT_KIND_UNSPECIFIED
	}
}

func agentWorkloadMode(mode v1alpha2.WorkloadMode) apiv1alpha1.WorkloadMode {
	switch mode {
	case v1alpha2.WorkloadModeDeployment:
		return apiv1alpha1.WorkloadMode_WORKLOAD_MODE_DEPLOYMENT
	case v1alpha2.WorkloadModeSandbox:
		return apiv1alpha1.WorkloadMode_WORKLOAD_MODE_SANDBOX
	default:
		return apiv1alpha1.WorkloadMode_WORKLOAD_MODE_UNSPECIFIED
	}
}

func agentActor(actor agentservice.Actor) *apiv1alpha1.AgentHarnessSessionActor {
	state := apiv1alpha1.AgentHarnessActorState_AGENT_HARNESS_ACTOR_STATE_UNSPECIFIED
	switch actor.State {
	case agentservice.ActorStateRunning:
		state = apiv1alpha1.AgentHarnessActorState_AGENT_HARNESS_ACTOR_STATE_RUNNING
	case agentservice.ActorStateSuspended:
		state = apiv1alpha1.AgentHarnessActorState_AGENT_HARNESS_ACTOR_STATE_SUSPENDED
	case agentservice.ActorStateMissing:
		state = apiv1alpha1.AgentHarnessActorState_AGENT_HARNESS_ACTOR_STATE_MISSING
	}
	return &apiv1alpha1.AgentHarnessSessionActor{
		Ref:       resourceReference(actor.Ref),
		SessionId: actor.SessionID,
		ActorId:   actor.ActorID,
		State:     state,
	}
}
