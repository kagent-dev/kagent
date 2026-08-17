package grpcserver

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"

	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	toolservice "github.com/kagent-dev/kagent/go/core/internal/service/tool"
	pkgAuth "github.com/kagent-dev/kagent/go/core/pkg/auth"
	kmcp "github.com/kagent-dev/kmcp/api/v1alpha1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type toolGRPCDiscoveryStore struct {
	tools       []dbpkg.Tool
	servers     []dbpkg.ToolServer
	serverTools map[string][]dbpkg.Tool
}

func (s *toolGRPCDiscoveryStore) ListTools(context.Context) ([]dbpkg.Tool, error) {
	return s.tools, nil
}

func (s *toolGRPCDiscoveryStore) ListToolServers(context.Context) ([]dbpkg.ToolServer, error) {
	return s.servers, nil
}

func (s *toolGRPCDiscoveryStore) ListToolsForServer(_ context.Context, name, groupKind string) ([]dbpkg.Tool, error) {
	return s.serverTools[name+"|"+groupKind], nil
}

type toolGRPCMCPClient struct {
	mu        sync.Mutex
	arguments any
}

func (*toolGRPCMCPClient) ListTools(context.Context, toolservice.MCPServerRef) ([]toolservice.MCPAppTool, error) {
	return []toolservice.MCPAppTool{{
		Name:          "move_task",
		Description:   "Move a task",
		InputSchema:   map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}},
		UIResourceURI: "ui://board",
		Meta:          map[string]any{"ui": map[string]any{"resourceUri": "ui://board"}},
	}}, nil
}

func (c *toolGRPCMCPClient) CallTool(_ context.Context, _ toolservice.MCPServerRef, _ string, arguments any) (*mcp.CallToolResult, error) {
	c.mu.Lock()
	c.arguments = arguments
	c.mu.Unlock()
	return &mcp.CallToolResult{}, nil
}

func (*toolGRPCMCPClient) ReadResource(context.Context, toolservice.MCPServerRef, string) (*mcp.ReadResourceResult, error) {
	return &mcp.ReadResourceResult{}, nil
}

func (c *toolGRPCMCPClient) recordedArguments() any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.arguments
}

type toolGRPCAuthorizer struct {
	mu   sync.Mutex
	verb pkgAuth.Verb
	deny bool
}

func (a *toolGRPCAuthorizer) Check(_ context.Context, _ pkgAuth.Principal, verb pkgAuth.Verb, _ pkgAuth.Resource) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.verb = verb
	if a.deny {
		return errors.New("denied")
	}
	return nil
}

func (a *toolGRPCAuthorizer) lastVerb() pkgAuth.Verb {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.verb
}

func (a *toolGRPCAuthorizer) setDenied(denied bool) {
	a.mu.Lock()
	a.deny = denied
	a.mu.Unlock()
}

