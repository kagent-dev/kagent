package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/config"
)

func TestMCPProxyHonorsHeadersAndSessionTermination(t *testing.T) {
	for _, terminate := range []bool{true, false} {
		t.Run(fmt.Sprint(terminate), func(t *testing.T) {
			var deletes atomic.Int32
			remote := mcp.NewServer(&mcp.Implementation{Name: "remote", Version: "1"}, nil)
			type input struct {
				A int `json:"a"`
				B int `json:"b"`
			}
			type output struct {
				Product int `json:"product"`
			}
			mcp.AddTool(remote, &mcp.Tool{Name: "multiply"}, func(_ context.Context, _ *mcp.CallToolRequest, arguments input) (*mcp.CallToolResult, output, error) {
				return nil, output{Product: arguments.A * arguments.B}, nil
			})
			handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return remote }, nil)
			httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "kagent-credential-injected" {
					http.Error(w, "missing auth", http.StatusUnauthorized)
					return
				}
				if r.Method == http.MethodDelete {
					deletes.Add(1)
				}
				handler.ServeHTTP(w, r)
			}))
			defer httpServer.Close()
			cfg, err := config.Parse([]byte(fmt.Sprintf(`{"model":{"type":"openai","model":"mock"},"runtime":{"control_plane_url":"http://controller:8083"},"http_tools":[{"params":{"url":%q,"timeout":30,"terminate_on_close":%t,"headers":{"Authorization":"__KAGENT_ENV[KAGENT_CREDENTIAL_TEST]__"}}}]}`, httpServer.URL, terminate)))
			require.NoError(t, err)
			bindings, err := cfg.Bindings([]string{"KAGENT_CREDENTIAL_TEST=kagent-credential-injected"})
			require.NoError(t, err)
			proxy, err := startMCP(t.Context(), cfg, bindings)
			require.NoError(t, err)
			client, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: proxy.servers[0].URL}, nil)
			require.NoError(t, err)
			result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "multiply", Arguments: map[string]int{"a": 6, "b": 7}})
			require.NoError(t, err)
			data, err := json.Marshal(result.StructuredContent)
			require.NoError(t, err)
			require.JSONEq(t, `{"product":42}`, string(data))
			require.NoError(t, client.Close())
			require.NoError(t, proxy.close())
			want := int32(0)
			if terminate {
				want = 1
			}
			require.Equal(t, want, deletes.Load())
		})
	}
}

func TestMCPProxyAppliesConfiguredTimeout(t *testing.T) {
	release := make(chan struct{})
	remote := mcp.NewServer(&mcp.Implementation{Name: "slow", Version: "1"}, nil)
	mcp.AddTool(remote, &mcp.Tool{Name: "wait"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct{}, error) {
		select {
		case <-ctx.Done():
			return nil, struct{}{}, ctx.Err()
		case <-release:
			return &mcp.CallToolResult{}, struct{}{}, nil
		}
	})
	httpServer := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return remote }, &mcp.StreamableHTTPOptions{Stateless: true}))
	defer func() { close(release); httpServer.Close() }()
	cfg, err := config.Parse([]byte(fmt.Sprintf(`{"model":{"type":"openai","model":"mock"},"runtime":{"control_plane_url":"http://controller:8083"},"http_tools":[{"params":{"url":%q,"timeout":0.2,"terminate_on_close":true}}]}`, httpServer.URL)))
	require.NoError(t, err)
	proxy, err := startMCP(t.Context(), cfg, cfg.MCP)
	require.NoError(t, err)
	defer func() { require.NoError(t, proxy.close()) }()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: proxy.servers[0].URL}, nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err = client.CallTool(ctx, &mcp.CallToolParams{Name: "wait", Arguments: struct{}{}})
	require.Error(t, err)
	require.NoError(t, ctx.Err(), "the shorter binding timeout must fail before the caller's deadline")
}
