package client

import (
	"context"
	"fmt"
	"strings"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const agentToolKind = "Tool"

// Agent defines the agent operations
type Agent interface {
	ListAgents(ctx context.Context, opts ...ListAgentsOptions) (*api.StandardResponse[[]api.AgentResponse], error)
	CreateAgent(ctx context.Context, request *v1alpha2.Agent) (*api.StandardResponse[*v1alpha2.Agent], error)
	GetAgent(ctx context.Context, agentRef string) (*api.StandardResponse[*api.AgentResponse], error)
	UpdateAgent(ctx context.Context, request *v1alpha2.Agent) (*api.StandardResponse[*v1alpha2.Agent], error)
	DeleteAgent(ctx context.Context, agentRef string) error
}

// ListAgentsOptions configures ListAgents requests.
type ListAgentsOptions struct {
	Namespace string
}

// agentClient handles agent-related requests
type agentClient struct {
	client *BaseClient
}

// NewAgentClient creates a new agent client
func NewAgentClient(client *BaseClient) Agent {
	return &agentClient{client: client}
}

// ListAgents lists all agents for a user. When Namespace is set, only agents in that namespace are returned.
func (c *agentClient) ListAgents(ctx context.Context, opts ...ListAgentsOptions) (*api.StandardResponse[[]api.AgentResponse], error) {
	if len(opts) > 1 {
		return nil, fmt.Errorf("ListAgents accepts at most one options argument")
	}

	userID := c.client.GetUserIDOrDefault("")
	if userID == "" {
		return nil, fmt.Errorf("userID is required")
	}

	namespace := ""
	if len(opts) > 0 {
		namespace = opts[0].Namespace
	}
	client, err := c.client.agentServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	response, err := client.ListAgents(callContext, &apiv1alpha1.ListAgentsRequest{Namespace: namespace})
	if err != nil {
		return nil, err
	}

	agents := make([]api.AgentResponse, 0, len(response.GetAgents()))
	for _, message := range response.GetAgents() {
		agent, err := c.client.decodeAgent(message)
		if err != nil {
			return nil, err
		}
		agents = append(agents, *agent)
	}
	result := api.NewResponse(agents, "Successfully listed agents", false)
	return &result, nil
}

// CreateAgent creates a new agent
func (c *agentClient) CreateAgent(ctx context.Context, request *v1alpha2.Agent) (*api.StandardResponse[*v1alpha2.Agent], error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "Agent request is required")
	}
	resource, err := c.client.encodeAgentResource(request, "Agent")
	if err != nil {
		return nil, err
	}
	client, err := c.client.agentServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	response, err := client.CreateAgent(callContext, &apiv1alpha1.CreateAgentRequest{
		Ref:      &apiv1alpha1.ResourceReference{Namespace: request.Namespace, Name: request.Name},
		Resource: resource,
	})
	if err != nil {
		return nil, err
	}
	created, err := c.client.decodeRegularAgent(response.GetAgent())
	if err != nil {
		return nil, err
	}
	result := api.NewResponse(created, "Successfully created agent", false)
	return &result, nil
}

// GetAgent retrieves a specific agent
func (c *agentClient) GetAgent(ctx context.Context, agentRef string) (*api.StandardResponse[*api.AgentResponse], error) {
	list, err := c.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	var selected *api.AgentResponse
	for _, row := range list.Data {
		if row.Agent == nil {
			continue
		}
		ns := row.Agent.Metadata.Namespace
		name := row.Agent.Metadata.Name
		ref := fmt.Sprintf("%s/%s", ns, name)
		if ref == agentRef || name == agentRef {
			rowCopy := row
			selected = &rowCopy
			break
		}
	}
	if selected == nil || selected.Agent == nil {
		return nil, status.Error(codes.NotFound, "Agent not found")
	}
	ref := &apiv1alpha1.ResourceReference{
		Namespace: selected.Agent.Metadata.Namespace,
		Name:      selected.Agent.Metadata.Name,
	}
	client, err := c.client.agentServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	var message *apiv1alpha1.Agent
	switch selected.Agent.Kind {
	case "SandboxAgent":
		response, callErr := client.GetSandboxAgent(callContext, &apiv1alpha1.GetSandboxAgentRequest{Ref: ref})
		if callErr != nil {
			return nil, callErr
		}
		message = response.GetAgent()
	case "AgentHarness":
		response, callErr := client.GetAgentHarness(callContext, &apiv1alpha1.GetAgentHarnessRequest{Ref: ref})
		if callErr != nil {
			return nil, callErr
		}
		message = response.GetAgent()
	default:
		response, callErr := client.GetAgent(callContext, &apiv1alpha1.GetAgentRequest{Ref: ref})
		if callErr != nil {
			return nil, callErr
		}
		message = response.GetAgent()
	}
	decoded, err := c.client.decodeAgent(message)
	if err != nil {
		return nil, err
	}
	result := api.NewResponse(decoded, "Successfully retrieved agent", false)
	return &result, nil
}

