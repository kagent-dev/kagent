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

// ADK renders MCP image content as a text placeholder, dropping the bytes. The
// toolset's client moves them into a per-call sink, imageTool stashes them by
// call ID, and the A2A converter adds them to the result sent to the UI. They
// stay out of the ADK session, so the model and session history never see them.

// ToolResultImagesKey holds a note of the images in a tool result, and the
// images themselves ([]ToolImage) in the A2A result.
const ToolResultImagesKey = "images"

const (
	// Caps on the base64 of one image, one call and one A2A task. The task store
	// resends the whole task under a 16 MiB gRPC limit. Each image also counts
	// toolImageOverheadBytes toward the call and task caps.
	maxToolImageBytes      = 2 << 20
	maxToolCallImageBytes  = 4 << 20
	maxTaskImageBytes      = 6 << 20
	toolImageOverheadBytes = 64
	maxToolCallImages      = 16
)

// ToolCallKey identifies a tool call. Some providers reuse call IDs, so the
// invocation scopes them.
type ToolCallKey struct {
	InvocationID string
	CallID       string
}

// toolImages holds an execution's images until their function responses are
// converted, and counts the image bytes the task carries.
type toolImages struct {
	sync.Mutex
	spent int
	calls map[ToolCallKey][]ToolImage
}

type toolImagesKey struct{}

// WithToolImages returns ctx with an image store. spent is the image bytes
// earlier executions already added to the task. Without a store, tool calls
// keep ADK's placeholders.
func WithToolImages(ctx context.Context, spent int) context.Context {
	return context.WithValue(ctx, toolImagesKey{}, &toolImages{spent: spent, calls: map[ToolCallKey][]ToolImage{}})
}

func toolImagesFrom(ctx context.Context) *toolImages {
	s, _ := ctx.Value(toolImagesKey{}).(*toolImages)
	return s
}

// ToolImageCost returns the bytes an image counts toward the task cap.
func ToolImageCost(data string) int {
	return len(data) + toolImageOverheadBytes
}

// reserve counts n bytes against the task budget, or reports false if they do
// not fit.
func (s *toolImages) reserve(n int) bool {
	s.Lock()
	defer s.Unlock()
	if s.spent+n > maxTaskImageBytes {
		return false
	}
	s.spent += n
	return true
}

// StashToolImages keeps a call's images until its function response is converted.
func StashToolImages(ctx context.Context, call ToolCallKey, images []ToolImage) {
	s := toolImagesFrom(ctx)
	if s == nil {
		return
	}
	s.Lock()
	defer s.Unlock()
	s.calls[call] = append(s.calls[call], images...)
}

// TakeToolImages returns a call's images and forgets them.
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

// newToolsetClient returns the toolset's MCP client, which advertises MCP Apps
// support and captures returned images.
func newToolsetClient() *mcpsdk.Client {
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "adk-mcp-client", Version: mcpClientVersion}, &mcpsdk.ClientOptions{Capabilities: mcpUIClientCapabilities()})
	client.AddSendingMiddleware(captureImages)
	return client
}

// captureImages moves renderable images of a successful tools/call result into
// the call's sink. Errors, other types, empty images and images over a cap keep
// their placeholder; the task cap is checked before removal so nothing is lost.
//
// ADK joins blocks with a newline around placeholders, so a removed image
// between two blocks leaves a newline to keep the text apart.
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

// capturableImage returns content and its cost if the UI renders its type and
// it fits the call's caps after count images totalling captured bytes.
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

// isRenderableImage mirrors RENDERABLE_IMAGE in ui/src/components/chat/toolImages.ts.
func isRenderableImage(mimeType string) bool {
	switch strings.ToLower(mimeType) {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

// contentText returns a text block's text, or "" for other blocks.
func contentText(content mcpsdk.Content) string {
	if text, ok := content.(*mcpsdk.TextContent); ok && text != nil {
		return text.Text
	}
	return ""
}

// withImages stashes the images captured during each of ts's tool calls.
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

// ProcessRequest declares the inner tool, then registers the wrapper in its place.
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
		// Nowhere to stash images: keep ADK's placeholders.
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
