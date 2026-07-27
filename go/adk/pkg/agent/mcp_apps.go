package agent

import (
	"encoding/json"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	adkmodel "google.golang.org/adk/v2/model"
)

// mcpAppRenderedNotice is the terminal message the model sees in place of an
// MCP App tool's render payload. An MCP App tool (one that declares a UI
// resourceUri) produces an interactive view that is displayed to the user and
// refreshes itself in-place via its own app-only tool calls. The model must
// treat a successful render as a completed, self-updating artifact; otherwise it
// tends to re-invoke the rendering tool on every "refresh", flooding the chat
// with duplicate app cards. This notice is protocol-oriented: it applies to any
// tool carrying a UI resourceUri, independent of the tool's name or payload keys.
const mcpAppRenderedNotice = "The interactive UI for this tool has been rendered to the user and now updates live inside the app. Treat this as complete and do not call this tool again unless the user explicitly asks for it."

// MakeMCPAppModelResultCallback replaces what the model sees for MCP App
// (UI-rendering) tool results: instead of the heavy render payload it receives a
// terminal directive (see mcpAppRenderedNotice). The full result is still
// streamed to the UI separately, so this only changes the model's view and
// prevents the model from looping on the rendering tool. Errors are passed
// through so the model can still react to and recover from failures.
func MakeMCPAppModelResultCallback(appToolNames map[string]bool) llmagent.BeforeModelCallback {
	return func(_ agent.Context, req *adkmodel.LLMRequest) (*adkmodel.LLMResponse, error) {
		for _, content := range req.Contents {
			if content == nil {
				continue
			}
			for _, part := range content.Parts {
				if part == nil || part.FunctionResponse == nil || !appToolNames[part.FunctionResponse.Name] {
					continue
				}
				part.FunctionResponse.Response = compactMCPAppModelResponse(part.FunctionResponse.Response)
			}
		}
		return nil, nil
	}
}

// compactMCPAppModelResponse rewrites an MCP App tool result for the model.
//
// The model exchanges tool results as a generic map (genai
// FunctionResponse.Response), but the payload is really an MCP
// [mcpsdk.CallToolResult]. We decode it into that typed result so the logic
// works against real fields (IsError, Content, Meta, StructuredContent) rather
// than poking at string keys. If the payload isn't a recognizable MCP result we
// leave it untouched.
func compactMCPAppModelResponse(response map[string]any) map[string]any {
	result, err := decodeCallToolResult(response)
	if err != nil {
		return response
	}

	if result.IsError {
		// On error, keep the original content/meta so the model can
		// diagnose and recover; only drop the heavy structured payload.
		result.StructuredContent = nil
		return encodeCallToolResult(result, response)
	}

	// On success, collapse the render payload into a terminal directive so the
	// model stops re-invoking the rendering tool. Preserve _meta (e.g.
	// resourceUri) in case downstream tooling relies on it.
	compact := &mcpsdk.CallToolResult{
		Meta:    result.Meta,
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: mcpAppRenderedNotice}},
	}
	return encodeCallToolResult(compact, response)
}

// decodeCallToolResult interprets a generic model-facing response map as a typed
// MCP CallToolResult.
func decodeCallToolResult(response map[string]any) (*mcpsdk.CallToolResult, error) {
	raw, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	var result mcpsdk.CallToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// encodeCallToolResult converts a typed CallToolResult back into the generic map
// the model expects, falling back to the original response if conversion fails.
func encodeCallToolResult(result *mcpsdk.CallToolResult, fallback map[string]any) map[string]any {
	raw, err := json.Marshal(result)
	if err != nil {
		return fallback
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return fallback
	}
	return out
}
