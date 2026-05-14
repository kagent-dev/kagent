package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/kagent-dev/kagent/go/core/internal/a2a"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
)

// fakeSession is a minimal auth.Session for testing.
type fakeSession struct{ principal auth.Principal }

func (s *fakeSession) Principal() auth.Principal { return s.principal }

// fakeAuthProvider propagates the incoming Bearer token to upstream requests unchanged.
type fakeAuthProvider struct {
	session auth.Session
}

func (f *fakeAuthProvider) Authenticate(_ context.Context, headers http.Header, _ url.Values) (auth.Session, error) {
	if headers.Get("Authorization") != "" {
		return f.session, nil
	}
	return nil, http.ErrNoCookie
}

func (f *fakeAuthProvider) UpstreamAuth(r *http.Request, _ auth.Session, _ auth.Principal) error {
	r.Header.Set("Authorization", "Bearer upstream-token")
	return nil
}

// a2aBackend is a fake A2A server that records the Authorization header of each request.
type a2aBackend struct {
	server         *httptest.Server
	mu             sync.Mutex
	lastAuthHeader string
}

func (b *a2aBackend) getLastAuthHeader() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastAuthHeader
}

func newA2ABackend(t *testing.T) *a2aBackend {
	t.Helper()
	b := &a2aBackend{}
	b.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.lastAuthHeader = r.Header.Get("Authorization")
		b.mu.Unlock()
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      "",
			"result": map[string]any{
				"kind":      "message",
				"messageId": "test-msg",
				"role":      "agent",
				"parts":     []any{map[string]any{"kind": "text", "text": "hello from agent"}},
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

// authRoundTripper injects a fixed Authorization header into every outgoing request.
type authRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (a *authRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+a.token)
	return a.base.RoundTrip(r)
}

// newTestRegistry builds an AgentClientRegistry with a single agent pre-registered,
// wired to send A2A requests to backendURL and propagate auth via authProvider.
func newTestRegistry(t *testing.T, namespace, name string, backendURL string, authProvider auth.AuthProvider) *a2a.AgentClientRegistry {
	t.Helper()
	agentRef := types.NamespacedName{Namespace: namespace, Name: name}
	c, err := a2aclient.NewA2AClient(
		backendURL+"/"+namespace+"/"+name+"/",
		a2aclient.WithHTTPReqHandler(authimpl.A2ARequestHandler(authProvider, agentRef)),
	)
	require.NoError(t, err)

	registry := a2a.NewAgentClientRegistry()
	registry.Register(namespace, name, c)
	return registry
}

// TestInvokeAgent_AuthPropagation exercises the full MCP HTTP stack:
// the MCP client sends a request with an Authorization header, the handler
// recovers the auth session from RequestExtra, and the A2A backend receives
// the token produced by UpstreamAuth.
func TestInvokeAgent_AuthPropagation(t *testing.T) {
	backend := newA2ABackend(t)
	authProvider := &fakeAuthProvider{session: &fakeSession{}}

	registry := newTestRegistry(t, "default", "test-agent", backend.server.URL, authProvider)
	mcpHandler, err := NewMCPHandler(nil, registry, authProvider)
	require.NoError(t, err)

	mcpServer := httptest.NewServer(mcpHandler)
	t.Cleanup(mcpServer.Close)

	transport := &mcpsdk.StreamableClientTransport{
		Endpoint: mcpServer.URL,
		HTTPClient: &http.Client{
			Transport: &authRoundTripper{base: http.DefaultTransport, token: "test-token"},
		},
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
			"agent": "default/test-agent",
			"task":  "say hello",
		},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError, "expected successful tool result, got: %v", result.Content)
	assert.Equal(t, "Bearer upstream-token", backend.getLastAuthHeader(), "A2A backend should receive the token produced by UpstreamAuth")
}

// TestInvokeAgent_NoAuthPropagationWithoutHeader verifies that when the MCP
// client sends no Authorization header, no Authorization header is
// propagated to the A2A backend.
func TestInvokeAgent_NoAuthPropagationWithoutHeader(t *testing.T) {
	backend := newA2ABackend(t)
	authProvider := &fakeAuthProvider{session: &fakeSession{}}

	registry := newTestRegistry(t, "default", "test-agent", backend.server.URL, authProvider)
	mcpHandler, err := NewMCPHandler(nil, registry, authProvider)
	require.NoError(t, err)

	mcpServer := httptest.NewServer(mcpHandler)
	t.Cleanup(mcpServer.Close)

	// No custom transport — requests carry no Authorization header.
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
			"agent": "default/test-agent",
			"task":  "say hello",
		},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Empty(t, backend.getLastAuthHeader(), "A2A backend should receive no Authorization header when the client sends none")
}
