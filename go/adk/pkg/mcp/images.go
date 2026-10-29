package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
)

// ADK's MCP tool renders image content as a text placeholder, so the image
// bytes never reach the tool result. The toolset's MCP client moves them from
// the raw tools/call response into a sink carried by the call's context.
// imageTool notes them in the tool result and keeps them, by function call ID,
// in the execution's tool image store, and the A2A converter puts them back
// into the result it sends to the UI. The bytes stay out of the ADK session, so
// neither the model nor the session history (including another agent's view of
// it) carries them.

// ToolResultImagesKey is the tool result key holding a note of the images an
// MCP tool returned. In the A2A result it holds the images, as a list of
// ToolImage.
const ToolResultImagesKey = "images"

const (
	// maxToolImageBytes, maxToolCallImageBytes and maxTaskImageBytes cap the
	// base64 an image, all images of one call, and all images of one A2A task
	// add to the task. The task store resends the whole task on each update
	// under a 16 MiB gRPC limit. Each image also counts toolImageOverheadBytes
	// toward the call and task caps.
	maxToolImageBytes      = 2 << 20
	maxToolCallImageBytes  = 4 << 20
	maxTaskImageBytes      = 6 << 20
	toolImageOverheadBytes = 64
	// maxToolCallImages caps the number of images captured from one call.
	maxToolCallImages = 16
)

// ToolCallKey identifies a tool call within an execution. Some model providers
// reuse call IDs such as "call_0", so the invocation scopes them.
type ToolCallKey struct {
	InvocationID string
	CallID       string
}

// toolImages holds the images MCP tool calls returned during one execution of
// an A2A task until their function responses are converted, and counts the
// image bytes the task carries. It lives on the execution's context, so it is
// gone when the execution ends.
type toolImages struct {
	sync.Mutex
	spent int
	calls map[ToolCallKey][]ToolImage
}

type toolImagesKey struct{}

// WithToolImages returns ctx carrying a store for the images MCP tool calls
// return during one execution of an A2A task. spent is the image bytes earlier
// executions already added to the task. Without a store, tool calls keep ADK's
// image placeholders.
func WithToolImages(ctx context.Context, spent int) context.Context {
	return context.WithValue(ctx, toolImagesKey{}, &toolImages{spent: spent, calls: map[ToolCallKey][]ToolImage{}})
}

func toolImagesFrom(ctx context.Context) *toolImages {
	s, _ := ctx.Value(toolImagesKey{}).(*toolImages)
	return s
}

// ToolImageCost returns the bytes an image with base64 data counts toward the
// task cap.
func ToolImageCost(data string) int {
	return len(data) + toolImageOverheadBytes
}

// reserve counts n image bytes against the task's budget, or reports false and
// counts nothing if they do not fit.
func (s *toolImages) reserve(n int) bool {
	s.Lock()
	defer s.Unlock()
	if s.spent+n > maxTaskImageBytes {
		return false
	}
	s.spent += n
	return true
}

// StashToolImages keeps the images an MCP tool call returned in ctx's store
// until its function response is converted to A2A. Images of a call already
// stashed are added to.
func StashToolImages(ctx context.Context, call ToolCallKey, images []ToolImage) {
	s := toolImagesFrom(ctx)
	if s == nil {
		return
	}
	s.Lock()
	defer s.Unlock()
	s.calls[call] = append(s.calls[call], images...)
}

// TakeToolImages returns the images an MCP tool call returned and forgets
// them. Each function response is converted to A2A once.
func TakeToolImages(ctx context.Context, call ToolCallKey) []ToolImage {
	s := toolImagesFrom(ctx)
	if s == nil {
		return nil
	}
	s.Lock()
	defer s.Unlock()
	images := s.calls[call]
	delete(s.calls, call)
	return images
}

// ToolImage is an image returned by an MCP tool.
type ToolImage struct {
	MIMEType string `json:"mimeType"`
	// Data is the base64-encoded image.
	Data string `json:"data"`
}

type imageSinkKey struct{}

// imageSink collects the images of one tool call.
type imageSink struct {
	store  *toolImages
	images []ToolImage
}

// newToolsetClient returns the toolset's MCP client. It advertises MCP Apps
// support like the classification client and captures returned images.
func newToolsetClient() *mcpsdk.Client {
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "adk-mcp-client", Version: mcpClientVersion}, &mcpsdk.ClientOptions{Capabilities: mcpUIClientCapabilities()})
	client.AddSendingMiddleware(captureImages)
	return client
}

