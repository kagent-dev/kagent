package bridge

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/config"
)

func TestMCPRejectsCrossOriginRedirect(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost"} {
		t.Run(host, func(t *testing.T) {
			var calls atomic.Int32
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusBadGateway)
			}))
			defer destination.Close()
			target := strings.Replace(destination.URL, "127.0.0.1", host, 1)
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-marker" {
					http.Error(w, "missing header", http.StatusUnauthorized)
					return
				}
				http.Redirect(w, r, target, http.StatusTemporaryRedirect)
			}))
			defer source.Close()
			cfg := redirectConfig(t, source.URL)
			p, err := startMCP(t.Context(), cfg, cfg.MCP)
			if p != nil {
				_ = p.close()
			}
			require.ErrorContains(t, err, "binding origin")
			require.Zero(t, calls.Load(), "cross-origin destination must receive no request, including binding credentials")
		})
	}
}

func TestMCPSameOriginRedirect(t *testing.T) {
	var authenticated atomic.Int32
	remote := mcp.NewServer(&mcp.Implementation{Name: "redirect-target", Version: "1"}, nil)
	mcp.AddTool(remote, &mcp.Tool{Name: "check"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
		return &mcp.CallToolResult{}, struct{}{}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return remote }, &mcp.StreamableHTTPOptions{Stateless: true})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/mcp", http.StatusTemporaryRedirect)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-marker" {
			http.Error(w, "missing header", http.StatusUnauthorized)
			return
		}
		authenticated.Add(1)
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	cfg := redirectConfig(t, server.URL+"/redirect")
	p, err := startMCP(t.Context(), cfg, cfg.MCP)
	require.NoError(t, err)
	require.NoError(t, p.close())
	require.Positive(t, authenticated.Load())
}

func TestMCPRedirectLoopIsBounded(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, "/loop", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	cfg := redirectConfig(t, server.URL)
	_, err := startMCP(t.Context(), cfg, cfg.MCP)
	require.ErrorContains(t, err, "10 redirects")
	// The SDK first tries capabilities negotiation, then legacy initialize.
	// Each request must stop after ten redirects.
	require.EqualValues(t, 20, calls.Load())
}

func TestMCPOriginComparison(t *testing.T) {
	origin, err := url.Parse("https://example.com/mcp")
	require.NoError(t, err)
	for _, tt := range []struct {
		target string
		same   bool
	}{
		{"https://EXAMPLE.com:443/other", true},
		{"http://example.com/mcp", false},
		{"https://example.com:8443/mcp", false},
		{"https://other.example.com/mcp", false},
	} {
		t.Run(tt.target, func(t *testing.T) {
			target, err := url.Parse(tt.target)
			require.NoError(t, err)
			require.Equal(t, tt.same, sameOrigin(origin, target))
			if !tt.same {
				request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, tt.target, nil)
				require.NoError(t, err)
				// No base transport: a forbidden request must be rejected before dispatch.
				_, err = (headerTransport{origin: origin, headers: []config.Header{{Name: "Authorization", Value: "Bearer test-marker"}}}).RoundTrip(request)
				require.ErrorContains(t, err, "binding origin")
				require.Empty(t, request.Header.Get("Authorization"))
			}
		})
	}
}

func redirectConfig(t *testing.T, endpoint string) *config.Config {
	t.Helper()
	cfg, err := config.Parse([]byte(fmt.Sprintf(`{"model":{"type":"openai","model":"mock"},"runtime":{"control_plane_url":"http://controller:8083"},"http_tools":[{"params":{"url":%q,"timeout":3,"headers":{"Authorization":"Bearer test-marker"}}}]}`, endpoint)))
	require.NoError(t, err)
	return cfg
}
