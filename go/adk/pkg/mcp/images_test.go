package mcp

import (
	"cmp"
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"
)

func TestCaptureImages(t *testing.T) {
	text := &mcpsdk.TextContent{Text: "chart"}
	image := &mcpsdk.ImageContent{MIMEType: "image/png", Data: []byte("png")}
	result := &mcpsdk.CallToolResult{}
	handler := captureImages(func(context.Context, string, mcpsdk.Request) (mcpsdk.Result, error) {
		return result, nil
	})
	withSink := func(sink *imageSink) context.Context {
		return context.WithValue(context.Background(), imageSinkKey{}, sink)
	}

	result.Content = []mcpsdk.Content{text, image}
	_, err := handler(context.Background(), "tools/call", nil)
	require.NoError(t, err)
	require.Equal(t, []mcpsdk.Content{text, image}, result.Content, "a call without a sink is passed through")

	result.IsError = true
	sink := &imageSink{store: &toolImages{}}
	_, err = handler(withSink(sink), "tools/call", nil)
	require.NoError(t, err)
	require.Equal(t, []mcpsdk.Content{text, image}, result.Content, "an error result keeps its content")
	require.Empty(t, sink.images)

	result.IsError = false
	_, err = handler(withSink(sink), "tools/call", nil)
	require.NoError(t, err)
	require.Equal(t, []mcpsdk.Content{text}, result.Content, "images are moved out of the result")
	require.Equal(t, []ToolImage{{MIMEType: "image/png", Data: "cG5n"}}, sink.images)
}

func TestImageToolReturnsMCPImages(t *testing.T) {
	var content []mcpsdk.Content
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "image-test", Version: "1.0.0"}, nil)
	server.AddTool(&mcpsdk.Tool{Name: "render_chart", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return &mcpsdk.CallToolResult{Content: content}, nil
	})
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })

	inner, err := mcptoolset.New(mcptoolset.Config{Client: newToolsetClient(), Transport: clientTransport})
	require.NoError(t, err)
	// Wrapped as addToolset wraps an approval-gated HTTP server.
	toolset := tool.WithConfirmation(withCallScope(withImages(inner)), false, nil)
	tools, err := toolset.Tools(readonlyContext{ctx: t.Context()})
	require.NoError(t, err)
	require.Len(t, tools, 1)

	// The outermost wrapper replaces the MCP tool in the request, so ADK runs
	// it and every wrapper inside it.
	req := &model.LLMRequest{}
	require.NoError(t, tools[0].(functionTool).ProcessRequest(nil, req))
	wrapped, ok := req.Tools["render_chart"].(functionTool)
	require.True(t, ok)
	require.Same(t, tools[0], wrapped, "the outermost wrapper is registered")

	ui := serverSession.InitializeParams().Capabilities.Extensions[mcpUIExtensionName]
	require.NotNil(t, ui, "the toolset client advertises MCP Apps support")

	text := func(s string) mcpsdk.Content { return &mcpsdk.TextContent{Text: s} }
	png := &mcpsdk.ImageContent{MIMEType: "image/png", Data: []byte("png")}
	captured := []ToolImage{{MIMEType: "image/png", Data: "cG5n"}}
	// A raw image of n bytes; 3/4 of maxToolImageBytes encodes to exactly the cap.
	image := func(n int) mcpsdk.Content {
		return &mcpsdk.ImageContent{MIMEType: "image/png", Data: make([]byte, n)}
	}
	encoded := func(n int) ToolImage {
		return ToolImage{MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(make([]byte, n))}
	}
	placeholder := func(n int) string { return fmt.Sprintf("[MCP image: mimeType=\"image/png\", size=%d bytes]", n) }
	// half is a raw image size that, with its overhead, counts exactly half the
	// call cap.
	half := (maxToolCallImageBytes/2 - toolImageOverheadBytes) * 3 / 4
	pngs := func(n int) []mcpsdk.Content {
		out := make([]mcpsdk.Content, n)
		for i := range out {
			out[i] = png
		}
		return out
	}
	// A task's context outlives its first case, so it derives from the test's.
	runCtx := t.Context()
	tasks := map[string]context.Context{}
	for _, tc := range []struct {
		name string
		// task defaults to the case name.
		task string
		// spent is the image bytes earlier executions added to the task.
		spent   int
		content []mcpsdk.Content
		output  string
		images  []ToolImage
	}{
		{name: "image between text", content: []mcpsdk.Content{text("chart"), png, text("https://example.test/panel")}, output: "chart\nhttps://example.test/panel", images: captured},
		{name: "image first", content: []mcpsdk.Content{png, text("https://example.test/panel")}, output: "https://example.test/panel", images: captured},
		{name: "image last", content: []mcpsdk.Content{text("chart"), png}, output: "chart", images: captured},
		{name: "text already ends the line", content: []mcpsdk.Content{text("chart\n"), png, text("url")}, output: "chart\nurl", images: captured},
		{name: "adjacent images", content: []mcpsdk.Content{text("a"), png, png, text("b")}, output: "a\nb", images: append(captured, captured...)},
		{name: "mime type case", content: []mcpsdk.Content{&mcpsdk.ImageContent{MIMEType: "IMAGE/PNG", Data: []byte("png")}}, output: "", images: []ToolImage{{MIMEType: "IMAGE/PNG", Data: "cG5n"}}},
		{
			name:    "unrenderable image keeps its placeholder",
			content: []mcpsdk.Content{text("chart"), &mcpsdk.ImageContent{MIMEType: "image/svg+xml", Data: []byte("svg")}},
			output:  "chart\n[MCP image: mimeType=\"image/svg+xml\", size=3 bytes]",
		},
		{
			name:    "image over the size cap keeps its placeholder",
			content: []mcpsdk.Content{image(maxToolImageBytes), png},
			output:  placeholder(maxToolImageBytes),
			images:  captured,
		},
		{
			name:    "images over the call cap keep their placeholder",
			content: []mcpsdk.Content{image(half), image(half), image(half)},
			output:  placeholder(half),
			images:  []ToolImage{encoded(half), encoded(half)},
		},
		{
			name:    "image overhead counts toward the call cap",
			content: []mcpsdk.Content{image(half + 24), image(half + 24)},
			output:  placeholder(half + 24),
			images:  []ToolImage{encoded(half + 24)},
		},
		{
			name:    "images over the count cap keep their placeholder",
			content: pngs(maxToolCallImages + 1),
			output:  placeholder(3),
			images:  slices.Repeat(captured, maxToolCallImages),
		},
		{
			name:    "empty image keeps its placeholder",
			content: []mcpsdk.Content{text("chart"), &mcpsdk.ImageContent{MIMEType: "image/png"}},
			output:  "chart\n" + placeholder(0),
		},
		{
			name:    "first call of a task",
			task:    "task-budget",
			content: []mcpsdk.Content{image(half), image(half)},
			output:  "",
			images:  []ToolImage{encoded(half), encoded(half)},
		},
		{
			name:    "images over the task cap keep their placeholder",
			task:    "task-budget",
			content: []mcpsdk.Content{image(half), image(half)},
			output:  placeholder(half),
			images:  []ToolImage{encoded(half)},
		},
		{
			name:    "another task has its own cap",
			content: []mcpsdk.Content{image(half), image(half)},
			output:  "",
			images:  []ToolImage{encoded(half), encoded(half)},
		},
		{
			name:    "a resumed task counts the images it already carries",
			spent:   maxTaskImageBytes - maxToolCallImageBytes/2,
			content: []mcpsdk.Content{image(half), image(half)},
			output:  placeholder(half),
			images:  []ToolImage{encoded(half)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content = tc.content
			call := ToolCallKey{InvocationID: tc.name, CallID: "call_0"}
			task := cmp.Or(tc.task, tc.name)
			if tasks[task] == nil {
				tasks[task] = WithToolImages(runCtx, tc.spent)
			}
			ctx := tasks[task]
			result, err := wrapped.Run(runContext{ContextMock: &adkagent.ContextMock{}, Context: ctx, invocationID: call.InvocationID, callID: call.CallID}, map[string]any{})
			require.NoError(t, err)
			require.Equal(t, tc.output, result["output"])
			// The result keeps only a note; the images wait for the A2A conversion.
			if tc.images == nil {
				require.NotContains(t, result, ToolResultImagesKey)
			} else {
				require.Equal(t, fmt.Sprintf("%d image(s) returned for display; you cannot see their contents", len(tc.images)), result[ToolResultImagesKey])
			}
			require.Equal(t, tc.images, TakeToolImages(ctx, call))
			require.Nil(t, TakeToolImages(ctx, call), "images are taken once")
		})
	}
}

