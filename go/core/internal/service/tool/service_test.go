package tool

import (
	"context"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/secretmaterial"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type fakeDiscoveryStore struct {
	toolsErr error
	tools    []database.Tool
	servers  []database.ToolServer
	err      error
}

func (f *fakeDiscoveryStore) ListTools(context.Context) ([]database.Tool, error) {
	if f.toolsErr != nil {
		return nil, f.toolsErr
	}
	return f.tools, f.err
}

func (f *fakeDiscoveryStore) ListToolServers(context.Context) ([]database.ToolServer, error) {
	return f.servers, f.err
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
	scope    *apiauthorization.AuthorizationScope
	scopeErr error
	err      error
	verb     pkgauth.Verb
	resource pkgauth.Resource
}

func (a *recordingAuthorizer) Check(_ context.Context, _ pkgauth.Principal, verb pkgauth.Verb, resource pkgauth.Resource) error {
	a.verb = verb
	a.resource = resource
	return a.err
}

func (a *recordingAuthorizer) Scope(_ context.Context, _ pkgauth.Principal, verb pkgauth.Verb, resourceType string) (apiauthorization.AuthorizationScope, error) {
	a.verb, a.resource = verb, pkgauth.Resource{Type: resourceType}
	if a.scopeErr != nil {
		return apiauthorization.AuthorizationScope{}, a.scopeErr
	}
	if a.scope != nil {
		return *a.scope, nil
	}
	return apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAll}, nil
}

func TestServiceDiscoveryAndAuthorization(t *testing.T) {
	store := &fakeDiscoveryStore{
		tools: []database.Tool{{ID: "server-tool", ServerName: "default/server", GroupKind: "RemoteMCPServer.api.kagent.dev", Description: "server"}},
		servers: []database.ToolServer{{
			Name:      "default/server",
			GroupKind: "RemoteMCPServer.api.kagent.dev",
		}},
	}
	authorizer := &recordingAuthorizer{}
	service := NewService(toolTestKube(t), store, authorizer, "default", &fakeMCPClient{})

	_, err := service.ListTools(context.Background())
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnauthenticated))

	ctx := toolTestContext()
	tools, err := service.ListTools(ctx)
	require.NoError(t, err)
	require.Equal(t, "server-tool", tools[0].ID)

	servers, err := service.ListToolServers(ctx)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	assert.Equal(t, "server-tool", servers[0].DiscoveredTools[0].Name)
	assert.Equal(t, pkgauth.VerbList, authorizer.verb)
	assert.Equal(t, pkgauth.Resource{Type: "ToolServer"}, authorizer.resource)

	types, err := service.ListToolServerTypes(ctx)
	require.NoError(t, err)
	assert.Equal(t, []ServerType{ServerTypeRemoteMCPServer}, types)
}

func TestServiceCreateToolServer(t *testing.T) {
	t.Run("creates companion secret and maps duplicate", func(t *testing.T) {
		kubeClient := toolTestKube(t)
		service := NewService(kubeClient, &fakeDiscoveryStore{}, &recordingAuthorizer{}, "default", nil)
		server := &v1alpha3.RemoteMCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "remote", UID: "remote-uid"},
			Spec:       v1alpha3.RemoteMCPServerSpec{URL: "https://example.com/mcp"},
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
		kubeClient := toolTestKube(t)
		authorizer := &recordingAuthorizer{err: errors.New("denied")}
		service := NewService(kubeClient, &fakeDiscoveryStore{}, authorizer, "default", nil)
		server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "denied"}}

		_, err := service.CreateToolServer(toolTestContext(), CreateToolServerRequest{
			Type:            ServerTypeRemoteMCPServer,
			RemoteMCPServer: server,
			Secrets:         []secretmaterial.Material{{Name: "INVALID NAME", Key: "bad key"}},
		})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), err)
		getErr := kubeClient.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "denied"}, &v1alpha3.RemoteMCPServer{})
		assert.True(t, apierrors.IsNotFound(getErr), getErr)
	})

	t.Run("rolls back owner after companion secret conflict", func(t *testing.T) {
		preexisting := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "default"},
			Type:       corev1.SecretTypeOpaque,
		}
		kubeClient := toolTestKube(t, preexisting)
		service := NewService(kubeClient, &fakeDiscoveryStore{}, &recordingAuthorizer{}, "default", nil)
		server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "local", UID: "local-uid"}}

		_, err := service.CreateToolServer(toolTestContext(), CreateToolServerRequest{
			Type:            ServerTypeRemoteMCPServer,
			RemoteMCPServer: server,
			Secrets:         []secretmaterial.Material{{Name: "shared", Key: "token", Value: "new"}},
		})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)
		getErr := kubeClient.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "local"}, &v1alpha3.RemoteMCPServer{})
		assert.True(t, apierrors.IsNotFound(getErr), getErr)
	})
}

