package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
)

// ADK's MCP tool renders image content as a text placeholder, so the image
// bytes never reach the tool result. The toolset's MCP client moves them from
// the raw tools/call response into a sink carried by the call's context.
// imageTool notes them in the tool result and stashes them by function call ID,
// and the A2A converter puts them back into the result it sends to the UI. The
// bytes stay out of the ADK session, so neither the model nor the session
// history (including another agent's view of it) carries them.

// ToolResultImagesKey is the tool result key holding a note of the images an
// MCP tool returned. In the A2A result it holds the images, as a list of
// ToolImage.
const ToolResultImagesKey = "images"

const (
	// stashedImagesTTL bounds how long images wait for a conversion that never
	// comes, such as for a cancelled run. Conversion follows the tool call
	// directly, in the same process.
	stashedImagesTTL = 5 * time.Minute
	// maxStashedImageCalls bounds the stash; the oldest call is dropped first.
	maxStashedImageCalls = 64
	// maxToolImageBytes and maxToolCallImageBytes cap the base64 an image, and
	// all images of one call, add to the A2A task. The task store resends the
	// whole task on each update under a 16 MiB gRPC limit.
	maxToolImageBytes     = 2 << 20
	maxToolCallImageBytes = 4 << 20
)

type stashedImages struct {
	images []ToolImage
	at     time.Time
}

// ToolCallKey identifies a tool call within the process. Some model providers
// reuse call IDs such as "call_0", so the invocation scopes them.
type ToolCallKey struct {
	InvocationID string
	CallID       string
}

var imageStash = struct {
	sync.Mutex
	calls map[ToolCallKey]stashedImages
}{calls: map[ToolCallKey]stashedImages{}}

// StashToolImages keeps the images an MCP tool call returned until its
// function response is converted to A2A.
func StashToolImages(call ToolCallKey, images []ToolImage) {
	imageStash.Lock()
	defer imageStash.Unlock()
	now := time.Now()
	var oldest ToolCallKey
	for key, stashed := range imageStash.calls {
		if now.Sub(stashed.at) > stashedImagesTTL {
			delete(imageStash.calls, key)
		} else if oldest == (ToolCallKey{}) || stashed.at.Before(imageStash.calls[oldest].at) {
			oldest = key
		}
	}
	if len(imageStash.calls) >= maxStashedImageCalls {
		delete(imageStash.calls, oldest)
	}
	imageStash.calls[call] = stashedImages{images: images, at: now}
}

// TakeToolImages returns the images an MCP tool call returned and forgets
// them. Each function response is converted to A2A once.
func TakeToolImages(call ToolCallKey) []ToolImage {
	imageStash.Lock()
	defer imageStash.Unlock()
	stashed := imageStash.calls[call]
	delete(imageStash.calls, call)
	return stashed.images
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
// types, and images over the size caps, keep their placeholder.
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
			if image, size := capturableImage(content, captured); image != nil {
				captured += size
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

// capturableImage returns content as an image to capture, with its base64
// size, if the UI renders its type and it fits the caps after captured bytes.
func capturableImage(content mcpsdk.Content, captured int) (*mcpsdk.ImageContent, int) {
	image, ok := content.(*mcpsdk.ImageContent)
	if !ok || image == nil || !isRenderableImage(image.MIMEType) {
		return nil, 0
	}
	size := base64.StdEncoding.EncodedLen(len(image.Data))
	if size > maxToolImageBytes || captured+size > maxToolCallImageBytes {
		return nil, 0
	}
	return image, size
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
	sink := &imageSink{}
	result, err := t.functionTool.Run(valuesContext{Context: ctx, values: context.WithValue(ctx, imageSinkKey{}, sink)}, args)
	call := ToolCallKey{InvocationID: ctx.InvocationID(), CallID: ctx.FunctionCallID()}
	if err != nil || len(sink.images) == 0 || call.InvocationID == "" || call.CallID == "" {
		return result, err
	}
	if result == nil {
		result = map[string]any{}
	}
	StashToolImages(call, sink.images)
	result[ToolResultImagesKey] = fmt.Sprintf("%d image(s) shown to the user", len(sink.images))
	return result, nil
}
