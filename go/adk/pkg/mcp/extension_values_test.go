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
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool/mcptoolset"
	"google.golang.org/adk/v2/tool/toolconfirmation"
)

// callContext is the part of an ADK tool context an unconfirmed MCP tool call
// uses.
type callContext struct {
	adkagent.Context
	ctx context.Context
}

func (c callContext) Deadline() (time.Time, bool)                          { return c.ctx.Deadline() }
func (c callContext) Done() <-chan struct{}                                { return c.ctx.Done() }
func (c callContext) Err() error                                           { return c.ctx.Err() }
func (c callContext) Value(key any) any                                    { return c.ctx.Value(key) }
func (c callContext) ToolConfirmation() *toolconfirmation.ToolConfirmation { return nil }

// metaServer serves one tool and records the `_meta` and protocol revision of
// its last tools/call. It declares the given MCP extensions.
func metaServer(t *testing.T, stateless bool, extensions ...string) (string, func() (map[string]any, string)) {
	t.Helper()
	var mu sync.Mutex
	var gotMeta map[string]any
	var gotVersion string
	caps := &mcpsdk.ServerCapabilities{}
	for _, id := range extensions {
		caps.AddExtension(id, nil)
	}
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "meta", Version: "0.0.1"}, &mcpsdk.ServerOptions{Capabilities: caps})
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "echo", Description: "Echoes"}, func(_ context.Context, request *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, any, error) {
		mu.Lock()
		defer mu.Unlock()
		gotMeta = request.Params.GetMeta()
		gotVersion = request.ProtocolVersion()
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "ok"}}}, nil, nil
	})
	httpServer := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, &mcpsdk.StreamableHTTPOptions{Stateless: stateless}))
	t.Cleanup(func() {
		httpServer.CloseClientConnections()
		httpServer.Close()
	})
	return httpServer.URL, func() (map[string]any, string) {
		mu.Lock()
		defer mu.Unlock()
		return gotMeta, gotVersion
	}
}

func callEcho(t *testing.T, serverURL string, values map[string]any) {
	t.Helper()
	discovery := &mcpAppDiscovery{listedAppNames: map[string]bool{}, appToolNames: map[string]bool{}}
	toolset, err := mcptoolset.New(mcptoolset.Config{
		Transport: &mcpsdk.StreamableClientTransport{Endpoint: serverURL},
		Client:    newMCPAppClient(nil, discovery),
	})
	require.NoError(t, err)
	ctx := callContext{ctx: WithExtensionValues(t.Context(), values)}
	tools, err := toolset.Tools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	runnable, ok := tools[0].(interface {
		Run(ctx adkagent.Context, args any) (map[string]any, error)
	})
	require.True(t, ok, "tool = %T", tools[0])
	_, err = runnable.Run(ctx, map[string]any{})
	require.NoError(t, err)
}

func TestToolCallSendsExtensionValuesOnlyToServersThatDeclareThem(t *testing.T) {
	values := map[string]any{
		"com.example/session": "a1",
		"com.example/config":  map[string]any{"format": "csv", "limit": 10.0},
		"org.other/flag":      true,
	}
	for _, test := range []struct {
		name      string
		stateless bool
		version   string
	}{
		{name: "2026-07-28", stateless: true, version: "2026-07-28"},
		{name: "2025-11-25", stateless: false, version: "2025-11-25"},
	} {
		t.Run(test.name, func(t *testing.T) {
			declaring, declaringReceived := metaServer(t, test.stateless, "com.example/session", "com.example/config")
			callEcho(t, declaring, values)
			meta, version := declaringReceived()
			require.Equal(t, test.version, version)
			require.Equal(t, "a1", meta["com.example/session"])
			require.Equal(t, map[string]any{"format": "csv", "limit": 10.0}, meta["com.example/config"])
			require.NotContains(t, meta, "org.other/flag", "a value goes only to a server that declared its extension")

			other, otherReceived := metaServer(t, test.stateless)
			callEcho(t, other, values)
			meta, _ = otherReceived()
			for id := range values {
				require.NotContains(t, meta, id, "a server that declared no extension gets no value")
			}

			callEcho(t, declaring, nil)
			meta, _ = declaringReceived()
			require.NotContains(t, meta, "com.example/session", "a turn without values sends none")
		})
	}
}