func TestServiceDeleteToolServer(t *testing.T) {
	server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "remote", Namespace: "default"}}
	kubeClient := toolTestKube(t, server)
	store := &fakeDiscoveryStore{servers: []database.ToolServer{{
		Name:      "default/remote",
		GroupKind: "RemoteMCPServer.api.kagent.dev",
	}}}
	service := NewService(kubeClient, store, &recordingAuthorizer{}, "default", nil)

	require.NoError(t, service.DeleteToolServer(toolTestContext(), types.NamespacedName{Namespace: "default", Name: "remote"}))
	err := kubeClient.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "remote"}, &v1alpha3.RemoteMCPServer{})
	assert.True(t, apierrors.IsNotFound(err), err)

	err = service.DeleteToolServer(toolTestContext(), types.NamespacedName{Namespace: "default", Name: "missing"})
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound), err)
}

func TestServiceMCPFacade(t *testing.T) {
	ref := MCPServerRef{
		Ref:       types.NamespacedName{Namespace: "default", Name: "server"},
		GroupKind: "RemoteMCPServer.api.kagent.dev",
	}
	mcpClient := &fakeMCPClient{
		tools:    []MCPAppTool{{Name: "move", UIResourceURI: "ui://board"}},
		call:     &mcp.CallToolResult{},
		resource: &mcp.ReadResourceResult{},
	}
	authorizer := &recordingAuthorizer{}
	service := NewService(toolTestKube(t), &fakeDiscoveryStore{}, authorizer, "default", mcpClient)

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
	remote := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "default"},
		Spec:       v1alpha3.RemoteMCPServerSpec{URL: "https://remote.example/mcp"},
	}
	client := NewRuntimeMCPClient(toolTestKube(t, remote))

	remoteResult, err := client.ResolveServer(t.Context(), MCPServerRef{
		Ref:       types.NamespacedName{Namespace: "default", Name: "shared"},
		GroupKind: "RemoteMCPServer.api.kagent.dev",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://remote.example/mcp", remoteResult.Spec.URL)
}

func toolTestContext() context.Context {
	return pkgauth.AuthSessionTo(context.Background(), &authimpl.SimpleSession{
		P: pkgauth.Principal{ID: "tool-user"},
	})
}

func toolTestKube(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func TestListToolServersGroupsByExactIdentity(t *testing.T) {
	store := &fakeDiscoveryStore{
		servers: []database.ToolServer{
			{Name: "default/shared", GroupKind: "RemoteMCPServer.api.kagent.dev"},
			{Name: "default/shared", GroupKind: "Service"},
			{Name: "default/empty", GroupKind: "RemoteMCPServer.api.kagent.dev"},
		},
		tools: []database.Tool{
			{ID: "first", ServerName: "default/shared", GroupKind: "RemoteMCPServer.api.kagent.dev", Description: "first description"},
			{ID: "local", ServerName: "default/shared", GroupKind: "Service"},
			{ID: "second", ServerName: "default/shared", GroupKind: "RemoteMCPServer.api.kagent.dev"},
			{ID: "orphan", ServerName: "default/absent", GroupKind: "RemoteMCPServer.api.kagent.dev"},
		},
	}
	service := NewService(toolTestKube(t), store, &recordingAuthorizer{}, "default", &fakeMCPClient{})
	servers, err := service.ListToolServers(toolTestContext())
	require.NoError(t, err)
	require.Equal(t, []ToolServer{
		{Ref: "default/shared", GroupKind: "RemoteMCPServer.api.kagent.dev", DiscoveredTools: []*v1alpha3.MCPTool{{Name: "first", Description: "first description"}, {Name: "second"}}},
		{Ref: "default/shared", GroupKind: "Service", DiscoveredTools: []*v1alpha3.MCPTool{{Name: "local"}}},
		{Ref: "default/empty", GroupKind: "RemoteMCPServer.api.kagent.dev", DiscoveredTools: []*v1alpha3.MCPTool{}},
	}, servers)
}

func TestListToolServersSkipsToolReadWithoutServers(t *testing.T) {
	store := &fakeDiscoveryStore{toolsErr: errors.New("tool read failed")}
	service := NewService(toolTestKube(t), store, &recordingAuthorizer{}, "default", &fakeMCPClient{})
	servers, err := service.ListToolServers(toolTestContext())
	require.NoError(t, err)
	require.Equal(t, []ToolServer{}, servers)
	store.servers = []database.ToolServer{{Name: "default/server", GroupKind: "RemoteMCPServer.api.kagent.dev"}}
	_, err = service.ListToolServers(toolTestContext())
	require.ErrorIs(t, err, store.toolsErr)
}

func TestDiscoveryScopesBothServerAndFlatToolLists(t *testing.T) {
	remote := remoteMCPServerGVK.GroupKind().String()
	servers := []database.ToolServer{
		{Name: "team/allowed", GroupKind: remote},
		{Name: "team/allowed", GroupKind: "Service"},
		{Name: "team/denied", GroupKind: remote},
		{Name: "other/allowed", GroupKind: remote},
		{Name: "unqualified", GroupKind: remote},
		{Name: "team/extra/slash", GroupKind: remote},
		{Name: "team/allowed", GroupKind: "Unknown"},
	}
	store := &fakeDiscoveryStore{servers: servers}
	for i, server := range servers {
		store.tools = append(store.tools, database.Tool{ID: string(rune('a' + i)), ServerName: server.Name, GroupKind: server.GroupKind, Description: "private description"})
	}
	store.tools = append(store.tools, database.Tool{ID: "orphan", ServerName: "team/missing", GroupKind: remote})
	scope := apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{
		{Attribute: apiauthorization.AttributeNamespace, Operator: apiauthorization.ScopeIn, Values: []string{"team"}},
		{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{"allowed"}},
	}}}}
	authorizer := &recordingAuthorizer{scope: &scope}
	service := NewService(toolTestKube(t), store, authorizer, "default", &fakeMCPClient{})
	listed, err := service.ListToolServers(toolTestContext())
	require.NoError(t, err)
	require.Len(t, listed, 2)
	require.Equal(t, "a", listed[0].DiscoveredTools[0].Name)
	require.Equal(t, "b", listed[1].DiscoveredTools[0].Name)
	flat, err := service.ListTools(toolTestContext())
	require.NoError(t, err)
	require.Equal(t, store.tools[:2], flat)
	require.Equal(t, pkgauth.VerbList, authorizer.verb)
	require.Equal(t, pkgauth.Resource{Type: "ToolServer"}, authorizer.resource)

	scope.Kind, scope.AnyOf = apiauthorization.ScopeNone, nil
	store.err = errors.New("denied collection must not be read")
	listed, err = service.ListToolServers(toolTestContext())
	require.NoError(t, err)
	require.NotNil(t, listed)
	require.Empty(t, listed)
	flat, err = service.ListTools(toolTestContext())
	require.NoError(t, err)
	require.NotNil(t, flat)
	require.Empty(t, flat)
	scope.Kind = apiauthorization.ScopeAnyOf
	_, err = service.ListToolServers(toolTestContext())
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInternal))
	authorizer.scopeErr = errors.New("policy service unavailable")
	_, err = service.ListTools(toolTestContext())
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnavailable))
	// Capability definitions reveal no configured object and remain available.
	_, err = service.ListToolServerTypes(toolTestContext())
	require.NoError(t, err)
	_, err = service.ListToolServerTypes(t.Context())
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnauthenticated))
}

