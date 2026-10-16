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
	// maxToolImageBytes, maxToolCallImageBytes and maxInvocationImageBytes cap
	// the base64 an image, all images of one call, and all images of one
	// invocation add to the A2A task. The task store resends the whole task on
	// each update under a 16 MiB gRPC limit. Each image also counts
	// toolImageOverheadBytes toward the call and invocation caps.
	maxToolImageBytes       = 2 << 20
	maxToolCallImageBytes   = 4 << 20
	maxInvocationImageBytes = 6 << 20
	toolImageOverheadBytes  = 64
	// maxToolCallImages caps the number of images captured from one call.
	maxToolCallImages = 16
)

type stashedImages struct {
	images []ToolImage
	at     time.Time
}

// invocationImages counts the image bytes an invocation has captured.
type invocationImages struct {
	bytes int
	at    time.Time
}

// ToolCallKey identifies a tool call within the process. Some model providers
// reuse call IDs such as "call_0", so the invocation scopes them.
type ToolCallKey struct {
	InvocationID string
	CallID       string
}

var imageStash = struct {
	sync.Mutex
	calls       map[ToolCallKey]stashedImages
	invocations map[string]invocationImages
}{calls: map[ToolCallKey]stashedImages{}, invocations: map[string]invocationImages{}}

// StashToolImages keeps the images an MCP tool call returned until its
// function response is converted to A2A. Images of a call already stashed are
// added to.
func StashToolImages(call ToolCallKey, images []ToolImage) {
	imageStash.Lock()
	defer imageStash.Unlock()
	now := time.Now()
	evictExpiredImages(now)
	if stashed, ok := imageStash.calls[call]; ok {
		images = append(stashed.images, images...)
	} else if len(imageStash.calls) >= maxStashedImageCalls {
		var oldest ToolCallKey
		for key, stashed := range imageStash.calls {
			if oldest == (ToolCallKey{}) || stashed.at.Before(imageStash.calls[oldest].at) {
				oldest = key
			}
		}
		delete(imageStash.calls, oldest)
	}
	imageStash.calls[call] = stashedImages{images: images, at: now}
}

// reserveInvocationImageBytes counts n image bytes against invocationID's
// budget, or reports false and counts nothing if they do not fit.
func reserveInvocationImageBytes(invocationID string, n int) bool {
	imageStash.Lock()
	defer imageStash.Unlock()
	now := time.Now()
	evictExpiredImages(now)
	spent := imageStash.invocations[invocationID].bytes
	if spent+n > maxInvocationImageBytes {
		return false
	}
	imageStash.invocations[invocationID] = invocationImages{bytes: spent + n, at: now}
	return true
}

// evictExpiredImages drops stashed calls and invocation budgets unused for
// stashedImagesTTL. The caller holds imageStash.
func evictExpiredImages(now time.Time) {
	for key, stashed := range imageStash.calls {
		if now.Sub(stashed.at) > stashedImagesTTL {
			delete(imageStash.calls, key)
		}
	}
	for id, spent := range imageStash.invocations {
		if now.Sub(spent.at) > stashedImagesTTL {
			delete(imageStash.invocations, id)
		}
	}
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

// imageSink collects the images of one tool call of an invocation.
type imageSink struct {
	invocationID string
	images       []ToolImage
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
// placeholder. The invocation cap is checked here, before an image is removed,
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
			if image, cost := capturableImage(content, captured, len(sink.images)); image != nil && reserveInvocationImageBytes(sink.invocationID, cost) {
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
	if call.InvocationID == "" || call.CallID == "" {
		// The images could not be stashed, so leave ADK's placeholders.
		return t.functionTool.Run(ctx, args)
	}
	sink := &imageSink{invocationID: call.InvocationID}
	result, err := t.functionTool.Run(valuesContext{Context: ctx, values: context.WithValue(ctx, imageSinkKey{}, sink)}, args)
	if err != nil || len(sink.images) == 0 {
		return result, err
	}
	if result == nil {
		result = map[string]any{}
	}
	StashToolImages(call, sink.images)
	result[ToolResultImagesKey] = fmt.Sprintf("%d image(s) returned for display; you cannot see their contents", len(sink.images))
	return result, nil
}
