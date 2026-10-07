package mcp

import (
	"context"
	"net/http"
	"testing"

	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"
)

func TestSessionTraceStripper(t *testing.T) {
	active, _ := startCallScope(context.Background())
	ended, end := startCallScope(context.Background())
	end()

	for _, tc := range []struct {
		name   string
		ctx    context.Context
		method string
		keep   bool
	}{
		{name: "call in progress", ctx: active, method: http.MethodPost, keep: true},
		{name: "session context after its call ended", ctx: ended, method: http.MethodPost},
		{name: "no call", ctx: context.Background(), method: http.MethodPost},
		{name: "event stream opened by a call", ctx: active, method: http.MethodGet},
		{name: "session close", ctx: active, method: http.MethodDelete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := &recordingTransport{}
			req, err := http.NewRequestWithContext(tc.ctx, tc.method, "http://mcp.example.test/mcp", nil)
			require.NoError(t, err)
			for _, header := range traceHeaders {
				req.Header.Set(header, "value")
			}
			req.Header.Set("Mcp-Session-Id", "session")

			_, err = sessionTraceStripper{base: base}.RoundTrip(req)
			require.NoError(t, err)
			for _, header := range traceHeaders {
				if tc.keep {
					require.Equal(t, "value", base.header.Get(header), header)
				} else {
					require.Empty(t, base.header.Get(header), header)
				}
			}
			require.Equal(t, "session", base.header.Get("Mcp-Session-Id"), "other headers are kept")
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

func TestCallScopeToolCall(t *testing.T) {
	inner := &recordingTool{name: "add_numbers"}
	tools, err := withCallScope(&staticToolset{tools: []tool.Tool{inner}}).Tools(readonlyContext{ctx: context.Background()})
	require.NoError(t, err)
	require.Len(t, tools, 1)
	wrapped := tools[0].(functionTool)

	// The wrapper replaces the inner tool in the request, so ADK runs it.
	req := &model.LLMRequest{Tools: map[string]any{}}
	require.NoError(t, wrapped.ProcessRequest(nil, req))
	require.Same(t, wrapped, req.Tools["add_numbers"])

	request := telemetry.WithBaggage(context.Background(), attribute.String("gen_ai.conversation.id", "conversation"))
	_, err = wrapped.Run(toolContext{ctx: request, callID: "call-1"}, nil)
	require.NoError(t, err)
	require.True(t, inner.ranInScope, "the tool call runs in a call scope")
	require.False(t, inCallScope(inner.ctx), "the scope ends with the call")
	got := baggage.FromContext(inner.ctx)
	require.Equal(t, "conversation", got.Member("gen_ai.conversation.id").Value())
	require.Equal(t, "add_numbers", got.Member(telemetry.BaggageToolName).Value())
	require.Equal(t, "call-1", got.Member(telemetry.BaggageToolCallID).Value())
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

func (c readonlyContext) Value(key any) any { return c.ctx.Value(key) }
