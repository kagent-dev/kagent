package grpcserver

import (
	"context"
	"reflect"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	toolservice "github.com/kagent-dev/kagent/go/core/internal/service/tool"
)

const (
	toolKind              = "Tool"
	toolAPIVersion        = "kagent.api/v1"
	mcpAPIVersion         = "mcp.kagent.dev/v1alpha1"
	mcpInputSchemaKind    = "MCPInputSchema"
	mcpMetadataKind       = "MCPMetadata"
	mcpArgumentsKind      = "MCPArguments"
	mcpCallToolResultKind = "MCPCallToolResult"
	mcpReadResourceKind   = "MCPReadResourceResult"
)

type toolServer struct {
	apiv1.UnimplementedToolServiceServer
	service         *toolservice.Service
	maxMessageBytes int
}

func newToolServer(service *toolservice.Service, maxMessageBytes int) *toolServer {
	return &toolServer{service: service, maxMessageBytes: maxMessageBytes}
}

func (s *toolServer) ListTools(ctx context.Context, _ *apiv1.ListToolsRequest) (*apiv1.ListToolsResponse, error) {
	result, err := s.service.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	tools := make([]*apiv1.Tool, 0, len(result))
	for index := range result {
		resource, err := structuredobject.FromGo(&result[index], toolAPIVersion, toolKind, s.maxMessageBytes)
		if err != nil {
			return nil, serviceerrors.NewInternal("Failed to encode Tool", err)
		}
		tools = append(tools, &apiv1.Tool{Resource: resource})
	}
	return &apiv1.ListToolsResponse{Tools: tools}, nil
}

func (s *toolServer) ListToolServers(ctx context.Context, _ *apiv1.ListToolServersRequest) (*apiv1.ListToolServersResponse, error) {
	result, err := s.service.ListToolServers(ctx)
	if err != nil {
		return nil, err
	}
	servers := make([]*apiv1.ToolServer, 0, len(result))
	for _, server := range result {
		discoveredTools := make([]*apiv1.DiscoveredTool, 0, len(server.DiscoveredTools))
		for _, discoveredTool := range server.DiscoveredTools {
			discoveredTools = append(discoveredTools, &apiv1.DiscoveredTool{
				Name:        discoveredTool.Name,
				Description: discoveredTool.Description,
			})
		}
		servers = append(servers, &apiv1.ToolServer{
			Ref:             server.Ref,
			GroupKind:       server.GroupKind,
			DiscoveredTools: discoveredTools,
		})
	}
	return &apiv1.ListToolServersResponse{ToolServers: servers}, nil
}

func (s *toolServer) CreateToolServer(ctx context.Context, request *apiv1.CreateToolServerRequest) (*apiv1.CreateToolServerResponse, error) {
	serviceRequest, err := s.createToolServerRequest(request)
	if err != nil {
		return nil, err
	}
	created, err := s.service.CreateToolServer(ctx, serviceRequest)
	if err != nil {
		return nil, err
	}
	resource, err := s.toolServerResource(created)
	if err != nil {
		return nil, err
	}
	return &apiv1.CreateToolServerResponse{Resource: resource}, nil
}

func (s *toolServer) DeleteToolServer(ctx context.Context, request *apiv1.DeleteToolServerRequest) (*apiv1.DeleteToolServerResponse, error) {
	ref, err := requiredToolRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	if err := s.service.DeleteToolServer(ctx, ref); err != nil {
		return nil, err
	}
	return &apiv1.DeleteToolServerResponse{}, nil
}

func (s *toolServer) ListToolServerTypes(ctx context.Context, _ *apiv1.ListToolServerTypesRequest) (*apiv1.ListToolServerTypesResponse, error) {
	result, err := s.service.ListToolServerTypes(ctx)
	if err != nil {
		return nil, err
	}
	types := make([]string, 0, len(result))
	for _, serverType := range result {
		types = append(types, string(serverType))
	}
	return &apiv1.ListToolServerTypesResponse{Types: types}, nil
}

func (s *toolServer) ListMCPAppTools(ctx context.Context, request *apiv1.ListMCPAppToolsRequest) (*apiv1.ListMCPAppToolsResponse, error) {
	ref, err := requiredMCPServerRef(request.GetServer())
	if err != nil {
		return nil, err
	}
	result, err := s.service.ListMCPAppTools(ctx, ref)
	if err != nil {
		return nil, err
	}
	tools := make([]*apiv1.MCPAppTool, 0, len(result))
	for _, discoveredTool := range result {
		inputSchema, err := s.optionalStructuredObject(discoveredTool.InputSchema, mcpInputSchemaKind)
		if err != nil {
			return nil, serviceerrors.NewInternal("Failed to encode MCP tool input schema", err)
		}
		metadata, err := s.optionalStructuredObject(discoveredTool.Meta, mcpMetadataKind)
		if err != nil {
			return nil, serviceerrors.NewInternal("Failed to encode MCP tool metadata", err)
		}
		tools = append(tools, &apiv1.MCPAppTool{
			Name:          discoveredTool.Name,
			Description:   discoveredTool.Description,
			InputSchema:   inputSchema,
			UiResourceUri: discoveredTool.UIResourceURI,
			Meta:          metadata,
		})
	}
	return &apiv1.ListMCPAppToolsResponse{Tools: tools}, nil
}

