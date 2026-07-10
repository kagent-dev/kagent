package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2aclient "github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/a2a"
	"github.com/kagent-dev/kagent/go/core/internal/controller/reconciler"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestListAgentsInputSchemaHasProperties asserts that the list_agents tool
// advertises an inputSchema containing an explicit "properties" key, even
// though it accepts no arguments. OpenAI strict mode requires this.
// Regression test for https://github.com/kagent-dev/kagent/issues/1889.
func TestListAgentsInputSchemaHasProperties(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	h, err := NewMCPHandler(kubeClient, nil, nil)
	require.NoError(t, err)

	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	// Run the server in a goroutine; it returns when the transport closes.
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- h.server.Run(ctx, serverTransport)
	}()
	// Registered first so it runs last (LIFO): after session.Close below has
	// disconnected the client, cancel the context and drain the server's
	// return value so the goroutine cannot leak and unexpected errors surface.
	t.Cleanup(func() {
		cancel()
		if err := <-serverDone; err != nil && err != context.Canceled {
			t.Errorf("MCP server returned unexpected error: %v", err)
		}
	})

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { session.Close() })

	tools, err := session.ListTools(ctx, &mcpsdk.ListToolsParams{})
	require.NoError(t, err)

	var listAgents *mcpsdk.Tool
	for i := range tools.Tools {
		if tools.Tools[i].Name == "list_agents" {
			listAgents = tools.Tools[i]
			break
		}
	}
	require.NotNil(t, listAgents, "list_agents tool not registered")

	raw, err := json.Marshal(listAgents.InputSchema)
	require.NoError(t, err)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))

	require.Equal(t, "object", schema["type"], "inputSchema type must be object")
	props, ok := schema["properties"]
	require.True(t, ok, "inputSchema must include a properties key (got %s)", string(raw))
	require.IsType(t, map[string]any{}, props, "properties must be a JSON object")
	require.Empty(t, props, "list_agents takes no args, properties should be empty")
	require.Equal(t, false, schema["additionalProperties"], "additionalProperties must remain false")
}

func TestListReadyAgentsIncludesSandboxAgents(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	regularAgent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "regular-agent", Namespace: "default"},
		Spec: v1alpha2.AgentSpec{
			Description: "regular",
		},
		Status: v1alpha2.AgentStatus{
			Conditions: []metav1.Condition{
				{Type: v1alpha2.AgentConditionTypeAccepted, Status: metav1.ConditionTrue},
				{Type: v1alpha2.AgentConditionTypeReady, Status: metav1.ConditionTrue, Reason: reconciler.AgentReadyReasonDeploymentReady},
			},
		},
	}
	sandboxAgent := &v1alpha2.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "sandbox-agent", Namespace: "default"},
		Spec: v1alpha2.SandboxAgentSpec{
			AgentSpec: v1alpha2.AgentSpec{
				Description: "sandbox",
			},
		},
		Status: v1alpha2.AgentStatus{
			Conditions: []metav1.Condition{
				{Type: v1alpha2.AgentConditionTypeAccepted, Status: metav1.ConditionTrue},
				{Type: v1alpha2.AgentConditionTypeReady, Status: metav1.ConditionTrue, Reason: reconciler.AgentReadyReasonWorkloadReady},
			},
		},
	}
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha2.Agent{}, &v1alpha2.SandboxAgent{}).
		WithObjects(regularAgent, sandboxAgent).
		Build()

	h, err := NewMCPHandler(kubeClient, nil, nil)
	require.NoError(t, err)

	agents, err := h.listReadyAgents(context.Background())
	require.NoError(t, err)

	assert.ElementsMatch(t, []AgentSummary{
		{Ref: "default/regular-agent", GroupKind: schema.GroupKind{Group: "kagent.dev", Kind: "Agent"}.String(), Description: "regular"},
		{Ref: "default/sandbox-agent", GroupKind: schema.GroupKind{Group: "kagent.dev", Kind: "SandboxAgent"}.String(), Description: "sandbox"},
	}, agents)
}

// a2aBackend is a fake A2A server that records whether it was called.
type a2aBackend struct {
	server *httptest.Server
	mu     sync.Mutex
	called bool
}

func (b *a2aBackend) wasCalled() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.called
}

func newA2ABackend(t *testing.T) *a2aBackend {
	t.Helper()
	b := &a2aBackend{}
	b.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.called = true
		b.mu.Unlock()
		var rpcReq map[string]any
		_ = json.NewDecoder(r.Body).Decode(&rpcReq)
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      rpcReq["id"],
			"result": map[string]any{
				"message": map[string]any{
					"messageId": "test-msg",
					"role":      "ROLE_AGENT",
					"parts":     []any{map[string]any{"text": "hello from agent"}},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode fake A2A response: %v", err)
		}
	}))
	t.Cleanup(b.server.Close)
	return b
}

// newTestRegistry builds an AgentClientRegistry with a single agent pre-registered.
func newTestRegistry(t *testing.T, namespace, name, backendURL string) *a2a.AgentClientRegistry {
	t.Helper()
	c := newTestA2AClient(t, namespace, name, backendURL)
	registry := a2a.NewAgentClientRegistry()
	registry.Register(namespace, name, c)
	return registry
}