func TestAmbiguousServerDeletionPreservesBothObjects(t *testing.T) {
	ref := types.NamespacedName{Namespace: "team", Name: "shared"}
	kube := toolTestKube(t,
		&v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Namespace: ref.Namespace, Name: ref.Name}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: ref.Namespace, Name: ref.Name}},
	)
	store := &fakeDiscoveryStore{servers: []database.ToolServer{
		{Name: ref.String(), GroupKind: remoteMCPServerGVK.GroupKind().String()},
		{Name: ref.String(), GroupKind: "Service"},
	}}
	service := NewService(kube, store, &recordingAuthorizer{}, "default", &fakeMCPClient{})
	err := service.DeleteToolServer(toolTestContext(), ref)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeFailedPrecondition))
	require.NoError(t, kube.Get(t.Context(), ref, &v1alpha3.RemoteMCPServer{}))
	require.NoError(t, kube.Get(t.Context(), ref, &corev1.Service{}))
}

func TestDeniedMCPAppOperationsDoNotReachClient(t *testing.T) {
	ref := MCPServerRef{Ref: types.NamespacedName{Namespace: "team", Name: "server"}, GroupKind: remoteMCPServerGVK.GroupKind().String()}
	authorizer := &recordingAuthorizer{err: errors.New("denied")}
	// A nil runtime client is a trap: a denied call must return PermissionDenied,
	// never the missing-client precondition or an outbound resolution error.
	service := NewService(nil, &fakeDiscoveryStore{}, authorizer, "default", nil)
	_, err := service.ListMCPAppTools(toolTestContext(), ref)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
	require.Equal(t, pkgauth.VerbGet, authorizer.verb)
	_, err = service.ReadMCPAppResource(toolTestContext(), ref, "ui://resource")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
	require.Equal(t, pkgauth.VerbGet, authorizer.verb)
	_, err = service.CallMCPAppTool(toolTestContext(), ref, "tool", nil)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
	require.Equal(t, pkgauth.VerbCreate, authorizer.verb)
	require.Equal(t, pkgauth.Resource{Type: "ToolServer", Namespace: "team", Name: "server"}, authorizer.resource)
}