func TestToolServiceGeneratedClient(t *testing.T) {
	kubeClient := toolGRPCKubeClient(t)
	store := &toolGRPCDiscoveryStore{
		tools:   []dbpkg.Tool{{ID: "move_task", ServerName: "default/shared", GroupKind: "RemoteMCPServer.kagent.dev", Description: "Move a task"}},
		servers: []dbpkg.ToolServer{{Name: "default/shared", GroupKind: "RemoteMCPServer.kagent.dev"}},
		serverTools: map[string][]dbpkg.Tool{
			"default/shared|RemoteMCPServer.kagent.dev": {{ID: "move_task", Description: "Move a task"}},
		},
	}
	authorizer := &toolGRPCAuthorizer{}
	mcpClient := &toolGRPCMCPClient{}
	service := toolservice.NewService(kubeClient, store, authorizer, "default", mcpClient)
	toolClient, cleanup := newToolGRPCClient(t, service)
	defer cleanup()
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "tool-user"))

	listedTools, err := toolClient.ListTools(ctx, &apiv1alpha1.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(listedTools.GetTools()) != 1 {
		t.Fatalf("ListTools() count = %d, want 1", len(listedTools.GetTools()))
	}
	decodedTool := &dbpkg.Tool{}
	if err := structuredobject.ToGo(listedTools.GetTools()[0].GetResource(), toolKind, decodedTool, DefaultMaxMessageSize); err != nil {
		t.Fatalf("decode listed Tool: %v", err)
	}
	if decodedTool.ID != "move_task" || decodedTool.GroupKind != "RemoteMCPServer.kagent.dev" {
		t.Fatalf("decoded Tool = %+v", decodedTool)
	}

	listedServers, err := toolClient.ListToolServers(ctx, &apiv1alpha1.ListToolServersRequest{})
	if err != nil {
		t.Fatalf("ListToolServers() error = %v", err)
	}
	if len(listedServers.GetToolServers()) != 1 || listedServers.GetToolServers()[0].GetDiscoveredTools()[0].GetName() != "move_task" {
		t.Fatalf("ListToolServers() = %+v", listedServers.GetToolServers())
	}
	if authorizer.lastVerb() != pkgAuth.VerbGet {
		t.Fatalf("ListToolServers() authorization verb = %q, want get", authorizer.lastVerb())
	}

	serverTypes, err := toolClient.ListToolServerTypes(ctx, &apiv1alpha1.ListToolServerTypesRequest{})
	if err != nil {
		t.Fatalf("ListToolServerTypes() error = %v", err)
	}
	if len(serverTypes.GetTypes()) != 2 || serverTypes.GetTypes()[1] != string(toolservice.ServerTypeMCPServer) {
		t.Fatalf("ListToolServerTypes() = %v", serverTypes.GetTypes())
	}

	ref := &apiv1alpha1.ResourceReference{Namespace: "default", Name: "shared"}
	remoteResource := toolGRPCResource(t, &v1alpha3.RemoteMCPServer{
		Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://remote.example/mcp"},
	}, v1alpha3.GroupVersion.String(), string(toolservice.ServerTypeRemoteMCPServer))
	createdRemote, err := toolClient.CreateToolServer(ctx, &apiv1alpha1.CreateToolServerRequest{
		Type:     string(toolservice.ServerTypeRemoteMCPServer),
		Ref:      ref,
		Resource: remoteResource,
		Secrets:  []*apiv1alpha1.SecretMaterial{{Name: "shared-token", Key: "token", Value: "super-secret"}},
	})
	if err != nil {
		t.Fatalf("CreateToolServer(RemoteMCPServer) error = %v", err)
	}
	decodedRemote := &v1alpha3.RemoteMCPServer{}
	if err := structuredobject.ToGo(createdRemote.GetResource(), string(toolservice.ServerTypeRemoteMCPServer), decodedRemote, DefaultMaxMessageSize); err != nil {
		t.Fatalf("decode created RemoteMCPServer: %v", err)
	}
	if decodedRemote.Namespace != "default" || decodedRemote.Name != "shared" || decodedRemote.Spec.URL != "https://remote.example/mcp" {
		t.Fatalf("created RemoteMCPServer = %+v", decodedRemote)
	}
	encodedResponse, err := json.Marshal(createdRemote)
	if err != nil {
		t.Fatalf("marshal create response: %v", err)
	}
	if strings.Contains(string(encodedResponse), "super-secret") {
		t.Fatal("CreateToolServer response leaked companion secret material")
	}
	secret := &corev1.Secret{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: "shared-token"}, secret); err != nil {
		t.Fatalf("get companion Secret: %v", err)
	}
	if string(secret.Data["token"]) != "super-secret" {
		t.Fatalf("companion Secret token = %q", secret.Data["token"])
	}
	if authorizer.lastVerb() != pkgAuth.VerbCreate {
		t.Fatalf("CreateToolServer() authorization verb = %q, want create", authorizer.lastVerb())
	}

	localResource := toolGRPCResource(t, &kmcp.MCPServer{}, kmcp.GroupVersion.String(), string(toolservice.ServerTypeMCPServer))
	if _, err := toolClient.CreateToolServer(ctx, &apiv1alpha1.CreateToolServerRequest{
		Type: string(toolservice.ServerTypeMCPServer), Ref: ref, Resource: localResource,
	}); err != nil {
		t.Fatalf("CreateToolServer(MCPServer collision) error = %v", err)
	}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "shared"}, &v1alpha3.RemoteMCPServer{}); err != nil {
		t.Fatalf("get colliding RemoteMCPServer: %v", err)
	}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "shared"}, &kmcp.MCPServer{}); err != nil {
		t.Fatalf("get colliding MCPServer: %v", err)
	}

	_, err = toolClient.CreateToolServer(ctx, &apiv1alpha1.CreateToolServerRequest{
		Type: string(toolservice.ServerTypeRemoteMCPServer), Ref: ref, Resource: remoteResource,
	})
	assertGRPCCode(t, err, codes.AlreadyExists)

	invalidKind := toolGRPCResource(t, &v1alpha3.RemoteMCPServer{}, v1alpha3.GroupVersion.String(), "Agent")
	_, err = toolClient.CreateToolServer(ctx, &apiv1alpha1.CreateToolServerRequest{
		Type: string(toolservice.ServerTypeRemoteMCPServer), Ref: &apiv1alpha1.ResourceReference{Name: "invalid"}, Resource: invalidKind,
	})
	assertGRPCCode(t, err, codes.InvalidArgument)

	mismatchedRef := toolGRPCResource(t, &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "resource-name"},
	}, v1alpha3.GroupVersion.String(), string(toolservice.ServerTypeRemoteMCPServer))
	_, err = toolClient.CreateToolServer(ctx, &apiv1alpha1.CreateToolServerRequest{
		Type: string(toolservice.ServerTypeRemoteMCPServer), Ref: &apiv1alpha1.ResourceReference{Name: "ref-name"}, Resource: mismatchedRef,
	})
	assertGRPCCode(t, err, codes.InvalidArgument)

	mcpRef := &apiv1alpha1.MCPServerReference{Ref: ref, GroupKind: "MCPServer.kagent.dev"}
	mcpTools, err := toolClient.ListMCPAppTools(ctx, &apiv1alpha1.ListMCPAppToolsRequest{Server: mcpRef})
	if err != nil {
		t.Fatalf("ListMCPAppTools() error = %v", err)
	}
	if len(mcpTools.GetTools()) != 1 || mcpTools.GetTools()[0].GetUiResourceUri() != "ui://board" {
		t.Fatalf("ListMCPAppTools() = %+v", mcpTools.GetTools())
	}
	inputSchema := map[string]any{}
	if err := structuredobject.ToGo(mcpTools.GetTools()[0].GetInputSchema(), mcpInputSchemaKind, &inputSchema, DefaultMaxMessageSize); err != nil {
		t.Fatalf("decode MCP input schema: %v", err)
	}
	if inputSchema["type"] != "object" || authorizer.lastVerb() != pkgAuth.VerbGet {
		t.Fatalf("MCP input schema = %+v, verb = %q", inputSchema, authorizer.lastVerb())
	}

	arguments := toolGRPCResource(t, map[string]any{"id": "task-1"}, mcpAPIVersion, mcpArgumentsKind)
	called, err := toolClient.CallMCPAppTool(ctx, &apiv1alpha1.CallMCPAppToolRequest{
		Server: mcpRef, ToolName: "move_task", Arguments: arguments,
	})
	if err != nil {
		t.Fatalf("CallMCPAppTool() error = %v", err)
	}
	if called.GetResult().GetKind() != mcpCallToolResultKind || authorizer.lastVerb() != pkgAuth.VerbCreate {
		t.Fatalf("CallMCPAppTool() result kind = %q, verb = %q", called.GetResult().GetKind(), authorizer.lastVerb())
	}
	recordedArguments, ok := mcpClient.recordedArguments().(map[string]any)
	if !ok || recordedArguments["id"] != "task-1" {
		t.Fatalf("recorded MCP arguments = %#v", mcpClient.recordedArguments())
	}

	read, err := toolClient.ReadMCPAppResource(ctx, &apiv1alpha1.ReadMCPAppResourceRequest{Server: mcpRef, Uri: "ui://board"})
	if err != nil {
		t.Fatalf("ReadMCPAppResource() error = %v", err)
	}
	if read.GetResult().GetKind() != mcpReadResourceKind || authorizer.lastVerb() != pkgAuth.VerbGet {
		t.Fatalf("ReadMCPAppResource() result kind = %q, verb = %q", read.GetResult().GetKind(), authorizer.lastVerb())
	}
	_, err = toolClient.ReadMCPAppResource(ctx, &apiv1alpha1.ReadMCPAppResourceRequest{Server: mcpRef, Uri: "https://example.com"})
	assertGRPCCode(t, err, codes.InvalidArgument)

	authorizer.setDenied(true)
	_, err = toolClient.ListToolServers(ctx, &apiv1alpha1.ListToolServersRequest{})
	assertGRPCCode(t, err, codes.PermissionDenied)
	authorizer.setDenied(false)

	if _, err := toolClient.DeleteToolServer(ctx, &apiv1alpha1.DeleteToolServerRequest{Ref: ref}); err != nil {
		t.Fatalf("DeleteToolServer() error = %v", err)
	}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "shared"}, &v1alpha3.RemoteMCPServer{}); !apierrors.IsNotFound(err) {
		t.Fatalf("deleted RemoteMCPServer get error = %v, want NotFound", err)
	}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "shared"}, &kmcp.MCPServer{}); err != nil {
		t.Fatalf("DeleteToolServer removed colliding MCPServer: %v", err)
	}
}

