package tool

import (
	"context"
	"errors"
	"testing"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/secretmaterial"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
	kmcp "github.com/kagent-dev/kmcp/api/v1alpha1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

type fakeDiscoveryStore struct {
	tools       []database.Tool
	servers     []database.ToolServer
	serverTools map[string][]database.Tool
	err         error
}

func (f *fakeDiscoveryStore) ListTools(context.Context) ([]database.Tool, error) {
	return f.tools, f.err
}

func (f *fakeDiscoveryStore) ListToolServers(context.Context) ([]database.ToolServer, error) {
	return f.servers, f.err
}

func (f *fakeDiscoveryStore) ListToolsForServer(_ context.Context, name, groupKind string) ([]database.Tool, error) {
	return f.serverTools[name+"|"+groupKind], f.err
}

type fakeMCPClient struct {
	tools     []MCPAppTool
	call      *mcp.CallToolResult
	resource  *mcp.ReadResourceResult
	ref       MCPServerRef
	toolName  string
	arguments any
	uri       string
	err       error
}

func (f *fakeMCPClient) ListTools(_ context.Context, ref MCPServerRef) ([]MCPAppTool, error) {
	f.ref = ref
	return f.tools, f.err
}

func (f *fakeMCPClient) CallTool(_ context.Context, ref MCPServerRef, toolName string, arguments any) (*mcp.CallToolResult, error) {
	f.ref = ref
	f.toolName = toolName
	f.arguments = arguments
	return f.call, f.err
}

func (f *fakeMCPClient) ReadResource(_ context.Context, ref MCPServerRef, uri string) (*mcp.ReadResourceResult, error) {
	f.ref = ref
	f.uri = uri
	return f.resource, f.err
}

type recordingAuthorizer struct {
	err      error
	verb     pkgauth.Verb
	resource pkgauth.Resource
}

func (a *recordingAuthorizer) Check(_ context.Context, _ pkgauth.Principal, verb pkgauth.Verb, resource pkgauth.Resource) error {
	a.verb = verb
	a.resource = resource
	return a.err
}

func TestServiceDiscoveryAndAuthorization(t *testing.T) {
	store := &fakeDiscoveryStore{
		tools: []database.Tool{{ID: "all-tool", Description: "all"}},
		servers: []database.ToolServer{{
			Name:      "default/server",
			GroupKind: "RemoteMCPServer.kagent.dev",
		}},
		serverTools: map[string][]database.Tool{
			"default/server|RemoteMCPServer.kagent.dev": {{ID: "server-tool", Description: "server"}},
		},
	}
	authorizer := &recordingAuthorizer{}
	service := NewService(toolTestKube(t, true), store, authorizer, "default", &fakeMCPClient{})

	_, err := service.ListTools(context.Background())
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnauthenticated))

	ctx := toolTestContext()
	tools, err := service.ListTools(ctx)
	require.NoError(t, err)
	require.Equal(t, "all-tool", tools[0].ID)

	servers, err := service.ListToolServers(ctx)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	assert.Equal(t, "server-tool", servers[0].DiscoveredTools[0].Name)
	assert.Equal(t, pkgauth.VerbGet, authorizer.verb)
	assert.Equal(t, pkgauth.Resource{Type: "ToolServer"}, authorizer.resource)

	types, err := service.ListToolServerTypes(ctx)
	require.NoError(t, err)
	assert.Equal(t, []ServerType{ServerTypeRemoteMCPServer, ServerTypeMCPServer}, types)
}

func TestServiceCreateToolServer(t *testing.T) {
	t.Run("creates companion secret and maps duplicate", func(t *testing.T) {
		kubeClient := toolTestKube(t, true)
		service := NewService(kubeClient, &fakeDiscoveryStore{}, &recordingAuthorizer{}, "default", nil)
		server := &v1alpha2.RemoteMCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "remote", UID: "remote-uid"},
			Spec:       v1alpha2.RemoteMCPServerSpec{URL: "https://example.com/mcp"},
		}
		request := CreateToolServerRequest{
			Type:            ServerTypeRemoteMCPServer,
			RemoteMCPServer: server,
			Secrets:         []secretmaterial.Material{{Name: "remote-token", Key: "token", Value: "secret"}},
		}

		created, err := service.CreateToolServer(toolTestContext(), request)
		require.NoError(t, err)
		assert.Equal(t, "default", created.GetNamespace())
		secret := &corev1.Secret{}
		require.NoError(t, kubeClient.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "remote-token"}, secret))
		assert.Equal(t, []byte("secret"), secret.Data["token"])
		require.Len(t, secret.OwnerReferences, 1)
		assert.Equal(t, "RemoteMCPServer", secret.OwnerReferences[0].Kind)

		duplicate := server.DeepCopy()
		duplicate.SetResourceVersion("")
		request.RemoteMCPServer = duplicate
		_, err = service.CreateToolServer(toolTestContext(), request)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeAlreadyExists), err)
	})

	t.Run("authorization precedes material validation", func(t *testing.T) {
		kubeClient := toolTestKube(t, true)
		authorizer := &recordingAuthorizer{err: errors.New("denied")}
		service := NewService(kubeClient, &fakeDiscoveryStore{}, authorizer, "default", nil)
		server := &v1alpha2.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "denied"}}

		_, err := service.CreateToolServer(toolTestContext(), CreateToolServerRequest{
			Type:            ServerTypeRemoteMCPServer,
			RemoteMCPServer: server,
			Secrets:         []secretmaterial.Material{{Name: "INVALID NAME", Key: "bad key"}},
		})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), err)
		getErr := kubeClient.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "denied"}, &v1alpha2.RemoteMCPServer{})
		assert.True(t, apierrors.IsNotFound(getErr), getErr)
	})

	t.Run("rolls back owner after companion secret conflict", func(t *testing.T) {
		preexisting := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "default"},
			Type:       corev1.SecretTypeOpaque,
		}
		kubeClient := toolTestKube(t, true, preexisting)
		service := NewService(kubeClient, &fakeDiscoveryStore{}, &recordingAuthorizer{}, "default", nil)
		server := &kmcp.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: "local", UID: "local-uid"}}

		_, err := service.CreateToolServer(toolTestContext(), CreateToolServerRequest{
			Type:      ServerTypeMCPServer,
			MCPServer: server,
			Secrets:   []secretmaterial.Material{{Name: "shared", Key: "token", Value: "new"}},
		})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)
		getErr := kubeClient.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "local"}, &kmcp.MCPServer{})
		assert.True(t, apierrors.IsNotFound(getErr), getErr)
	})
}

