package client

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	legacyv1alpha1 "github.com/kagent-dev/kagent/go/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	kmcp "github.com/kagent-dev/kmcp/api/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	remoteMCPServerKind  = "RemoteMCPServer"
	managedMCPServerKind = "MCPServer"
)

// ToolServer defines the tool server operations
type ToolServer interface {
	ListToolServers(ctx context.Context) ([]api.ToolServerResponse, error)
	CreateToolServer(ctx context.Context, toolServer *legacyv1alpha1.ToolServer) (*legacyv1alpha1.ToolServer, error)
	DeleteToolServer(ctx context.Context, namespace, toolServerName string) error
}

// ToolServerClient handles tool server-related requests
type ToolServerClient struct {
	client *BaseClient
}

// NewToolServerClient creates a new tool server client
func NewToolServerClient(client *BaseClient) ToolServer {
	return &ToolServerClient{client: client}
}

// ListToolServers lists all tool servers
func (c *ToolServerClient) ListToolServers(ctx context.Context) ([]api.ToolServerResponse, error) {
	client, err := c.client.toolServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	response, err := client.ListToolServers(callContext, &apiv1alpha1.ListToolServersRequest{})
	if err != nil {
		return nil, err
	}

	toolServers := make([]api.ToolServerResponse, 0, len(response.GetToolServers()))
	for _, message := range response.GetToolServers() {
		discoveredTools := make([]*v1alpha2.MCPTool, 0, len(message.GetDiscoveredTools()))
		for _, tool := range message.GetDiscoveredTools() {
			discoveredTools = append(discoveredTools, &v1alpha2.MCPTool{
				Name:        tool.GetName(),
				Description: tool.GetDescription(),
			})
		}
		toolServers = append(toolServers, api.ToolServerResponse{
			Ref:             message.GetRef(),
			GroupKind:       message.GetGroupKind(),
			DiscoveredTools: discoveredTools,
		})
	}
	return toolServers, nil
}

// CreateToolServer creates a new tool server
func (c *ToolServerClient) CreateToolServer(ctx context.Context, toolServer *legacyv1alpha1.ToolServer) (*legacyv1alpha1.ToolServer, error) {
	request, decodeCreated, err := c.createLegacyToolServerRequest(toolServer)
	if err != nil {
		return nil, err
	}
	client, err := c.client.toolServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	response, err := client.CreateToolServer(callContext, request)
	if err != nil {
		return nil, err
	}
	return decodeCreated(response.GetResource())
}

// DeleteToolServer deletes a tool server
func (c *ToolServerClient) DeleteToolServer(ctx context.Context, namespace, toolServerName string) error {
	if namespace == "" || toolServerName == "" {
		return status.Error(codes.InvalidArgument, "ToolServer namespace and name are required")
	}
	client, err := c.client.toolServiceClient()
	if err != nil {
		return err
	}
	callContext, cancel := c.client.grpcCallContext(ctx)
	defer cancel()
	_, err = client.DeleteToolServer(callContext, &apiv1alpha1.DeleteToolServerRequest{
		Ref: &apiv1alpha1.ResourceReference{Namespace: namespace, Name: toolServerName},
	})
	return err
}

func (c *ToolServerClient) createLegacyToolServerRequest(
	toolServer *legacyv1alpha1.ToolServer,
) (*apiv1alpha1.CreateToolServerRequest, func(*apiv1alpha1.StructuredObject) (*legacyv1alpha1.ToolServer, error), error) {
	if toolServer == nil {
		return nil, nil, status.Error(codes.InvalidArgument, "ToolServer request is required")
	}
	if toolServer.Name == "" {
		return nil, nil, status.Error(codes.InvalidArgument, "ToolServer name is required")
	}

	serverType, resource, err := c.legacyToolServerResource(toolServer)
	if err != nil {
		return nil, nil, err
	}
	request := &apiv1alpha1.CreateToolServerRequest{
		Type:     serverType,
		Ref:      &apiv1alpha1.ResourceReference{Namespace: toolServer.Namespace, Name: toolServer.Name},
		Resource: resource,
	}
	decodeCreated := func(created *apiv1alpha1.StructuredObject) (*legacyv1alpha1.ToolServer, error) {
		result := toolServer.DeepCopy()
		switch serverType {
		case remoteMCPServerKind:
			server := &v1alpha2.RemoteMCPServer{}
			if err := structuredobject.ToGo(created, remoteMCPServerKind, server, c.client.grpc.maxMessageBytes); err != nil {
				return nil, fmt.Errorf("decode RemoteMCPServer resource: %w", err)
			}
			result.ObjectMeta = *server.ObjectMeta.DeepCopy()
		case managedMCPServerKind:
			server := &kmcp.MCPServer{}
			if err := structuredobject.ToGo(created, managedMCPServerKind, server, c.client.grpc.maxMessageBytes); err != nil {
				return nil, fmt.Errorf("decode MCPServer resource: %w", err)
			}
			result.ObjectMeta = *server.ObjectMeta.DeepCopy()
		}
		return result, nil
	}
	return request, decodeCreated, nil
}