func newTestA2AClient(t *testing.T, namespace, name, backendURL string) *a2aclient.Client {
	t.Helper()
	interfaces := []*a2atype.AgentInterface{
		{
			URL:             backendURL + "/" + namespace + "/" + name + "/",
			ProtocolBinding: a2atype.TransportProtocolJSONRPC,
			ProtocolVersion: a2atype.Version,
		},
	}
	c, err := a2aclient.NewFromEndpoints(context.Background(), interfaces, a2aclient.WithJSONRPCTransport(&http.Client{}))
	require.NoError(t, err)
	return c
}

// TestInvokeAgent_InvalidAgentRef verifies that invoke_agent returns a tool
// error for agent references that are not exactly "namespace/name".
func TestInvokeAgent_InvalidAgentRef(t *testing.T) {
	for _, ref := range []string{"no-slash", "ns/name/extra", "/name", "ns/"} {
		t.Run(ref, func(t *testing.T) {
			registry := a2a.NewAgentClientRegistry()
			mcpHandler, err := NewMCPHandler(nil, registry, nil)
			require.NoError(t, err)

			mcpServer := httptest.NewServer(mcpHandler)
			t.Cleanup(mcpServer.Close)

			transport := &mcpsdk.StreamableClientTransport{
				Endpoint:             mcpServer.URL,
				DisableStandaloneSSE: true,
			}

			ctx := context.Background()
			cs, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "1.0"}, nil).
				Connect(ctx, transport, nil)
			require.NoError(t, err)
			t.Cleanup(func() { cs.Close() })

			result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
				Name:      "invoke_agent",
				Arguments: map[string]any{"agent": ref, "task": "say hello"},
			})
			require.NoError(t, err)
			assert.True(t, result.IsError, "expected a tool error for invalid agent ref %q", ref)
		})
	}
}

// TestInvokeAgent_UnregisteredAgent verifies that invoke_agent returns a tool
// error when the requested agent is not present in the AgentClientRegistry.
func TestInvokeAgent_UnregisteredAgent(t *testing.T) {
	registry := a2a.NewAgentClientRegistry() // empty — no agents registered
	mcpHandler, err := NewMCPHandler(nil, registry, nil)
	require.NoError(t, err)

	mcpServer := httptest.NewServer(mcpHandler)
	t.Cleanup(mcpServer.Close)

	transport := &mcpsdk.StreamableClientTransport{
		Endpoint:             mcpServer.URL,
		DisableStandaloneSSE: true,
	}

	ctx := context.Background()
	cs, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "1.0"}, nil).
		Connect(ctx, transport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { cs.Close() })

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "invoke_agent",
		Arguments: map[string]any{"agent": "default/unknown-agent", "task": "say hello"},
	})
	require.NoError(t, err)
	assert.True(t, result.IsError, "expected a tool error for an unregistered agent")
}

// TestInvokeAgent_RoutesViaRegistry verifies that invoke_agent retrieves the
// pre-registered A2A client from AgentClientRegistry and forwards the call.
func TestInvokeAgent_RoutesViaRegistry(t *testing.T) {
	backend := newA2ABackend(t)

	registry := newTestRegistry(t, "default", "test-agent", backend.server.URL)
	mcpHandler, err := NewMCPHandler(nil, registry, nil)
	require.NoError(t, err)

	mcpServer := httptest.NewServer(mcpHandler)
	t.Cleanup(mcpServer.Close)

	transport := &mcpsdk.StreamableClientTransport{
		Endpoint:             mcpServer.URL,
		DisableStandaloneSSE: true,
	}

	ctx := context.Background()
	cs, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "1.0"}, nil).
		Connect(ctx, transport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { cs.Close() })

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "invoke_agent",
		Arguments: map[string]any{"agent": "default/test-agent", "task": "say hello"},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError, "unexpected tool error: %v", result.Content)
	assert.True(t, backend.wasCalled(), "A2A backend should have received the forwarded request")
}

func TestInvokeAgent_RoutesSandboxAgentByGroupKind(t *testing.T) {
	regularBackend := newA2ABackend(t)
	sandboxBackend := newA2ABackend(t)

	registry := newTestRegistry(t, "default", "shared-name", regularBackend.server.URL)
	sandboxClient := newTestA2AClient(t, "default", "shared-name", sandboxBackend.server.URL)
	sandboxGroupKind := schema.GroupKind{Group: "kagent.dev", Kind: "SandboxAgent"}.String()
	require.NoError(t, registry.RegisterForGroupKind(sandboxGroupKind, "default", "shared-name", sandboxClient))

	mcpHandler, err := NewMCPHandler(nil, registry, nil)
	require.NoError(t, err)

	mcpServer := httptest.NewServer(mcpHandler)
	t.Cleanup(mcpServer.Close)

	transport := &mcpsdk.StreamableClientTransport{
		Endpoint:             mcpServer.URL,
		DisableStandaloneSSE: true,
	}

	ctx := context.Background()
	cs, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "1.0"}, nil).
		Connect(ctx, transport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { cs.Close() })

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "invoke_agent",
		Arguments: map[string]any{
			"agent":     "default/shared-name",
			"groupKind": sandboxGroupKind,
			"task":      "say hello",
		},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError, "unexpected tool error: %v", result.Content)
	assert.False(t, regularBackend.wasCalled(), "regular Agent backend should not receive SandboxAgent invocation")
	assert.True(t, sandboxBackend.wasCalled(), "SandboxAgent backend should receive the invocation")
}