// UpdateAgent updates an existing agent
func (c *agentClient) UpdateAgent(ctx context.Context, request *v1alpha2.Agent) (*api.StandardResponse[*v1alpha2.Agent], error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "Agent request is required")
	}
	if request.Namespace == "" || request.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "Agent namespace and name are required")
	}
	resource, err := c.client.encodeAgentResource(request, "Agent")
	if err != nil {
		return nil, err
	}
	client, err := c.client.agentServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	response, err := client.UpdateAgent(callContext, &apiv1alpha1.UpdateAgentRequest{
		Ref:      &apiv1alpha1.ResourceReference{Namespace: request.Namespace, Name: request.Name},
		Resource: resource,
	})
	if err != nil {
		return nil, err
	}
	updated, err := c.client.decodeRegularAgent(response.GetAgent())
	if err != nil {
		return nil, err
	}
	result := api.NewResponse(updated, "Successfully updated agent", false)
	return &result, nil
}

// DeleteAgent deletes a agent
func (c *agentClient) DeleteAgent(ctx context.Context, agentRef string) error {
	ref, err := namespacedAgentRef(agentRef)
	if err != nil {
		return err
	}
	client, err := c.client.agentServiceClient()
	if err != nil {
		return err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	_, err = client.DeleteAgent(callContext, &apiv1alpha1.DeleteAgentRequest{Ref: ref})
	return err
}

func (c *BaseClient) agentServiceClient() (apiv1alpha1.AgentServiceClient, error) {
	connection, err := c.grpcConnection()
	if err != nil {
		return nil, err
	}
	return apiv1alpha1.NewAgentServiceClient(connection), nil
}

func (c *BaseClient) encodeAgentResource(object any, kind string) (*apiv1alpha1.StructuredObject, error) {
	resource, err := structuredobject.FromGo(object, v1alpha2.GroupVersion.String(), kind, c.grpc.maxMessageBytes)
	if err != nil {
		return nil, fmt.Errorf("encode %s resource: %w", kind, err)
	}
	return resource, nil
}

func (c *BaseClient) decodeRegularAgent(message *apiv1alpha1.Agent) (*v1alpha2.Agent, error) {
	if message == nil || message.GetKind() != apiv1alpha1.AgentKind_AGENT_KIND_AGENT {
		return nil, fmt.Errorf("AgentService response did not include an Agent resource")
	}
	resource := &v1alpha2.Agent{}
	if err := structuredobject.ToGo(message.GetResource(), "Agent", resource, c.grpc.maxMessageBytes); err != nil {
		return nil, fmt.Errorf("decode Agent resource: %w", err)
	}
	return resource, nil
}

func (c *BaseClient) decodeAgent(message *apiv1alpha1.Agent) (*api.AgentResponse, error) {
	if message == nil || message.GetRef() == nil || message.GetRef().GetNamespace() == "" || message.GetRef().GetName() == "" {
		return nil, fmt.Errorf("AgentService response did not include a complete Agent reference")
	}

	response := &api.AgentResponse{
		ID:              message.GetId(),
		ModelProvider:   v1alpha2.ModelProvider(message.GetModelProvider()),
		Model:           message.GetModel(),
		MemoryRefs:      append([]string(nil), message.GetMemoryRefs()...),
		DeploymentReady: message.GetDeploymentReady(),
		Accepted:        message.GetAccepted(),
		WorkloadMode:    agentWorkloadMode(message.GetWorkloadMode()),
	}
	if modelRef := message.GetModelConfigRef(); modelRef != nil && modelRef.GetName() != "" {
		if modelRef.GetNamespace() == "" {
			response.ModelConfigRef = modelRef.GetName()
		} else {
			response.ModelConfigRef = modelRef.GetNamespace() + "/" + modelRef.GetName()
		}
	}

	switch message.GetKind() {
	case apiv1alpha1.AgentKind_AGENT_KIND_AGENT:
		resource := &v1alpha2.Agent{}
		if err := structuredobject.ToGo(message.GetResource(), "Agent", resource, c.grpc.maxMessageBytes); err != nil {
			return nil, fmt.Errorf("decode Agent resource: %w", err)
		}
		response.Agent = api.AgentResourceFrom(resource)
	case apiv1alpha1.AgentKind_AGENT_KIND_SANDBOX_AGENT:
		resource := &v1alpha2.SandboxAgent{}
		if err := structuredobject.ToGo(message.GetResource(), "SandboxAgent", resource, c.grpc.maxMessageBytes); err != nil {
			return nil, fmt.Errorf("decode SandboxAgent resource: %w", err)
		}
		response.Agent = api.AgentResourceFrom(resource)
	case apiv1alpha1.AgentKind_AGENT_KIND_AGENT_HARNESS:
		resource := &v1alpha2.AgentHarness{}
		if err := structuredobject.ToGo(message.GetResource(), "AgentHarness", resource, c.grpc.maxMessageBytes); err != nil {
			return nil, fmt.Errorf("decode AgentHarness resource: %w", err)
		}
		response.Agent = &api.AgentResource{
			APIVersion: v1alpha2.GroupVersion.String(),
			Kind:       "AgentHarness",
			Metadata:   *resource.ObjectMeta.DeepCopy(),
			Spec: v1alpha2.SandboxAgentSpec{AgentSpec: v1alpha2.AgentSpec{
				Description: strings.TrimSpace(resource.Spec.Description),
			}},
		}
	default:
		return nil, fmt.Errorf("AgentService response included an unknown Agent kind %q", message.GetKind())
	}

	tools := make([]*v1alpha2.Tool, 0, len(message.GetTools()))
	for _, encodedTool := range message.GetTools() {
		tool := &v1alpha2.Tool{}
		if err := structuredobject.ToGo(encodedTool, agentToolKind, tool, c.grpc.maxMessageBytes); err != nil {
			return nil, fmt.Errorf("decode Agent tool: %w", err)
		}
		tools = append(tools, tool)
	}
	response.Tools = tools
	if harness := message.GetAgentHarness(); harness != nil {
		response.SubstrateAgentHarness = &api.SubstrateAgentHarnessListEntry{
			Backend:        v1alpha2.AgentHarnessBackendType(harness.GetBackend()),
			ActorID:        harness.GetActorId(),
			AcpPath:        harness.GetAcpPath(),
			ModelConfigRef: response.ModelConfigRef,
			BackendRefID:   harness.GetBackendRefId(),
			Endpoint:       harness.GetEndpoint(),
		}
	}
	return response, nil
}

func namespacedAgentRef(ref string) (*apiv1alpha1.ResourceReference, error) {
	namespace, name, found := strings.Cut(ref, "/")
	if !found || namespace == "" || name == "" || strings.Contains(name, "/") {
		return nil, status.Error(codes.InvalidArgument, "Agent reference must use namespace/name format")
	}
	return &apiv1alpha1.ResourceReference{Namespace: namespace, Name: name}, nil
}

func agentWorkloadMode(mode apiv1alpha1.WorkloadMode) v1alpha2.WorkloadMode {
	switch mode {
	case apiv1alpha1.WorkloadMode_WORKLOAD_MODE_DEPLOYMENT:
		return v1alpha2.WorkloadModeDeployment
	case apiv1alpha1.WorkloadMode_WORKLOAD_MODE_SANDBOX:
		return v1alpha2.WorkloadModeSandbox
	default:
		return ""
	}
}
