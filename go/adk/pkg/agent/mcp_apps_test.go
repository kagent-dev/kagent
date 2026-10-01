package agent

import (
	"testing"

	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestMakeMCPAppModelResultCallbackReplacesRenderPayloadWithNotice(t *testing.T) {
	t.Parallel()

	req := &adkmodel.LLMRequest{
		Contents: []*genai.Content{{
			Parts: []*genai.Part{{
				FunctionResponse: &genai.FunctionResponse{
					Name: "jenkins_monitor_build",
					Response: map[string]any{
						"content": []map[string]any{{
							"type": "text",
							"text": "Opened Jenkins Build Monitor for https://example.com/job/demo/1/ (current status: IN_PROGRESS).",
						}},
						"structuredContent": map[string]any{
							"build": map[string]any{
								"stages": []any{map[string]any{"name": "Deploy", "status": "IN_PROGRESS"}},
							},
							"polling_data": "large payload",
						},
						"_meta": map[string]any{
							"ui": map[string]any{
								"resourceUri": "ui://jenkins-mcp/build-monitor",
							},
						},
					},
				},
			}},
		}},
	}

	callback := MakeMCPAppModelResultCallback(map[string]bool{"jenkins_monitor_build": true})
	if _, err := callback(nil, req); err != nil {
		t.Fatalf("callback returned error: %v", err)
	}

	got := req.Contents[0].Parts[0].FunctionResponse.Response

	// Success render payload should be collapsed into the terminal notice so the
	// model stops re-invoking the rendering tool.
	content, ok := got["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content not replaced with notice: %#v", got["content"])
	}
	part, ok := content[0].(map[string]any)
	if !ok || part["text"] != mcpAppRenderedNotice {
		t.Fatalf("notice text missing: %#v", content[0])
	}

	// Should strip structuredContent (heavy render payload).
	if _, ok := got["structuredContent"]; ok {
		t.Fatalf("structuredContent should be stripped, got: %#v", got)
	}

	// Should preserve _meta
	meta, ok := got["_meta"].(map[string]any)
	if !ok {
		t.Fatalf("_meta not preserved: %#v", got["_meta"])
	}
	if _, ok := meta["ui"]; !ok {
		t.Fatalf("_meta.ui not preserved: %#v", meta)
	}
}

func TestMakeMCPAppModelResultCallbackPreservesIsError(t *testing.T) {
	t.Parallel()

	req := &adkmodel.LLMRequest{
		Contents: []*genai.Content{{
			Parts: []*genai.Part{{
				FunctionResponse: &genai.FunctionResponse{
					Name: "jenkins_monitor_build",
					Response: map[string]any{
						"content": []map[string]any{{
							"type": "text",
							"text": "Tool execution failed.",
						}},
						"structuredContent": map[string]any{"error": "connection timeout"},
						"isError":           true,
					},
				},
			}},
		}},
	}

	callback := MakeMCPAppModelResultCallback(map[string]bool{"jenkins_monitor_build": true})
	if _, err := callback(nil, req); err != nil {
		t.Fatalf("callback returned error: %v", err)
	}

	got := req.Contents[0].Parts[0].FunctionResponse.Response

	// Should preserve isError
	isErr, ok := got["isError"].(bool)
	if !ok || !isErr {
		t.Fatalf("isError not preserved or false: %#v", got["isError"])
	}

	// Should still strip structuredContent
	if _, ok := got["structuredContent"]; ok {
		t.Fatalf("structuredContent should be stripped")
	}
}

func TestMakeMCPAppModelResultCallbackPassesThroughPlainResultFromAppTool(t *testing.T) {
	t.Parallel()

	// An App-capable tool ("jenkins_monitor_build" is in appToolNames) can still
	// return an ordinary text-only result: no structuredContent and no _meta.
	// There is nothing to compact, so the text must reach the model unchanged.
	const text = "No build found for job demo/1: it was deleted."
	req := &adkmodel.LLMRequest{
		Contents: []*genai.Content{{
			Parts: []*genai.Part{{
				FunctionResponse: &genai.FunctionResponse{
					Name: "jenkins_monitor_build",
					Response: map[string]any{
						"content": []map[string]any{{
							"type": "text",
							"text": text,
						}},
					},
				},
			}},
		}},
	}

	callback := MakeMCPAppModelResultCallback(map[string]bool{"jenkins_monitor_build": true})
	if _, err := callback(nil, req); err != nil {
		t.Fatalf("callback returned error: %v", err)
	}

	got := req.Contents[0].Parts[0].FunctionResponse.Response
	content, ok := got["content"].([]map[string]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content unexpectedly rewritten: %#v", got["content"])
	}
	if content[0]["text"] != text {
		t.Fatalf("plain result text not preserved: %#v", content[0])
	}
}

