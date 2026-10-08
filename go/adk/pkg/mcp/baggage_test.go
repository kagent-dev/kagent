package mcp

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"
	"google.golang.org/genai"
)

func TestSessionTraceStripper(t *testing.T) {
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{1},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	request := trace.ContextWithRemoteSpanContext(context.Background(), spanContext)
	request = telemetry.WithBaggage(request, attribute.String("gen_ai.conversation.id", "conversation-1"))
	active, _ := startCallScope(request)
	ended, end := startCallScope(request)
	end()

	for _, tc := range []struct {
		name   string
		ctx    context.Context
		method string
		trace  bool
	}{
		{name: "call in progress", ctx: active, method: http.MethodPost, trace: true},
		{name: "session context after its call ended", ctx: ended, method: http.MethodPost},
		{name: "no call", ctx: request, method: http.MethodPost},
		{name: "event stream opened by a call", ctx: active, method: http.MethodGet},
		{name: "session close", ctx: active, method: http.MethodDelete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

			base := &recordingTransport{}
			transport := otelhttp.NewTransport(base,
				otelhttp.WithFilter(traceMCPRequest),
				otelhttp.WithTracerProvider(provider),
				otelhttp.WithPropagators(propagation.NewCompositeTextMapPropagator(
					propagation.TraceContext{},
					propagation.Baggage{},
				)),
			)
			req, err := http.NewRequestWithContext(tc.ctx, tc.method, "http://mcp.example.test/mcp", nil)
			require.NoError(t, err)
			req.Header.Set("Mcp-Session-Id", "session")

			resp, err := transport.RoundTrip(req)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, "session", base.header.Get("Mcp-Session-Id"), "other headers are kept")
			if tc.trace {
				require.NotEmpty(t, base.header.Get("traceparent"))
				require.Equal(t, "gen_ai.conversation.id=conversation-1", base.header.Get("baggage"))
				require.Len(t, recorder.Ended(), 1, "a traced MCP request creates one HTTP span")
			} else {
				require.Empty(t, base.header.Get("traceparent"))
				require.Empty(t, base.header.Get("baggage"))
				require.Empty(t, recorder.Started(), "a filtered MCP request creates no HTTP span")
				require.Empty(t, recorder.Ended(), "a filtered MCP request exports no HTTP span")
			}
		})
	}
}

func TestCallScopeToolListing(t *testing.T) {
	inner := &recordingTool{name: "add_numbers"}
	toolset := &staticToolset{tools: []tool.Tool{inner}}
	_, err := withCallScope(toolset).Tools(readonlyContext{ctx: context.Background()})
	require.NoError(t, err)
	require.True(t, toolset.listedInScope, "tool listing runs in a call scope")
	require.False(t, inCallScope(toolset.listCtx), "the scope ends with the listing")
}

func TestScopedToolMatchesMCPTool(t *testing.T) {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "capability-test", Version: "1.0.0"}, nil)
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "add_numbers"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		return nil, map[string]any{"result": 0}, nil
	})
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })

	toolset, err := mcptoolset.New(mcptoolset.Config{Transport: clientTransport})
	require.NoError(t, err)
	tools, err := toolset.Tools(readonlyContext{ctx: t.Context()})
	require.NoError(t, err)
	require.Len(t, tools, 1)

	innerType := reflect.TypeOf(tools[0])
	require.Equal(t, "*mcptoolset.mcpTool", innerType.String(), "test must inspect ADK's concrete MCP tool")
	wrappedType := reflect.TypeFor[scopedTool]()
	for method := range innerType.Methods() {
		_, ok := wrappedType.MethodByName(method.Name)
		require.Truef(t, ok, "scopedTool does not preserve %s.%s; review the new ADK MCP tool capability", innerType, method.Name)
	}
}

func TestCallScopeToolCall(t *testing.T) {
	inner := &recordingTool{name: "add_numbers"}
	tools, err := withCallScope(&staticToolset{tools: []tool.Tool{inner}}).Tools(readonlyContext{ctx: context.Background()})
	require.NoError(t, err)
	require.Len(t, tools, 1)
	wrapped := tools[0].(functionTool)

	// The wrapper replaces the inner tool in the request, so ADK runs it.
	req := &model.LLMRequest{Tools: map[string]any{}}
	require.NoError(t, wrapped.ProcessRequest(nil, req))
	wrapped, ok := req.Tools["add_numbers"].(functionTool)
	require.True(t, ok, "the wrapper replaces the inner tool in the request")

	request := telemetry.WithBaggage(context.Background(), attribute.String("gen_ai.conversation.id", "conversation"))
	_, err = wrapped.Run(toolContext{ctx: request, callID: "call-1"}, nil)
	require.NoError(t, err)
	require.True(t, inner.ranInScope, "the tool call runs in a call scope")
	require.False(t, inCallScope(inner.ctx), "the scope ends with the call")
	got := baggage.FromContext(inner.ctx)
	require.Equal(t, "conversation", got.Member("gen_ai.conversation.id").Value())
	require.Equal(t, "add_numbers", got.Member(string(conv.GenAIToolNameKey)).Value())
	require.Equal(t, "call-1", got.Member(string(conv.GenAIToolCallIDKey)).Value())
	require.Equal(t, "call-1", inner.ctx.FunctionCallID(), "the tool keeps its ADK context")
}

type recordingTransport struct{ header http.Header }

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.header = req.Header.Clone()
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
}

type staticToolset struct {
	tools         []tool.Tool
	listCtx       context.Context
	listedInScope bool
}

func (*staticToolset) Name() string { return "static" }

func (s *staticToolset) Tools(ctx adkagent.ReadonlyContext) ([]tool.Tool, error) {
	s.listCtx, s.listedInScope = ctx, inCallScope(ctx)
	return s.tools, nil
}

type recordingTool struct {
	name       string
	ctx        adkagent.Context
	ranInScope bool
}

func (t *recordingTool) Name() string        { return t.name }
func (t *recordingTool) Description() string { return "" }
func (t *recordingTool) IsLongRunning() bool { return false }
func (t *recordingTool) Declaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{Name: t.name}
}

func (t *recordingTool) ProcessRequest(_ adkagent.Context, req *model.LLMRequest) error {
	req.Tools[t.name] = t
	return nil
}

func (t *recordingTool) Run(ctx adkagent.Context, _ any) (map[string]any, error) {
	t.ctx, t.ranInScope = ctx, inCallScope(ctx)
	return map[string]any{}, nil
}

// toolContext and readonlyContext are the parts of ADK contexts the wrappers
// use.
type toolContext struct {
	adkagent.Context
	ctx    context.Context
	callID string
}

func (c toolContext) Value(key any) any      { return c.ctx.Value(key) }
func (c toolContext) FunctionCallID() string { return c.callID }

type readonlyContext struct {
	adkagent.ReadonlyContext
	ctx context.Context
}

func (c readonlyContext) Deadline() (time.Time, bool) { return c.ctx.Deadline() }
func (c readonlyContext) Done() <-chan struct{}       { return c.ctx.Done() }
func (c readonlyContext) Err() error                  { return c.ctx.Err() }
func (c readonlyContext) Value(key any) any           { return c.ctx.Value(key) }
