package tool

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/secretmaterial"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	kmcp "github.com/kagent-dev/kmcp/api/v1alpha1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type ServerType string

const (
	ServerTypeRemoteMCPServer ServerType = "RemoteMCPServer"
	ServerTypeMCPServer       ServerType = "MCPServer"
)

var (
	remoteMCPServerGVK = v1alpha3.GroupVersion.WithKind(string(ServerTypeRemoteMCPServer))
	mcpServerGVK       = kmcp.GroupVersion.WithKind(string(ServerTypeMCPServer))
)

type DiscoveryStore interface {
	ListTools(context.Context) ([]database.Tool, error)
	ListToolServers(context.Context) ([]database.ToolServer, error)
	ListToolsForServer(context.Context, string, string) ([]database.Tool, error)
}

type MCPClient interface {
	ListTools(context.Context, MCPServerRef) ([]MCPAppTool, error)
	CallTool(context.Context, MCPServerRef, string, any) (*mcp.CallToolResult, error)
	ReadResource(context.Context, MCPServerRef, string) (*mcp.ReadResourceResult, error)
}

type Service struct {
	kubeClient       client.Client
	discoveryStore   DiscoveryStore
	authorizer       auth.Authorizer
	defaultNamespace string
	mcpClient        MCPClient
}

type ToolServer struct {
	Ref             string
	GroupKind       string
	DiscoveredTools []*v1alpha3.MCPTool
}

type CreateToolServerRequest struct {
	Type            ServerType
	RemoteMCPServer *v1alpha3.RemoteMCPServer
	MCPServer       *kmcp.MCPServer
	Secrets         []secretmaterial.Material
}

type MCPServerRef struct {
	Ref       types.NamespacedName
	GroupKind string
}

type MCPAppTool struct {
	Name          string
	Description   string
	InputSchema   any
	UIResourceURI string
	Meta          map[string]any
}

func NewService(
	kubeClient client.Client,
	discoveryStore DiscoveryStore,
	authorizer auth.Authorizer,
	defaultNamespace string,
	mcpClient MCPClient,
) *Service {
	if mcpClient == nil && kubeClient != nil {
		mcpClient = NewRuntimeMCPClient(kubeClient)
	}
	return &Service{
		kubeClient:       kubeClient,
		discoveryStore:   discoveryStore,
		authorizer:       authorizer,
		defaultNamespace: defaultNamespace,
		mcpClient:        mcpClient,
	}
}

func (s *Service) ListTools(ctx context.Context) ([]database.Tool, error) {
	if err := requireSession(ctx); err != nil {
		return nil, err
	}
	tools, err := s.discoveryStore.ListTools(ctx)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to list tools", err)
	}
	return tools, nil
}

func (s *Service) ListToolServers(ctx context.Context) ([]ToolServer, error) {
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: "ToolServer"}); err != nil {
		return nil, err
	}
	servers, err := s.discoveryStore.ListToolServers(ctx)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to list ToolServers from database", err)
	}
	result := make([]ToolServer, 0, len(servers))
	for _, server := range servers {
		tools, err := s.discoveryStore.ListToolsForServer(ctx, server.Name, server.GroupKind)
		if err != nil {
			return nil, serviceerrors.NewInternal("Failed to list tools for ToolServer from database", err)
		}
		discovered := make([]*v1alpha3.MCPTool, 0, len(tools))
		for _, discoveredTool := range tools {
			discovered = append(discovered, &v1alpha3.MCPTool{
				Name:        discoveredTool.ID,
				Description: discoveredTool.Description,
			})
		}
		result = append(result, ToolServer{
			Ref:             server.Name,
			GroupKind:       server.GroupKind,
			DiscoveredTools: discovered,
		})
	}
	return result, nil
}