func (s *toolServer) CallMCPAppTool(ctx context.Context, request *apiv1.CallMCPAppToolRequest) (*apiv1.CallMCPAppToolResponse, error) {
	ref, err := requiredMCPServerRef(request.GetServer())
	if err != nil {
		return nil, err
	}
	var arguments any
	if request.GetArguments() != nil {
		decoded := map[string]any{}
		if err := structuredobject.ToGo(request.GetArguments(), mcpArgumentsKind, &decoded, s.maxMessageBytes); err != nil {
			return nil, serviceerrors.NewInvalidArgument("Invalid MCP tool arguments", err)
		}
		arguments = decoded
	}
	result, err := s.service.CallMCPAppTool(ctx, ref, request.GetToolName(), arguments)
	if err != nil {
		return nil, err
	}
	resource, err := structuredobject.FromGo(result, mcpAPIVersion, mcpCallToolResultKind, s.maxMessageBytes)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to encode MCP tool result", err)
	}
	return &apiv1.CallMCPAppToolResponse{Result: resource}, nil
}

func (s *toolServer) ReadMCPAppResource(ctx context.Context, request *apiv1.ReadMCPAppResourceRequest) (*apiv1.ReadMCPAppResourceResponse, error) {
	ref, err := requiredMCPServerRef(request.GetServer())
	if err != nil {
		return nil, err
	}
	result, err := s.service.ReadMCPAppResource(ctx, ref, request.GetUri())
	if err != nil {
		return nil, err
	}
	resource, err := structuredobject.FromGo(result, mcpAPIVersion, mcpReadResourceKind, s.maxMessageBytes)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to encode MCP resource result", err)
	}
	return &apiv1.ReadMCPAppResourceResponse{Result: resource}, nil
}

func (s *toolServer) createToolServerRequest(request *apiv1.CreateToolServerRequest) (toolservice.CreateToolServerRequest, error) {
	ref, err := createToolRef(request.GetRef())
	if err != nil {
		return toolservice.CreateToolServerRequest{}, err
	}
	result := toolservice.CreateToolServerRequest{
		Type:    toolservice.ServerType(request.GetType()),
		Secrets: secretMaterials(request.GetSecrets()),
	}
	switch result.Type {
	case toolservice.ServerTypeRemoteMCPServer:
		server := &v1alpha3.RemoteMCPServer{}
		if err := s.decodeCreateToolServerResource(request.GetResource(), string(result.Type), ref, server); err != nil {
			return toolservice.CreateToolServerRequest{}, err
		}
		result.RemoteMCPServer = server
	default:
		return toolservice.CreateToolServerRequest{}, serviceerrors.NewInvalidArgument("Invalid tool server type", nil)
	}
	return result, nil
}

func (s *toolServer) decodeCreateToolServerResource(resource *apiv1.StructuredObject, kind string, ref types.NamespacedName, destination client.Object) error {
	if err := structuredobject.ToGo(resource, kind, destination, s.maxMessageBytes); err != nil {
		return serviceerrors.NewInvalidArgument("Invalid ToolServer resource", err)
	}
	if destination.GetName() != "" && destination.GetName() != ref.Name {
		return serviceerrors.NewInvalidArgument("ToolServer reference does not match resource metadata", nil)
	}
	if destination.GetNamespace() != "" && ref.Namespace != "" && destination.GetNamespace() != ref.Namespace {
		return serviceerrors.NewInvalidArgument("ToolServer reference does not match resource metadata", nil)
	}
	destination.SetName(ref.Name)
	if ref.Namespace != "" {
		destination.SetNamespace(ref.Namespace)
	}
	return nil
}

func (s *toolServer) toolServerResource(server client.Object) (*apiv1.StructuredObject, error) {
	var apiVersion, kind string
	switch server.(type) {
	case *v1alpha3.RemoteMCPServer:
		apiVersion = v1alpha3.GroupVersion.String()
		kind = string(toolservice.ServerTypeRemoteMCPServer)
	default:
		return nil, serviceerrors.NewInternal("Failed to encode ToolServer", nil)
	}
	resource, err := structuredobject.FromGo(server, apiVersion, kind, s.maxMessageBytes)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to encode ToolServer", err)
	}
	return resource, nil
}

func (s *toolServer) optionalStructuredObject(value any, kind string) (*apiv1.StructuredObject, error) {
	if value == nil {
		return nil, nil
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Map || reflected.Kind() == reflect.Pointer || reflected.Kind() == reflect.Interface || reflected.Kind() == reflect.Slice {
		if reflected.IsNil() {
			return nil, nil
		}
	}
	return structuredobject.FromGo(value, mcpAPIVersion, kind, s.maxMessageBytes)
}

func createToolRef(ref *apiv1.ResourceReference) (types.NamespacedName, error) {
	if ref == nil || ref.GetName() == "" {
		return types.NamespacedName{}, serviceerrors.NewInvalidArgument("ToolServer name is required", nil)
	}
	return types.NamespacedName{Namespace: ref.GetNamespace(), Name: ref.GetName()}, nil
}

func requiredToolRef(ref *apiv1.ResourceReference) (types.NamespacedName, error) {
	if ref == nil || ref.GetNamespace() == "" || ref.GetName() == "" {
		return types.NamespacedName{}, serviceerrors.NewInvalidArgument("ToolServer namespace and name are required", nil)
	}
	return types.NamespacedName{Namespace: ref.GetNamespace(), Name: ref.GetName()}, nil
}

func requiredMCPServerRef(server *apiv1.MCPServerReference) (toolservice.MCPServerRef, error) {
	if server == nil {
		return toolservice.MCPServerRef{}, serviceerrors.NewInvalidArgument("ToolServer namespace and name are required", nil)
	}
	ref, err := requiredToolRef(server.GetRef())
	if err != nil {
		return toolservice.MCPServerRef{}, err
	}
	return toolservice.MCPServerRef{Ref: ref, GroupKind: server.GetGroupKind()}, nil
}
