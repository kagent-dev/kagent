package mcp

import (
	"context"
	"encoding/base64"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
)

// ADK's MCP tool renders image content as a text placeholder, so the image
// bytes never reach the tool result. The toolset's MCP client moves them from
// the raw tools/call response into a sink carried by the call's context, and
// imageTool adds them to the result the UI receives. The agent replaces them
// with a short note before the result reaches the model.

// ToolResultImagesKey is the tool result key holding the images an MCP tool
// returned, as a list of ToolImage.
const ToolResultImagesKey = "images"

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

// captureImages moves the images of a successful tools/call result into the
// call's image sink, if it has one. Removing them before ADK formats the result
// keeps their placeholders out of the tool output, joined exactly as if the
// server had returned no images. Errors keep their content for the model.
func captureImages(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
	return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
		result, err := next(ctx, method, req)
		sink, _ := ctx.Value(imageSinkKey{}).(*imageSink)
		res, ok := result.(*mcpsdk.CallToolResult)
		if err != nil || sink == nil || !ok || res == nil || res.IsError {
			return result, err
		}
		kept := make([]mcpsdk.Content, 0, len(res.Content))
		for _, content := range res.Content {
			image, ok := content.(*mcpsdk.ImageContent)
			if !ok || image == nil {
				kept = append(kept, content)
				continue
			}
			sink.images = append(sink.images, ToolImage{
				MIMEType: image.MIMEType,
				Data:     base64.StdEncoding.EncodeToString(image.Data),
			})
		}
		res.Content = kept
		return result, nil
	}
}

// withImages adds the images captured during each tool call of ts to the
// call's result.
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
	if err != nil || len(sink.images) == 0 {
		return result, err
	}
	if result == nil {
		result = map[string]any{}
	}
	result[ToolResultImagesKey] = sink.images
	return result, nil
}