func (c *ToolServerClient) legacyToolServerResource(toolServer *legacyv1alpha1.ToolServer) (string, *apiv1alpha1.StructuredObject, error) {
	config := toolServer.Spec.Config
	switch config.Type {
	case legacyv1alpha1.ToolServerTypeSse:
		if config.Sse == nil {
			return "", nil, status.Error(codes.InvalidArgument, "ToolServer SSE configuration is required")
		}
		server, err := legacyRemoteMCPServer(toolServer, config.Sse.HttpToolServerConfig, v1alpha2.RemoteMCPServerProtocolSse, nil)
		if err != nil {
			return "", nil, err
		}
		resource, err := structuredobject.FromGo(server, v1alpha2.GroupVersion.String(), remoteMCPServerKind, c.client.grpc.maxMessageBytes)
		return remoteMCPServerKind, resource, err
	case legacyv1alpha1.ToolServerTypeStreamableHttp:
		if config.StreamableHttp == nil {
			return "", nil, status.Error(codes.InvalidArgument, "ToolServer Streamable HTTP configuration is required")
		}
		server, err := legacyRemoteMCPServer(
			toolServer,
			config.StreamableHttp.HttpToolServerConfig,
			v1alpha2.RemoteMCPServerProtocolStreamableHttp,
			config.StreamableHttp.TerminateOnClose,
		)
		if err != nil {
			return "", nil, err
		}
		resource, err := structuredobject.FromGo(server, v1alpha2.GroupVersion.String(), remoteMCPServerKind, c.client.grpc.maxMessageBytes)
		return remoteMCPServerKind, resource, err
	case legacyv1alpha1.ToolServerTypeStdio:
		if config.Stdio == nil {
			return "", nil, status.Error(codes.InvalidArgument, "ToolServer stdio configuration is required")
		}
		server, err := legacyManagedMCPServer(toolServer, config.Stdio)
		if err != nil {
			return "", nil, err
		}
		resource, err := structuredobject.FromGo(server, kmcp.GroupVersion.String(), managedMCPServerKind, c.client.grpc.maxMessageBytes)
		return managedMCPServerKind, resource, err
	default:
		return "", nil, status.Error(codes.InvalidArgument, "ToolServer type must be stdio, sse, or streamableHttp")
	}
}

func legacyRemoteMCPServer(
	toolServer *legacyv1alpha1.ToolServer,
	config legacyv1alpha1.HttpToolServerConfig,
	protocol v1alpha2.RemoteMCPServerProtocol,
	terminateOnClose *bool,
) (*v1alpha2.RemoteMCPServer, error) {
	headersFrom, err := legacyRemoteHeaders(toolServer.Namespace, config.Headers, config.HeadersFrom)
	if err != nil {
		return nil, err
	}
	return &v1alpha2.RemoteMCPServer{
		ObjectMeta: *toolServer.ObjectMeta.DeepCopy(),
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description:      toolServer.Spec.Description,
			Protocol:         protocol,
			URL:              config.URL,
			HeadersFrom:      headersFrom,
			Timeout:          config.Timeout,
			SseReadTimeout:   config.SseReadTimeout,
			TerminateOnClose: terminateOnClose,
		},
	}, nil
}

func legacyRemoteHeaders(
	namespace string,
	headers map[string]legacyv1alpha1.AnyType,
	headersFrom []legacyv1alpha1.ValueRef,
) ([]v1alpha2.ValueRef, error) {
	result := make([]v1alpha2.ValueRef, 0, len(headers)+len(headersFrom))
	for name, rawValue := range headers {
		var value string
		if err := json.Unmarshal(rawValue.RawMessage, &value); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "ToolServer header %q must contain a string value", name)
		}
		result = append(result, v1alpha2.ValueRef{Name: name, Value: value})
	}
	for _, ref := range headersFrom {
		converted := v1alpha2.ValueRef{Name: ref.Name, Value: ref.Value}
		if ref.ValueFrom != nil {
			valueNamespace, valueName, found := strings.Cut(ref.ValueFrom.ValueRef, "/")
			if !found {
				valueName = valueNamespace
				valueNamespace = namespace
			}
			if valueName == "" || valueNamespace != namespace {
				return nil, status.Errorf(codes.InvalidArgument, "ToolServer header %q uses an unsupported cross-namespace value reference", ref.Name)
			}
			converted.ValueFrom = &v1alpha2.ValueSource{
				Type: v1alpha2.ValueSourceType(ref.ValueFrom.Type),
				Name: valueName,
				Key:  ref.ValueFrom.Key,
			}
		}
		result = append(result, converted)
	}
	return result, nil
}

func legacyManagedMCPServer(toolServer *legacyv1alpha1.ToolServer, config *legacyv1alpha1.StdioMcpServerConfig) (*kmcp.MCPServer, error) {
	environment := maps.Clone(config.Env)
	if environment == nil {
		environment = map[string]string{}
	}
	for _, value := range config.EnvFrom {
		if value.ValueFrom != nil {
			return nil, status.Errorf(codes.InvalidArgument, "ToolServer environment variable %q uses a value reference that MCPServer cannot preserve", value.Name)
		}
		environment[value.Name] = value.Value
	}
	var timeout *metav1.Duration
	if config.ReadTimeoutSeconds > 0 {
		timeout = &metav1.Duration{Duration: time.Duration(config.ReadTimeoutSeconds) * time.Second}
	}
	return &kmcp.MCPServer{
		ObjectMeta: *toolServer.ObjectMeta.DeepCopy(),
		Spec: kmcp.MCPServerSpec{
			Deployment: kmcp.MCPServerDeployment{
				Cmd:  config.Command,
				Args: append([]string(nil), config.Args...),
				Env:  environment,
			},
			TransportType:  kmcp.TransportTypeStdio,
			StdioTransport: &kmcp.StdioTransport{},
			Timeout:        timeout,
		},
	}, nil
}
