package grpcserver

import (
	"context"
	"maps"

	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

const agentKind = "Agent"

type agentServer struct {
	apiv1.UnimplementedAgentServiceServer
	service         *kubecrud.Service[*v1alpha3.Agent, *v1alpha3.AgentList]
	maxMessageBytes int
}

func newAgentServer(service *kubecrud.Service[*v1alpha3.Agent, *v1alpha3.AgentList], maxMessageBytes int) *agentServer {
	return &agentServer{service: service, maxMessageBytes: maxMessageBytes}
}

func (s *agentServer) ListAgents(ctx context.Context, request *apiv1.ListAgentsRequest) (*apiv1.ListAgentsResponse, error) {
	items, err := s.service.List(ctx, request.GetNamespace())
	if err != nil {
		return nil, err
	}
	items, next, err := kubecrud.Page(items, int(request.GetPage().GetLimit()), request.GetPage().GetPageToken())
	if err != nil {
		return nil, err
	}
	agents := make([]*apiv1.Agent, 0, len(items))
	for _, item := range items {
		agent, err := s.agent(item)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return &apiv1.ListAgentsResponse{Agents: agents, Page: &apiv1.PageResponse{NextPageToken: next}}, nil
}

func (s *agentServer) GetAgent(ctx context.Context, request *apiv1.GetAgentRequest) (*apiv1.GetAgentResponse, error) {
	ref := types.NamespacedName{Namespace: request.GetRef().GetNamespace(), Name: request.GetRef().GetName()}
	result, err := s.service.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	agent, err := s.agent(result)
	if err != nil {
		return nil, err
	}
	return &apiv1.GetAgentResponse{Agent: agent}, nil
}

func (s *agentServer) CreateAgent(ctx context.Context, request *apiv1.CreateAgentRequest) (*apiv1.CreateAgentResponse, error) {
	incoming := &v1alpha3.Agent{}
	if err := s.decodeResource(request.GetRef(), request.GetResource(), incoming); err != nil {
		return nil, err
	}
	incoming.Status = v1alpha3.AgentStatus{}
	result, err := s.service.Create(ctx, incoming)
	if err != nil {
		return nil, err
	}
	agent, err := s.agent(result)
	if err != nil {
		return nil, err
	}
	return &apiv1.CreateAgentResponse{Agent: agent}, nil
}

func (s *agentServer) UpdateAgent(ctx context.Context, request *apiv1.UpdateAgentRequest) (*apiv1.UpdateAgentResponse, error) {
	ref := types.NamespacedName{Namespace: request.GetRef().GetNamespace(), Name: request.GetRef().GetName()}
	incoming := &v1alpha3.Agent{}
	if err := s.decodeResource(request.GetRef(), request.GetResource(), incoming); err != nil {
		return nil, err
	}
	existing, err := s.service.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	existing.Spec = *incoming.Spec.DeepCopy()
	existing.Labels = maps.Clone(incoming.Labels)
	result, err := s.service.Update(ctx, existing)
	if err != nil {
		return nil, err
	}
	agent, err := s.agent(result)
	if err != nil {
		return nil, err
	}
	return &apiv1.UpdateAgentResponse{Agent: agent}, nil
}

func (s *agentServer) DeleteAgent(ctx context.Context, request *apiv1.DeleteAgentRequest) (*apiv1.DeleteAgentResponse, error) {
	ref := types.NamespacedName{Namespace: request.GetRef().GetNamespace(), Name: request.GetRef().GetName()}
	if err := s.service.Delete(ctx, ref); err != nil {
		return nil, err
	}
	return &apiv1.DeleteAgentResponse{}, nil
}

func (s *agentServer) agent(agent *v1alpha3.Agent) (*apiv1.Agent, error) {
	resource, err := structuredobject.FromGo(agent, v1alpha3.GroupVersion.String(), agentKind, s.maxMessageBytes)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to encode Agent resource", err)
	}
	return &apiv1.Agent{
		Ref:      &apiv1.ResourceReference{Namespace: agent.Namespace, Name: agent.Name},
		Resource: resource,
	}, nil
}

// decodeResource reads the CR out of the request and forces its metadata to
// agree with the ref, so a payload naming a different object cannot be used to
// write outside the namespace the caller was authorized against.
func (s *agentServer) decodeResource(ref *apiv1.ResourceReference, resource *apiv1.StructuredObject, destination *v1alpha3.Agent) error {
	if err := structuredobject.ToGo(resource, agentKind, destination, s.maxMessageBytes); err != nil {
		return serviceerrors.NewInvalidArgument("Invalid Agent resource", err)
	}
	if destination.GetName() != "" && destination.GetName() != ref.GetName() {
		return serviceerrors.NewInvalidArgument("Agent reference does not match resource metadata", nil)
	}
	if destination.GetNamespace() != "" && destination.GetNamespace() != ref.GetNamespace() {
		return serviceerrors.NewInvalidArgument("Agent reference does not match resource metadata", nil)
	}
	destination.SetName(ref.GetName())
	destination.SetNamespace(ref.GetNamespace())
	return nil
}
