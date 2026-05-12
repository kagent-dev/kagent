package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestInvokeAgent_AuthPropagation exercises the full MCP HTTP stack:
// the MCP client sends a request with an Authorization header, the handler
// recovers the auth session from RequestExtra, and the A2A backend receives
// the token produced by UpstreamAuth.
func TestInvokeAgent_AuthPropagation(t *testing.T) {
	// Fake A2A backend — records the Authorization header it receives.
	backend := newA2ABackend(t)

	authProvider := &fakeAuthProvider{session: &fakeSession{}}

	// Real MCP handler (kubeClient is nil; invoke_agent does not use it).
	mcpHandler, err := NewMCPHandler(nil, backend.server.URL, authProvider, 5*time.Second)
	require.NoError(t, err)

	mcpServer := httptest.NewServer(mcpHandler)
	t.Cleanup(mcpServer.Close)

	// MCP client whose HTTP transport injects an Authorization header on every request.
	transport := &mcpsdk.StreamableClientTransport{
		Endpoint: mcpServer.URL,
		HTTPClient: &http.Client{
			Transport: &authRoundTripper{
				base:  http.DefaultTransport,
				token: "test-token",
			},
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

	mcpHandler, err := NewMCPHandler(nil, backend.server.URL, authProvider, 5*time.Second)
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