func (s *Service) CreateToolServer(ctx context.Context, request CreateToolServerRequest) (client.Object, error) {
	supportedTypes := s.supportedServerTypes()
	if !slices.Contains(supportedTypes, request.Type) {
		return nil, serviceerrors.NewInvalidArgument(
			fmt.Sprintf("Invalid tool server type. Must be one of %s", joinServerTypes(supportedTypes)),
			nil,
		)
	}

	var owner client.Object
	var gvk schema.GroupVersionKind
	switch request.Type {
	case ServerTypeRemoteMCPServer:
		if request.RemoteMCPServer == nil {
			return nil, serviceerrors.NewInvalidArgument("RemoteMCPServer data is required when type is RemoteMCPServer", nil)
		}
		owner = request.RemoteMCPServer
		gvk = remoteMCPServerGVK
	case ServerTypeMCPServer:
		if request.MCPServer == nil {
			return nil, serviceerrors.NewInvalidArgument("MCPServer data is required when type is MCPServer", nil)
		}
		owner = request.MCPServer
		gvk = mcpServerGVK
	}

	ref, err := normalizeCreateRef(owner, s.defaultNamespace)
	if err != nil {
		return nil, serviceerrors.NewInvalidArgument("Invalid ToolServer metadata", err)
	}
	if err := s.authorize(ctx, auth.VerbCreate, auth.Resource{Type: "ToolServer", Name: ref.String()}); err != nil {
		return nil, err
	}
	if err := secretmaterial.ValidateMaterials(request.Secrets); err != nil {
		return nil, err
	}

	if err := s.kubeClient.Create(ctx, owner); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil, serviceerrors.NewAlreadyExists(string(request.Type)+" already exists", err)
		}
		return nil, serviceerrors.NewInternal("Failed to create "+string(request.Type)+" in Kubernetes", err)
	}
	if err := secretmaterial.CreateCompanionSecrets(ctx, s.kubeClient, owner, gvk, request.Secrets); err != nil {
		if rollbackErr := secretmaterial.RollbackOwnerOnCreateFailure(ctx, s.kubeClient, owner); rollbackErr != nil {
			ctrllog.FromContext(ctx).Error(rollbackErr, "failed to roll back ToolServer after companion-secret failure")
		}
		return nil, err
	}
	return owner, nil
}

func (s *Service) DeleteToolServer(ctx context.Context, ref types.NamespacedName) error {
	if ref.Namespace == "" || ref.Name == "" {
		return serviceerrors.NewInvalidArgument("ToolServer namespace and name are required", nil)
	}
	if err := s.authorize(ctx, auth.VerbDelete, auth.Resource{Type: "ToolServer", Name: ref.String()}); err != nil {
		return err
	}

	servers, err := s.discoveryStore.ListToolServers(ctx)
	if err != nil {
		return serviceerrors.NewInternal("Failed to list tool servers from database", err)
	}
	groupKind := ""
	for _, server := range servers {
		if server.Name == ref.String() {
			groupKind = server.GroupKind
			break
		}
	}
	if groupKind == "" {
		return serviceerrors.NewNotFound("ToolServer not found", nil)
	}

	var object client.Object
	switch groupKind {
	case "RemoteMCPServer.kagent.dev":
		object = &v1alpha3.RemoteMCPServer{}
	case "MCPServer.kagent.dev":
		object = &kmcp.MCPServer{}
	case "Service":
		object = &corev1.Service{}
	default:
		return serviceerrors.NewInvalidArgument("Unknown tool server type", nil)
	}
	if err := s.kubeClient.Get(ctx, ref, object); err != nil {
		if apierrors.IsNotFound(err) {
			return serviceerrors.NewNotFound(objectKindName(object)+" not found", err)
		}
		return serviceerrors.NewInternal("Failed to get "+objectKindName(object), err)
	}
	if err := s.kubeClient.Delete(ctx, object); err != nil {
		return serviceerrors.NewInternal("Failed to delete "+objectKindName(object)+" from Kubernetes", err)
	}
	return nil
}

func (s *Service) ListToolServerTypes(ctx context.Context) ([]ServerType, error) {
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: "ToolServerType"}); err != nil {
		return nil, err
	}
	return s.supportedServerTypes(), nil
}

func (s *Service) ListMCPAppTools(ctx context.Context, ref MCPServerRef) ([]MCPAppTool, error) {
	if err := s.authorizeMCP(ctx, auth.VerbGet, ref); err != nil {
		return nil, err
	}
	if s.mcpClient == nil {
		return nil, serviceerrors.NewFailedPrecondition("MCP client is not configured", nil)
	}
	tools, err := s.mcpClient.ListTools(ctx, ref)
	if err != nil {
		return nil, normalizeMCPError("Failed to list MCP tools", err)
	}
	return tools, nil
}