func TestMakeMCPAppModelResultCallbackPassesThroughDataOnlyResultFromAppTool(t *testing.T) {
	t.Parallel()

	// A UI-capable tool's result carrying data (structuredContent) but no UI
	// resource of its own must pass through untouched, matching the Python fix
	// in kagent-adk (#2579): the gate is the result's UI resource, not the
	// presence of a payload.
	req := &adkmodel.LLMRequest{
		Contents: []*genai.Content{{
			Parts: []*genai.Part{{
				FunctionResponse: &genai.FunctionResponse{
					Name: "jira_search",
					Response: map[string]any{
						"content": []map[string]any{{
							"type": "text",
							"text": `{"issues": [{"key": "GF-3687"}]}`,
						}},
						"structuredContent": map[string]any{
							"issues": []any{map[string]any{"key": "GF-3687"}},
						},
						"_meta": map[string]any{},
					},
				},
			}},
		}},
	}

	callback := MakeMCPAppModelResultCallback(map[string]bool{"jira_search": true})
	if _, err := callback(nil, req); err != nil {
		t.Fatalf("callback returned error: %v", err)
	}

	got := req.Contents[0].Parts[0].FunctionResponse.Response
	if _, ok := got["structuredContent"]; !ok {
		t.Fatalf("structuredContent should be preserved on a non-UI result: %#v", got)
	}
	content, ok := got["content"].([]map[string]any)
	if !ok || len(content) != 1 || content[0]["text"] == mcpAppRenderedNotice {
		t.Fatalf("data-only result was collapsed into the render notice: %#v", got["content"])
	}
}

func TestMakeMCPAppModelResultCallbackCompactsFlatUIResourceMetaShape(t *testing.T) {
	t.Parallel()

	// The flat `_meta["ui/resourceUri"]` shape parseMCPUIMetadata also accepts
	// must trigger compaction, same as the nested `_meta.ui.resourceUri` form.
	req := &adkmodel.LLMRequest{
		Contents: []*genai.Content{{
			Parts: []*genai.Part{{
				FunctionResponse: &genai.FunctionResponse{
					Name: "show_dashboard",
					Response: map[string]any{
						"content": []map[string]any{{
							"type": "text",
							"text": "rendered",
						}},
						"structuredContent": map[string]any{"x": 1},
						"_meta": map[string]any{
							"ui/resourceUri": "ui://server/dashboard",
						},
					},
				},
			}},
		}},
	}

	callback := MakeMCPAppModelResultCallback(map[string]bool{"show_dashboard": true})
	if _, err := callback(nil, req); err != nil {
		t.Fatalf("callback returned error: %v", err)
	}

	got := req.Contents[0].Parts[0].FunctionResponse.Response
	content, ok := got["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content not replaced with notice: %#v", got["content"])
	}
	part, ok := content[0].(map[string]any)
	if !ok || part["text"] != mcpAppRenderedNotice {
		t.Fatalf("notice text missing: %#v", content[0])
	}
	if _, ok := got["structuredContent"]; ok {
		t.Fatalf("structuredContent should be stripped, got: %#v", got)
	}
}

func TestMakeMCPAppModelResultCallbackLeavesNonAppToolsAlone(t *testing.T) {
	t.Parallel()

	original := map[string]any{
		"output": map[string]any{"answer": 42},
		"content": []map[string]any{{
			"type": "text",
			"text": "Answer is 42",
		}},
	}
	req := &adkmodel.LLMRequest{
		Contents: []*genai.Content{{
			Parts: []*genai.Part{{
				FunctionResponse: &genai.FunctionResponse{
					Name:     "regular_tool",
					Response: original,
				},
			}},
		}},
	}

	callback := MakeMCPAppModelResultCallback(map[string]bool{"some_app_tool": true})
	if _, err := callback(nil, req); err != nil {
		t.Fatalf("callback returned error: %v", err)
	}

	got := req.Contents[0].Parts[0].FunctionResponse.Response

	// Non-app tools should pass through unchanged
	if _, ok := got["output"]; !ok {
		t.Fatalf("non-app tool response modified: %#v", got)
	}
}
