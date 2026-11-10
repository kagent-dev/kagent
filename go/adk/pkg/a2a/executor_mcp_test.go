package a2a

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"

	"github.com/kagent-dev/kagent/go/adk/pkg/mcp"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func TestKAgentExecutorScopesMCPDiscoveryFailures(t *testing.T) {
	// Given a temporarily unavailable MCP catalog, when execution continues and
	// later resumes with reused IDs, assert that omission lasts only one execution.
	var unavailable atomic.Bool
	unavailable.Store(true)
	var listings atomic.Int32
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "execution-discovery", Version: "1"}, &mcpsdk.ServerOptions{
		SupportedProtocolVersions: []string{"2025-11-25"},
	})
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "available_tool"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		return nil, map[string]any{"result": "ok"}, nil
	})
	server.AddReceivingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			if method == "tools/list" {
				listings.Add(1)
				if unavailable.Load() {
					return nil, errors.New("catalog temporarily unavailable")
				}
			}
			return next(ctx, method, req)
		}
	})
	httpServer := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, &mcpsdk.StreamableHTTPOptions{Stateless: true}))
	t.Cleanup(httpServer.Close)
	healthy := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "healthy-discovery"}, &mcpsdk.ServerOptions{SupportedProtocolVersions: []string{"2025-11-25"}})
	mcpsdk.AddTool(healthy, &mcpsdk.Tool{Name: "healthy_tool"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		return nil, map[string]any{"result": "healthy"}, nil
	})
	healthyHTTP := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return healthy }, &mcpsdk.StreamableHTTPOptions{Stateless: true}))
	t.Cleanup(healthyHTTP.Close)
	toolsets := mcp.CreateToolsets(t.Context(), []*configv1.ADKHTTPMCPServerConfig{{
		Params: &configv1.ADKHTTPConnectionParams{Url: httpServer.URL},
	}, {
		Params: &configv1.ADKHTTPConnectionParams{Url: healthyHTTP.URL},
	}}, nil, nil, false, nil)
	require.Len(t, toolsets, 2)

	var visible [][]string
	builtin := &recordingExecutor{onExecute: func(ctx context.Context) {
		readonly := &executionDiscoveryContext{StrictContextMock: adkagent.NewStrictContextMock(ctx)}
		for range 2 {
			var names []string
			for _, toolset := range toolsets {
				tools, err := toolset.Tools(readonly)
				require.NoError(t, err, "discovery failures must leave the execution usable")
				for _, tool := range tools {
					names = append(names, tool.Name())
				}
			}
			slices.Sort(names)
			visible = append(visible, names)
			unavailable.Store(false)
		}
	}}
	executor := &KAgentExecutor{builtin: builtin, logger: slog.New(slog.DiscardHandler)}
	request := &a2asrv.ExecutorContext{
		TaskID: "reused-task", ContextID: "reused-session",
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("continue")),
	}
	for _, err := range executor.Execute(t.Context(), request) {
		require.NoError(t, err)
	}
	require.Equal(t, [][]string{{"healthy_tool"}, {"healthy_tool"}}, visible, "recovery during an execution must not retry its omitted server")
	require.EqualValues(t, 1, listings.Load())

	visible = nil
	for _, err := range executor.Execute(t.Context(), request) {
		require.NoError(t, err)
	}
	require.Equal(t, [][]string{{"available_tool", "healthy_tool"}, {"available_tool", "healthy_tool"}}, visible, "a later execution with the same IDs must retry discovery")
	require.Greater(t, listings.Load(), int32(1))

	// Given prior successful discovery, when the server goes down again, assert
	// that healthy tools remain available and a fresh execution retries recovery.
	unavailable.Store(true)
	before := listings.Load()
	visible = nil
	for _, err := range executor.Execute(t.Context(), request) {
		require.NoError(t, err)
	}
	require.Equal(t, [][]string{{"healthy_tool"}, {"healthy_tool"}}, visible)
	require.Equal(t, before+1, listings.Load(), "the failed catalog must stay omitted across model steps")
	visible = nil
	for _, err := range executor.Execute(t.Context(), request) {
		require.NoError(t, err)
	}
	require.Equal(t, [][]string{{"available_tool", "healthy_tool"}, {"available_tool", "healthy_tool"}}, visible)
}

type executionDiscoveryContext struct {
	adkagent.StrictContextMock
}

func (*executionDiscoveryContext) InvocationID() string { return "reused-invocation" }
func (*executionDiscoveryContext) SessionID() string    { return "reused-session" }

func TestKAgentExecutorSendsPublicContextIDToMCP(t *testing.T) {
	// Given a native ADK session ID that differs from the public context ID,
	// when a tool call reaches an MCP server, assert the public ID is sent.
	const header = "X-Kagent-Session-Id"
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "session-header", Version: "1"}, &mcpsdk.ServerOptions{
		SupportedProtocolVersions: []string{"2025-11-25"},
	})
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "echo"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		return nil, map[string]any{"result": "ok"}, nil
	})
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, &mcpsdk.StreamableHTTPOptions{Stateless: true})
	var mu sync.Mutex
	var seen []string
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get(header))
		mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(httpServer.Close)
	toolsets := mcp.CreateToolsets(t.Context(), []*configv1.ADKHTTPMCPServerConfig{{
		Params:          &configv1.ADKHTTPConnectionParams{Url: httpServer.URL},
		SessionIdHeader: header,
	}}, nil, nil, false, nil)
	require.Len(t, toolsets, 1)

	builtin := &recordingExecutor{onExecute: func(ctx context.Context) {
		readonly := &forkedNativeSessionContext{StrictContextMock: adkagent.NewStrictContextMock(ctx)}
		tools, err := toolsets[0].Tools(readonly)
		require.NoError(t, err)
		require.Len(t, tools, 1)
	}}
	executor := &KAgentExecutor{builtin: builtin, logger: slog.New(slog.DiscardHandler)}
	request := &a2asrv.ExecutorContext{
		TaskID: "task", ContextID: "public-context",
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("go")),
	}
	for _, err := range executor.Execute(t.Context(), request) {
		require.NoError(t, err)
	}

	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, seen, "public-context")
	require.NotContains(t, seen, "native-session")
}

type forkedNativeSessionContext struct {
	adkagent.StrictContextMock
}

func (*forkedNativeSessionContext) InvocationID() string { return "invocation" }
func (*forkedNativeSessionContext) SessionID() string    { return "native-session" }
