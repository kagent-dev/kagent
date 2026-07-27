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
