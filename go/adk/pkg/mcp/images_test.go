package mcp

import (
	"context"
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
	sink := &imageSink{}
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
	for _, tc := range []struct {
		name    string
		content []mcpsdk.Content
		output  string
		images  any
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			content = tc.content
			result, err := wrapped.Run(runContext{ContextMock: &adkagent.ContextMock{}, Context: t.Context()}, map[string]any{})
			require.NoError(t, err)
			require.Equal(t, tc.output, result["output"])
			require.Equal(t, tc.images, result[ToolResultImagesKey])
		})
	}
}

// runContext is an ADK tool context backed by a Go context, enough for ADK's
// MCP tool to run.
type runContext struct {
	*adkagent.ContextMock
	context.Context
}

func (c runContext) Deadline() (time.Time, bool) { return c.Context.Deadline() }
func (c runContext) Done() <-chan struct{}       { return c.Context.Done() }
func (c runContext) Err() error                  { return c.Context.Err() }
func (c runContext) Value(key any) any           { return c.Context.Value(key) }