func newToolGRPCClient(t *testing.T, service *toolservice.Service) (apiv1alpha1.ToolServiceClient, func()) {
	t.Helper()
	listener := bufconn.Listen(DefaultMaxMessageSize)
	server, err := New(Config{
		Listener:      listener,
		Registerer:    prometheus.NewRegistry(),
		Authenticator: &authimpl.UnsecureAuthenticator{},
		ToolService:   service,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	serverContext, cancelServer := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Start(serverContext) }()

	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	if err != nil {
		cancelServer()
		<-done
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	cleanup := func() {
		_ = connection.Close()
		cancelServer()
		if err := <-done; err != nil {
			t.Errorf("gRPC server shutdown error = %v", err)
		}
	}
	return apiv1alpha1.NewToolServiceClient(connection), cleanup
}

func toolGRPCKubeClient(t *testing.T) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha3.AddToScheme(scheme); err != nil {
		t.Fatalf("v1alpha3.AddToScheme() error = %v", err)
	}
	if err := kmcp.AddToScheme(scheme); err != nil {
		t.Fatalf("kmcp.AddToScheme() error = %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("corev1.AddToScheme() error = %v", err)
	}
	restMapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{kmcp.GroupVersion})
	restMapper.Add(
		schema.GroupVersionKind{Group: kmcp.GroupVersion.Group, Version: kmcp.GroupVersion.Version, Kind: "MCPServer"},
		meta.RESTScopeNamespace,
	)
	return fake.NewClientBuilder().WithScheme(scheme).WithRESTMapper(restMapper).Build()
}

func toolGRPCResource(t *testing.T, value any, apiVersion, kind string) *apiv1alpha1.StructuredObject {
	t.Helper()
	resource, err := structuredobject.FromGo(value, apiVersion, kind, DefaultMaxMessageSize)
	if err != nil {
		t.Fatalf("structuredobject.FromGo(%s) error = %v", kind, err)
	}
	return resource
}
