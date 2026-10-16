package agent

import (
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	adkmodel "google.golang.org/adk/v2/model"

	"github.com/kagent-dev/kagent/go/adk/pkg/mcp"
)

// MakeMCPImageModelResultCallback replaces the images in MCP tool results with
// a short note before they reach the model. The images stay in the session
// history and the UI stream: ADK builds req.Contents from deep copies of the
// session events, so editing it changes only what the model sees.
func MakeMCPImageModelResultCallback() llmagent.BeforeModelCallback {
	return func(_ agent.Context, req *adkmodel.LLMRequest) (*adkmodel.LLMResponse, error) {
		for _, content := range req.Contents {
			if content == nil {
				continue
			}
			for _, part := range content.Parts {
				if part == nil || part.FunctionResponse == nil {
					continue
				}
				count, ok := mcpImageCount(part.FunctionResponse.Response[mcp.ToolResultImagesKey])
				if !ok {
					continue
				}
				part.FunctionResponse.Response[mcp.ToolResultImagesKey] = fmt.Sprintf("%d image(s) were displayed to the user", count)
			}
		}
		return nil, nil
	}
}

// mcpImageCount reports how many images value holds if it is the images list
// of an MCP tool result. Images are typed in the call's own result and generic
// once the session history has been decoded from JSON. Any other value, such
// as an "images" key of a local tool or an empty list, is not MCP images.
func mcpImageCount(value any) (int, bool) {
	switch images := value.(type) {
	case []mcp.ToolImage:
		return len(images), len(images) > 0
	case []any:
		if len(images) == 0 {
			return 0, false
		}
		for _, item := range images {
			image, ok := item.(map[string]any)
			if !ok {
				return 0, false
			}
			if _, ok := image["mimeType"].(string); !ok {
				return 0, false
			}
			if _, ok := image["data"].(string); !ok {
				return 0, false
			}
		}
		return len(images), true
	}
	return 0, false
}
