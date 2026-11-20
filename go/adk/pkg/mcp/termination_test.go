package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func TestCreateToolsetsTerminateOnClose(t *testing.T) {
	for _, auth := range []struct {
		name    string
		headers map[string]string
	}{
		{name: "anonymous"},
		{name: "authenticated", headers: map[string]string{"Authorization": "Bearer test-token"}},
	} {
		for _, tt := range []struct {
			name       string
			terminate  *bool
			wantDelete int64
		}{
			{name: "default", wantDelete: 1},
			{name: "enabled", terminate: new(true), wantDelete: 1},
			{name: "disabled", terminate: new(false)},
		} {
			t.Run(tt.name+"/"+auth.name, func(t *testing.T) {
				t.Parallel()
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()

				server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "termination-test", Version: "1.0.0"}, nil)
				mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "echo"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
					return nil, nil, nil
				})
				t.Cleanup(func() {
					for session := range server.Sessions() {
						require.NoError(t, session.Close())
					}
				})
				handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
				var deletes, unauthorized atomic.Int64
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != auth.headers["Authorization"] {
						unauthorized.Add(1)
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					if r.Method == http.MethodDelete {
						deletes.Add(1)
					}
					handler.ServeHTTP(w, r)
				}))
				defer srv.Close()
				defer srv.CloseClientConnections()

				params := &configv1.ADKHTTPConnectionParams{Url: srv.URL, Headers: auth.headers, TerminateOnClose: tt.terminate}
				toolsets := CreateToolsets(ctx, []*configv1.ADKHTTPMCPServerConfig{{Params: params}}, nil, nil, false, nil)
				require.Len(t, toolsets, 1)
				require.Zero(t, unauthorized.Load())
				require.Equal(t, tt.wantDelete, deletes.Load(), "tool discovery must honor the configured session termination policy")
			})
		}
	}
}

func TestStreamableTransportPreservesSessionAndClosesStreams(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "termination-test", Version: "1.0.0"}, nil)
	t.Cleanup(func() {
		for session := range server.Sessions() {
			require.NoError(t, session.Close())
		}
	})
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
	started, stopped := make(chan struct{}, 1), make(chan struct{}, 1)
	var deletes atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes.Add(1)
		}
		if r.Method == http.MethodGet {
			started <- struct{}{}
			defer func() { stopped <- struct{}{} }()
		}
		handler.ServeHTTP(w, r)
	}))
	defer srv.Close()
	defer srv.CloseClientConnections()

	transport, err := createTransport(ctx, mcpServerParams{
		URL: srv.URL, ServerType: "http", TerminateOnClose: new(false),
	})
	require.NoError(t, err)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "termination-client", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, session.Close()) })
	require.NoError(t, session.Ping(ctx, nil))
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("MCP event stream did not start")
	}

	require.NoError(t, session.Close())
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("closing the MCP client did not close its local event stream")
	}
	require.Zero(t, deletes.Load())
	var remaining int
	for range server.Sessions() {
		remaining++
	}
	require.Equal(t, 1, remaining, "closing the client must preserve the remote session")
}
