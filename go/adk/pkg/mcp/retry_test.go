package mcp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// recordingServer answers each request with the next status in statuses, then
// 200, and records the bodies it received.
type recordingServer struct {
	mu       sync.Mutex
	statuses []int
	bodies   []string
}

func (s *recordingServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bodies = append(s.bodies, string(body))
	status := http.StatusOK
	if len(s.statuses) > 0 {
		status, s.statuses = s.statuses[0], s.statuses[1:]
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{}`))
}

func (s *recordingServer) received() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

func postRPC(t *testing.T, rt http.RoundTripper, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(body))
	require.NoError(t, err)
	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestTransientRetryRoundTripper(t *testing.T) {
	const (
		toolsList = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
		toolsCall = `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"add_issue_comment"}}`
	)
	scenarios := []struct {
		name       string
		body       string
		statuses   []int
		wantStatus int
		wantSent   int
	}{
		{
			name:     "Given a server that fails twice with 503, when the client lists tools, assert that the third attempt succeeds",
			body:     toolsList,
			statuses: []int{503, 503}, wantStatus: 200, wantSent: 3,
		},
		{
			name:     "Given a server that fails with 502 and then 504, when the client initializes, assert that the third attempt succeeds",
			body:     `{"jsonrpc":"2.0","id":0,"method":"initialize"}`,
			statuses: []int{502, 504}, wantStatus: 200, wantSent: 3,
		},
		{
			name:     "Given a server that keeps failing with 503, when the client lists tools, assert that the last 503 is returned after two retries",
			body:     toolsList,
			statuses: []int{503, 503, 503, 503}, wantStatus: 503, wantSent: 3,
		},
		{
			name:     "Given a server that fails with 503, when the client calls a tool, assert that the call is sent once",
			body:     toolsCall,
			statuses: []int{503}, wantStatus: 503, wantSent: 1,
		},
		{
			name:     "Given a server that fails with 503, when the client sends a notification, assert that it is sent once",
			body:     `{"jsonrpc":"2.0","method":"notifications/initialized"}`,
			statuses: []int{503}, wantStatus: 503, wantSent: 1,
		},
		{
			name:     "Given a server that fails with 503, when the client sends a batch, assert that it is sent once",
			body:     "[" + toolsList + "]",
			statuses: []int{503}, wantStatus: 503, wantSent: 1,
		},
		{
			name:     "Given a server that fails with 500, when the client lists tools, assert that the request is sent once",
			body:     toolsList,
			statuses: []int{500}, wantStatus: 500, wantSent: 1,
		},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			server := &recordingServer{statuses: sc.statuses}
			srv := httptest.NewServer(server)
			defer srv.Close()
			rt := &transientRetryRoundTripper{base: newTestTransport(t), retries: 2, backoff: time.Millisecond}

			resp := postRPC(t, rt, srv.URL, sc.body)

			require.Equal(t, sc.wantStatus, resp.StatusCode)
			received := server.received()
			require.Len(t, received, sc.wantSent)
			for _, body := range received {
				require.Equal(t, sc.body, body)
			}
		})
	}

	t.Run("Given a server that fails with 503, when a read-only request is larger than the retry buffer, assert that it is sent once and intact", func(t *testing.T) {
		t.Parallel()
		server := &recordingServer{statuses: []int{503}}
		srv := httptest.NewServer(server)
		defer srv.Close()
		rt := &transientRetryRoundTripper{base: newTestTransport(t), retries: 2, backoff: time.Millisecond}
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"cursor":"` + strings.Repeat("x", maxRetryBodyBytes) + `"}}`

		resp := postRPC(t, rt, srv.URL, body)

		require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
		require.Equal(t, []string{body}, server.received())
	})

	t.Run("Given a server that keeps failing with 503, when the request context ends during the backoff, assert that the retry stops with the context error", func(t *testing.T) {
		t.Parallel()
		server := &recordingServer{statuses: []int{503, 503, 503}}
		srv := httptest.NewServer(server)
		defer srv.Close()
		rt := &transientRetryRoundTripper{base: newTestTransport(t), retries: 2, backoff: time.Hour}
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, strings.NewReader(toolsList))
		require.NoError(t, err)

		resp, err := rt.RoundTrip(req)

		if resp != nil {
			_ = resp.Body.Close()
		}
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Len(t, server.received(), 1)
	})
}

// TestToolSetRetriesTransientToolsList is the regression test for an agent run
// that failed with "calling "tools/list": rejected by transport: sending
// "tools/list": Service Unavailable" when a gateway answered one tools/list
// with 503.
func TestToolSetRetriesTransientToolsList(t *testing.T) {
	mcpServer := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "weather-test", Version: "1.0.0"}, nil)
	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{Name: "getWeather"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		return nil, map[string]any{"weather": "sunny"}, nil
	})
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return mcpServer }, nil)

	var mu sync.Mutex
	armed, failed := false, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		mu.Lock()
		fail := armed && failed == 0 && jsonRPCMethod(body) == "tools/list"
		if fail {
			failed++
		}
		mu.Unlock()
		if fail {
			http.Error(w, "authentication service temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	// The MCP client keeps a stream open, which would block Close.
	defer func() {
		srv.CloseClientConnections()
		srv.Close()
	}()

	toolset, err := initializeToolSet(t.Context(), mcpServerParams{URL: srv.URL, ServerType: "http"}, map[string]bool{"getWeather": true})
	require.NoError(t, err)
	// Fail the next tools/list, the one an agent run sends, not the one that
	// classifies the tools at startup.
	mu.Lock()
	armed = true
	mu.Unlock()

	tools, err := toolset.Tools(testReadonlyContext{Context: t.Context()})
	require.NoError(t, err)
	require.Len(t, tools, 1)
	require.Equal(t, "getWeather", tools[0].Name())
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, failed, "the server never failed a tools/list")
}
