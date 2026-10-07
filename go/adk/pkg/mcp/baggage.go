package mcp

import (
	"context"
	"net/http"
	"sync/atomic"

	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"
)

// An MCP session is shared by every request the runtime serves, and opens
// with the context of the call that first needs it. The SDK keeps that
// context's values, not its cancellation, for the session's own requests: its
// event stream, the replies to server requests, and its close. Trace context
// and baggage taken from it would name a request that has since ended, so only
// requests made while a call is in progress carry them.

// callScope marks the context of a tool listing or tool call. It is active
// only while that call runs; a session context detached from it keeps the
// marker but sees it inactive.
type callScope struct{ active atomic.Bool }

type callScopeKey struct{}

func startCallScope(ctx context.Context) (context.Context, func()) {
	scope := &callScope{}
	scope.active.Store(true)
	return context.WithValue(ctx, callScopeKey{}, scope), func() { scope.active.Store(false) }
}

func inCallScope(ctx context.Context) bool {
	scope, _ := ctx.Value(callScopeKey{}).(*callScope)
	return scope != nil && scope.active.Load()
}

// traceHeaders are the W3C headers a request inherits from the context that
// sends it.
var traceHeaders = []string{"traceparent", "tracestate", "baggage"}

// sessionTraceStripper removes trace context and baggage from the session's
// own requests. It sits inside the OpenTelemetry transport, which injects them.
type sessionTraceStripper struct {
	base http.RoundTripper
}

func (s sessionTraceStripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// The event stream (GET) and close (DELETE) belong to the session even when
	// a call happens to open them.
	if req.Method == http.MethodPost && inCallScope(req.Context()) {
		return s.base.RoundTrip(req)
	}
	req = req.Clone(req.Context())
	for _, header := range traceHeaders {
		req.Header.Del(header)
	}
	return s.base.RoundTrip(req)
}

// withCallScope runs each tool listing and tool call of ts in a call scope.
// Tool calls also add the tool name and call ID to the request's baggage.
func withCallScope(ts tool.Toolset) tool.Toolset {
	return &scopedToolset{inner: ts}
}

type scopedToolset struct {
	inner tool.Toolset
}

func (s *scopedToolset) Name() string { return s.inner.Name() }

func (s *scopedToolset) Tools(ctx adkagent.ReadonlyContext) ([]tool.Tool, error) {
	values, end := startCallScope(ctx)
	defer end()
	tools, err := s.inner.Tools(readonlyValuesContext{ReadonlyContext: ctx, values: values})
	if err != nil {
		return nil, err
	}
	wrapped := make([]tool.Tool, len(tools))
	for i, t := range tools {
		if fn, ok := t.(functionTool); ok {
			wrapped[i] = &scopedTool{functionTool: fn}
		} else {
			wrapped[i] = t
		}
	}
	return wrapped, nil
}

// functionTool is the shape ADK runs and declares to the model. MCP tools
// implement it.
type functionTool interface {
	tool.Tool
	Declaration() *genai.FunctionDeclaration
	ProcessRequest(ctx adkagent.Context, req *model.LLMRequest) error
	Run(ctx adkagent.Context, args any) (map[string]any, error)
}

type scopedTool struct {
	functionTool
}

// ProcessRequest lets the inner tool declare itself, then registers the
// wrapper in its place so that ADK runs the wrapper.
func (t *scopedTool) ProcessRequest(ctx adkagent.Context, req *model.LLMRequest) error {
	if err := t.functionTool.ProcessRequest(ctx, req); err != nil {
		return err
	}
	if _, ok := req.Tools[t.Name()]; ok {
		req.Tools[t.Name()] = t
	}
	return nil
}

func (t *scopedTool) Run(ctx adkagent.Context, args any) (map[string]any, error) {
	values, end := startCallScope(telemetry.WithBaggage(ctx,
		attribute.String(telemetry.BaggageToolName, t.Name()),
		attribute.String(telemetry.BaggageToolCallID, ctx.FunctionCallID()),
	))
	defer end()
	return t.functionTool.Run(valuesContext{Context: ctx, values: values}, args)
}

// valuesContext and readonlyValuesContext are ADK contexts with the values of
// another context derived from them, because ADK contexts cannot be rebuilt
// around new values.
type valuesContext struct {
	adkagent.Context
	values context.Context
}

func (c valuesContext) Value(key any) any { return c.values.Value(key) }

type readonlyValuesContext struct {
	adkagent.ReadonlyContext
	values context.Context
}

func (c readonlyValuesContext) Value(key any) any { return c.values.Value(key) }