func TestServiceDeleteToolServer(t *testing.T) {
	server := &v1alpha2.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "remote", Namespace: "default"}}
	kubeClient := toolTestKube(t, true, server)
	store := &fakeDiscoveryStore{servers: []database.ToolServer{{
		Name:      "default/remote",
		GroupKind: "RemoteMCPServer.kagent.dev",
	}}}
	service := NewService(kubeClient, store, &recordingAuthorizer{}, "default", nil)

	require.NoError(t, service.DeleteToolServer(toolTestContext(), types.NamespacedName{Namespace: "default", Name: "remote"}))
	err := kubeClient.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "remote"}, &v1alpha2.RemoteMCPServer{})
	assert.True(t, apierrors.IsNotFound(err), err)

	err = service.DeleteToolServer(toolTestContext(), types.NamespacedName{Namespace: "default", Name: "missing"})
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound), err)
}

func TestServiceMCPFacade(t *testing.T) {
	ref := MCPServerRef{
		Ref:       types.NamespacedName{Namespace: "default", Name: "server"},
		GroupKind: "MCPServer.kagent.dev",
	}
	mcpClient := &fakeMCPClient{
		tools:    []MCPAppTool{{Name: "move", UIResourceURI: "ui://board"}},
		call:     &mcp.CallToolResult{},
		resource: &mcp.ReadResourceResult{},
	}
	authorizer := &recordingAuthorizer{}
	service := NewService(toolTestKube(t, true), &fakeDiscoveryStore{}, authorizer, "default", mcpClient)

	tools, err := service.ListMCPAppTools(toolTestContext(), ref)
	require.NoError(t, err)
	assert.Equal(t, "move", tools[0].Name)
	assert.Equal(t, pkgauth.VerbGet, authorizer.verb)

	arguments := map[string]any{"column": "done"}
	result, err := service.CallMCPAppTool(toolTestContext(), ref, "move", arguments)
	require.NoError(t, err)
	assert.Same(t, mcpClient.call, result)
	assert.Equal(t, "move", mcpClient.toolName)
	assert.Equal(t, arguments, mcpClient.arguments)
	assert.Equal(t, pkgauth.VerbCreate, authorizer.verb)

	_, err = service.ReadMCPAppResource(toolTestContext(), ref, "https://example.com/board")
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)
	resultResource, err := service.ReadMCPAppResource(toolTestContext(), ref, "ui://board")
	require.NoError(t, err)
	assert.Same(t, mcpClient.resource, resultResource)
	assert.Equal(t, "ui://board", mcpClient.uri)
	assert.Equal(t, pkgauth.VerbGet, authorizer.verb)
}

func TestRuntimeMCPClientResolveServer(t *testing.T) {
	remote := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "default"},
		Spec:       v1alpha2.RemoteMCPServerSpec{URL: "https://remote.example/mcp"},
	}
	local := &kmcp.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "default"}}
	local.Spec.Deployment.Port = 9090
	client := NewRuntimeMCPClient(toolTestKube(t, true, remote, local))

	remoteResult, err := client.ResolveServer(t.Context(), MCPServerRef{
		Ref:       types.NamespacedName{Namespace: "default", Name: "shared"},
		GroupKind: "RemoteMCPServer.kagent.dev",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://remote.example/mcp", remoteResult.Spec.URL)

	localResult, err := client.ResolveServer(t.Context(), MCPServerRef{
		Ref:       types.NamespacedName{Namespace: "default", Name: "shared"},
		GroupKind: "MCPServer.kagent.dev",
	})
	require.NoError(t, err)
	assert.Equal(t, "http://shared.default:9090/mcp", localResult.Spec.URL)
}

func toolTestContext() context.Context {
	return pkgauth.AuthSessionTo(context.Background(), &authimpl.SimpleSession{
		P: pkgauth.Principal{User: pkgauth.User{ID: "tool-user"}},
	})
}

func toolTestKube(t *testing.T, withMCPServer bool, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	require.NoError(t, kmcp.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	restMapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{kmcp.GroupVersion})
	if withMCPServer {
		restMapper.Add(
			schema.GroupVersionKind{Group: kmcp.GroupVersion.Group, Version: kmcp.GroupVersion.Version, Kind: "MCPServer"},
			meta.RESTScopeNamespace,
		)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithRESTMapper(restMapper).WithObjects(objects...).Build()
}