func TestTakeToolImages(t *testing.T) {
	first := []ToolImage{{MIMEType: "image/png", Data: "Zmlyc3Q="}}
	second := []ToolImage{{MIMEType: "image/png", Data: "c2Vjb25k"}}
	ctx := WithToolImages(t.Context(), 0)
	StashToolImages(ctx, ToolCallKey{InvocationID: "invocation-a", CallID: "call_0"}, first)
	StashToolImages(ctx, ToolCallKey{InvocationID: "invocation-b", CallID: "call_0"}, second)
	StashToolImages(ctx, ToolCallKey{InvocationID: "invocation-b", CallID: "call_0"}, first)
	require.Nil(t, TakeToolImages(WithToolImages(t.Context(), 0), ToolCallKey{InvocationID: "invocation-a", CallID: "call_0"}), "another execution's images are not taken")
	require.Nil(t, TakeToolImages(t.Context(), ToolCallKey{InvocationID: "invocation-a", CallID: "call_0"}), "a context without a store has no images")
	require.Equal(t, append(slices.Clone(second), first...), TakeToolImages(ctx, ToolCallKey{InvocationID: "invocation-b", CallID: "call_0"}), "images of a call are added to")
	require.Equal(t, first, TakeToolImages(ctx, ToolCallKey{InvocationID: "invocation-a", CallID: "call_0"}), "call IDs are scoped by invocation")
}

// runContext is an ADK tool context backed by a Go context, enough for ADK's
// MCP tool to run.
type runContext struct {
	*adkagent.ContextMock
	context.Context
	invocationID string
	callID       string
}

func (c runContext) InvocationID() string   { return c.invocationID }
func (c runContext) FunctionCallID() string { return c.callID }

func (c runContext) Deadline() (time.Time, bool) { return c.Context.Deadline() }
func (c runContext) Done() <-chan struct{}       { return c.Context.Done() }
func (c runContext) Err() error                  { return c.Context.Err() }
func (c runContext) Value(key any) any           { return c.Context.Value(key) }