func (s *Service) CallMCPAppTool(ctx context.Context, ref MCPServerRef, toolName string, arguments any) (*mcp.CallToolResult, error) {
	if err := s.authorizeMCP(ctx, auth.VerbCreate, ref); err != nil {
		return nil, err
	}
	if strings.TrimSpace(toolName) == "" {
		return nil, serviceerrors.NewInvalidArgument("MCP tool name is required", nil)
	}
	if s.mcpClient == nil {
		return nil, serviceerrors.NewFailedPrecondition("MCP client is not configured", nil)
	}
	result, err := s.mcpClient.CallTool(ctx, ref, toolName, arguments)
	if err != nil {
		return nil, normalizeMCPError("Failed to call MCP tool", err)
	}
	return result, nil
}

func (s *Service) ReadMCPAppResource(ctx context.Context, ref MCPServerRef, uri string) (*mcp.ReadResourceResult, error) {
	if err := s.authorizeMCP(ctx, auth.VerbGet, ref); err != nil {
		return nil, err
	}
	if uri == "" {
		return nil, serviceerrors.NewInvalidArgument("Missing required uri", nil)
	}
	if !strings.HasPrefix(uri, "ui://") {
		return nil, serviceerrors.NewInvalidArgument("MCP Apps resources must use ui:// URIs", nil)
	}
	if s.mcpClient == nil {
		return nil, serviceerrors.NewFailedPrecondition("MCP client is not configured", nil)
	}
	result, err := s.mcpClient.ReadResource(ctx, ref, uri)
	if err != nil {
		return nil, normalizeMCPError("Failed to read MCP resource", err)
	}
	return result, nil
}

func (s *Service) supportedServerTypes() []ServerType {
	result := []ServerType{ServerTypeRemoteMCPServer}
	if s.kubeClient != nil {
		groupKind := schema.GroupKind{Group: kmcp.GroupVersion.Group, Kind: string(ServerTypeMCPServer)}
		if _, err := s.kubeClient.RESTMapper().RESTMapping(groupKind); err == nil {
			result = append(result, ServerTypeMCPServer)
		}
	}
	return result
}

func (s *Service) authorizeMCP(ctx context.Context, verb auth.Verb, ref MCPServerRef) error {
	if ref.Ref.Namespace == "" || ref.Ref.Name == "" {
		return serviceerrors.NewInvalidArgument("ToolServer namespace and name are required", nil)
	}
	return s.authorize(ctx, verb, auth.Resource{Type: "ToolServer", Name: ref.Ref.String()})
}

func (s *Service) authorize(ctx context.Context, verb auth.Verb, resource auth.Resource) error {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok || session == nil {
		return serviceerrors.NewUnauthenticated("Failed to get authenticated principal", fmt.Errorf("no session found"))
	}
	if err := s.authorizer.Check(ctx, session.Principal(), verb, resource); err != nil {
		return serviceerrors.NewPermissionDenied("Not authorized", err)
	}
	return nil
}

func requireSession(ctx context.Context) error {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok || session == nil {
		return serviceerrors.NewUnauthenticated("Failed to get authenticated principal", fmt.Errorf("no session found"))
	}
	return nil
}

func normalizeCreateRef(object client.Object, defaultNamespace string) (types.NamespacedName, error) {
	namespace := object.GetNamespace()
	if namespace == "" {
		namespace = defaultNamespace
	}
	ref, err := utils.ParseRefString(object.GetName(), namespace)
	if err != nil {
		return types.NamespacedName{}, err
	}
	object.SetNamespace(ref.Namespace)
	object.SetName(ref.Name)
	return ref, nil
}

func joinServerTypes(serverTypes []ServerType) string {
	values := make([]string, 0, len(serverTypes))
	for _, serverType := range serverTypes {
		values = append(values, string(serverType))
	}
	return strings.Join(values, ", ")
}

func objectKindName(object client.Object) string {
	switch object.(type) {
	case *v1alpha3.RemoteMCPServer:
		return "RemoteMCPServer"
	case *kmcp.MCPServer:
		return "MCPServer"
	case *corev1.Service:
		return "Service"
	default:
		return "ToolServer"
	}
}

func normalizeMCPError(message string, err error) error {
	if serviceerrors.CodeOf(err) != "" {
		return err
	}
	return serviceerrors.NewInternal(message, err)
}
