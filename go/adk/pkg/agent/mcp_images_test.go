package agent

import (
	"reflect"
	"testing"

	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/kagent-dev/kagent/go/adk/pkg/mcp"
)

func TestMakeMCPImageModelResultCallbackReplacesImagesWithNote(t *testing.T) {
	t.Parallel()

	images := []mcp.ToolImage{{MIMEType: "image/png", Data: "cG5n"}, {MIMEType: "image/png", Data: "cG5n"}}
	original := map[string]any{"output": "chart", mcp.ToolResultImagesKey: images}
	req := &adkmodel.LLMRequest{
		Contents: []*genai.Content{{
			Parts: []*genai.Part{
				{FunctionResponse: &genai.FunctionResponse{Name: "render_chart", Response: original}},
				{FunctionResponse: &genai.FunctionResponse{Name: "from_history", Response: map[string]any{mcp.ToolResultImagesKey: []any{map[string]any{"mimeType": "image/png", "data": "cG5n"}}}}},
				{FunctionResponse: &genai.FunctionResponse{Name: "local_tool", Response: map[string]any{mcp.ToolResultImagesKey: []any{"cat.png"}}}},
				{FunctionResponse: &genai.FunctionResponse{Name: "sub_agent", Response: map[string]any{mcp.ToolResultImagesKey: "none"}}},
				{FunctionResponse: &genai.FunctionResponse{Name: "plain", Response: map[string]any{"output": "text"}}},
			},
		}},
	}

	if _, err := MakeMCPImageModelResultCallback()(nil, req); err != nil {
		t.Fatalf("callback returned error: %v", err)
	}

	parts := req.Contents[0].Parts
	if got := parts[0].FunctionResponse.Response; got[mcp.ToolResultImagesKey] != "2 image(s) were displayed to the user" || got["output"] != "chart" {
		t.Fatalf("images not replaced with note: %#v", got)
	}
	if _, ok := original[mcp.ToolResultImagesKey].([]mcp.ToolImage); !ok {
		t.Fatalf("original response must keep its images: %#v", original)
	}
	if got := parts[1].FunctionResponse.Response[mcp.ToolResultImagesKey]; got != "1 image(s) were displayed to the user" {
		t.Fatalf("decoded history images not replaced: %#v", got)
	}
	if got := parts[2].FunctionResponse.Response[mcp.ToolResultImagesKey]; !reflect.DeepEqual(got, []any{"cat.png"}) {
		t.Fatalf("non-MCP images list changed: %#v", got)
	}
	if got := parts[3].FunctionResponse.Response[mcp.ToolResultImagesKey]; got != "none" {
		t.Fatalf("non-list images value changed: %#v", got)
	}
	if got := parts[4].FunctionResponse.Response; len(got) != 1 || got["output"] != "text" {
		t.Fatalf("response without images changed: %#v", got)
	}
}