// captureImages moves the renderable images of a successful tools/call result
// into the call's image sink, if it has one, so ADK formats the result without
// their placeholders. Errors keep their content for the model. Other image
// types, empty images, and images over the count or size caps keep their
// placeholder. The task cap is checked here, before an image is removed,
// so an image the stash will not hold is never dropped from the output.
//
// ADK joins content blocks with a newline wherever either side is a
// placeholder, unless one already ends or starts there. A removed image between
// two blocks becomes that newline, so the text around it stays apart; at either
// end of the output it leaves nothing.
func captureImages(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
	return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
		result, err := next(ctx, method, req)
		sink, _ := ctx.Value(imageSinkKey{}).(*imageSink)
		res, ok := result.(*mcpsdk.CallToolResult)
		if err != nil || sink == nil || !ok || res == nil || res.IsError {
			return result, err
		}
		kept := make([]mcpsdk.Content, 0, len(res.Content))
		// previous is the last kept block ADK renders as non-empty text.
		var previous mcpsdk.Content
		removed := false
		captured := 0
		for _, content := range res.Content {
			if image, cost := capturableImage(content, captured, len(sink.images)); image != nil && sink.store.reserve(cost) {
				captured += cost
				sink.images = append(sink.images, ToolImage{
					MIMEType: image.MIMEType,
					Data:     base64.StdEncoding.EncodeToString(image.Data),
				})
				removed = previous != nil
				continue
			}
			text, isText := content.(*mcpsdk.TextContent)
			if isText && text != nil && text.Text == "" {
				kept = append(kept, content)
				continue
			}
			if removed && !strings.HasSuffix(contentText(previous), "\n") && !strings.HasPrefix(contentText(content), "\n") {
				kept = append(kept, &mcpsdk.TextContent{Text: "\n"})
			}
			removed = false
			kept = append(kept, content)
			previous = content
		}
		res.Content = kept
		return result, nil
	}
}

// capturableImage returns content as an image to capture, with the bytes it
// counts toward the caps, if the UI renders its type and it fits the call's
// caps after count images of captured bytes.
func capturableImage(content mcpsdk.Content, captured, count int) (*mcpsdk.ImageContent, int) {
	image, ok := content.(*mcpsdk.ImageContent)
	if !ok || image == nil || len(image.Data) == 0 || count >= maxToolCallImages || !isRenderableImage(image.MIMEType) {
		return nil, 0
	}
	size := base64.StdEncoding.EncodedLen(len(image.Data))
	cost := size + toolImageOverheadBytes
	if size > maxToolImageBytes || captured+cost > maxToolCallImageBytes {
		return nil, 0
	}
	return image, cost
}

// isRenderableImage reports whether the UI renders images of mimeType. Keep in
// step with RENDERABLE_IMAGE in ui/src/components/chat/toolImages.ts.
func isRenderableImage(mimeType string) bool {
	switch strings.ToLower(mimeType) {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

// contentText returns the text of a text block. Other blocks are treated as
// having no newline at either end, which holds for ADK's placeholders.
func contentText(content mcpsdk.Content) string {
	if text, ok := content.(*mcpsdk.TextContent); ok && text != nil {
		return text.Text
	}
	return ""
}

// withImages notes and stashes the images captured during each tool call of
// ts.
func withImages(ts tool.Toolset) tool.Toolset {
	return imageToolset{ts}
}

type imageToolset struct {
	tool.Toolset
}

func (s imageToolset) Tools(ctx adkagent.ReadonlyContext) ([]tool.Tool, error) {
	tools, err := s.Toolset.Tools(ctx)
	if err != nil {
		return nil, err
	}
	wrapped := make([]tool.Tool, len(tools))
	for i, t := range tools {
		if fn, ok := t.(functionTool); ok {
			wrapped[i] = imageTool{functionTool: fn}
		} else {
			wrapped[i] = t
		}
	}
	return wrapped, nil
}

type imageTool struct {
	functionTool
}

// ProcessRequest lets the inner tool declare itself, then registers the
// wrapper in its place so that ADK runs the wrapper.
func (t imageTool) ProcessRequest(ctx adkagent.Context, req *model.LLMRequest) error {
	if err := t.functionTool.ProcessRequest(ctx, req); err != nil {
		return err
	}
	if _, ok := req.Tools[t.Name()]; ok {
		req.Tools[t.Name()] = t
	}
	return nil
}

func (t imageTool) Run(ctx adkagent.Context, args any) (map[string]any, error) {
	call := ToolCallKey{InvocationID: ctx.InvocationID(), CallID: ctx.FunctionCallID()}
	store := toolImagesFrom(ctx)
	if store == nil || call.InvocationID == "" || call.CallID == "" {
		// The images could not be stashed, so leave ADK's placeholders.
		return t.functionTool.Run(ctx, args)
	}
	sink := &imageSink{store: store}
	result, err := t.functionTool.Run(valuesContext{Context: ctx, values: context.WithValue(ctx, imageSinkKey{}, sink)}, args)
	if err != nil || len(sink.images) == 0 {
		return result, err
	}
	if result == nil {
		result = map[string]any{}
	}
	StashToolImages(ctx, call, sink.images)
	result[ToolResultImagesKey] = fmt.Sprintf("%d image(s) returned for display; you cannot see their contents", len(sink.images))
	return result, nil
}
