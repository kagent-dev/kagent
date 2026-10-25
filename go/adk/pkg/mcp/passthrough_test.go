package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func TestTokenPassthroughRoundTripper(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		wantAuth string
		wantErr  bool
	}{
		{name: "caller token replaces every other Authorization source", token: "caller-token", wantAuth: "Bearer caller-token"},
		// The egress gateway replaces this placeholder with the caller's token.
		{name: "gateway placeholder is forwarded unchanged", token: "kagent-credential-injected", wantAuth: "Bearer kagent-credential-injected"},
		{name: "missing caller token fails without dialing", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests int
			var gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				gotAuth = r.Header.Get("Authorization")
			}))
			defer srv.Close()

			ctx := a2aCtx(map[string][]string{"Authorization": {"Bearer propagated-token"}})
			if tt.token != "" {
				ctx = context.WithValue(ctx, models.BearerTokenKey, tt.token)
			}
			rt := &headerRoundTripper{
				base:             newTestTransport(t),
				headers:          map[string]string{"Authorization": "Bearer static-token", "X-Static": "kept"},
				propagateToken:   true,
				headerProvider:   func(context.Context) map[string]string { return map[string]string{"Authorization": "Bearer sts-token"} },
				tokenPassthrough: true,
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
			require.NoError(t, err)
			resp, err := rt.RoundTrip(req)
			if tt.wantErr {
				require.Error(t, err)
				require.Zero(t, requests)
				return
			}
			require.NoError(t, err)
			resp.Body.Close()
			require.Equal(t, tt.wantAuth, gotAuth)
		})
	}
}

func TestMCPTransportKeepsCallerForBackgroundRequests(t *testing.T) {
	for _, protocol := range []string{"http", "sse"} {
		t.Run(protocol, func(t *testing.T) {
			server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "bound-caller", Version: "1"}, nil)
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "whoami"}, func(context.Context, *mcpsdk.CallToolRequest, struct{}) (*mcpsdk.CallToolResult, any, error) {
				return &mcpsdk.CallToolResult{}, nil, nil
			})
			var handler http.Handler = mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
			if protocol == "sse" {
				handler = mcpsdk.NewSSEHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
			}
			gets := make(chan struct{}, 1)
			deletes := make(chan struct{}, 1)
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer original-caller" {
					t.Errorf("%s used the wrong caller", r.Method)
					http.Error(w, "wrong caller", http.StatusUnauthorized)
					return
				}
				switch r.Method {
				case http.MethodGet:
					select {
					case gets <- struct{}{}:
					default:
					}
				case http.MethodDelete:
					select {
					case deletes <- struct{}{}:
					default:
					}
				}
				handler.ServeHTTP(w, r)
			}))
			t.Cleanup(endpoint.Close)
			t.Cleanup(endpoint.CloseClientConnections)
			ctx := a2aCtx(map[string][]string{"Authorization": {"Bearer original-caller"}})
			transport, err := createTransport(ctx, mcpServerParams{URL: endpoint.URL, ServerType: protocol,
				PassthroughHeaders: []*configv1.InvocationCredentialHeader{{Name: "Authorization", Prefix: "Bearer "}},
			})
			require.NoError(t, err)
			client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
			session, err := client.Connect(ctx, transport, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = session.Close() })
			otherCaller := a2aCtx(map[string][]string{"Authorization": {"Bearer other-caller"}})
			_, err = session.ListTools(otherCaller, nil)
			require.NoError(t, err)
			select {
			case <-gets:
			case <-time.After(5 * time.Second):
				t.Fatal("MCP connection did not open its background stream")
			}
			require.NoError(t, session.Close())
			if protocol == "http" {
				select {
				case <-deletes:
				case <-time.After(5 * time.Second):
					t.Fatal("MCP connection did not send session termination")
				}
			}
		})
	}
}

func TestTokenPassthroughToolsetDiscoversWithCallerToken(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	mcpServer := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "passthrough-test", Version: "1.0.0"}, nil)
	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{Name: "whoami"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		return nil, nil, nil
	})
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return mcpServer }, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	defer srv.Close()
	// The client keeps a standalone SSE GET open; drop it so Close can return.
	defer srv.CloseClientConnections()

	toolset, err := initializeToolSet(t.Context(), mcpServerParams{
		URL: srv.URL, ServerType: "http", TokenPassthrough: true,
	}, nil)
	require.NoError(t, err)
	mu.Lock()
	require.Empty(t, seen, "startup must not dial without a caller token")
	mu.Unlock()

	_, err = toolset.Tools(testReadonlyContext{Context: t.Context()})
	require.Error(t, err)

	ctx := context.WithValue(t.Context(), models.BearerTokenKey, "alice")
	tools, err := toolset.Tools(testReadonlyContext{Context: ctx})
	require.NoError(t, err)
	require.Len(t, tools, 1)
	require.Equal(t, "whoami", tools[0].Name())
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, seen)
	for _, auth := range seen {
		require.Equal(t, "Bearer alice", auth)
	}
}
