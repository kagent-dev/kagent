package agent

import (
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
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

func TestMakeMCPAppModelResultCallbackLeavesDataOnlyResultAlone(t *testing.T) {
	t.Parallel()

	original := map[string]any{
		"content": []map[string]any{{
			"type": "text",
			"text": `{"issues": [{"key": "GF-3687"}]}`,
		}},
		"structuredContent": map[string]any{
			"issues": []any{map[string]any{"key": "GF-3687"}},
		},
		"_meta": map[string]any{},
	}
	req := &adkmodel.LLMRequest{
		Contents: []*genai.Content{{
			Parts: []*genai.Part{{
				FunctionResponse: &genai.FunctionResponse{
					Name:     "jira_search",
					Response: original,
				},
			}},
		}},
	}

	callback := MakeMCPAppModelResultCallback(map[string]bool{"jira_search": true})
	_, err := callback(nil, req)
	require.NoError(t, err)

	got := req.Contents[0].Parts[0].FunctionResponse.Response
	require.Equal(t, original, got)
}

func TestMakeMCPAppModelResultCallbackLeavesNoMetaResultAlone(t *testing.T) {
	t.Parallel()

	original := map[string]any{
		"content": []map[string]any{{
			"type": "text",
			"text": "plain output",
		}},
		"structuredContent": map[string]any{"val": 123},
	}
	req := &adkmodel.LLMRequest{
		Contents: []*genai.Content{{
			Parts: []*genai.Part{{
				FunctionResponse: &genai.FunctionResponse{
					Name:     "app_tool",
					Response: original,
				},
			}},
		}},
	}

	callback := MakeMCPAppModelResultCallback(map[string]bool{"app_tool": true})
	_, err := callback(nil, req)
	require.NoError(t, err)

	got := req.Contents[0].Parts[0].FunctionResponse.Response
	require.Equal(t, original, got)
}

func TestMakeMCPAppModelResultCallbackAcceptsFlatResourceURI(t *testing.T) {
	t.Parallel()

	req := &adkmodel.LLMRequest{
		Contents: []*genai.Content{{
			Parts: []*genai.Part{{
				FunctionResponse: &genai.FunctionResponse{
					Name: "weather_tool",
					Response: map[string]any{
						"content": []map[string]any{{
							"type": "text",
							"text": "Weather: Rain, 36C",
						}},
						"structuredContent": map[string]any{"temperature": 36},
						"_meta": map[string]any{
							"ui/resourceUri": "ui://server-everything/weather-dashboard",
						},
					},
				},
			}},
		}},
	}

	callback := MakeMCPAppModelResultCallback(map[string]bool{"weather_tool": true})
	_, err := callback(nil, req)
	require.NoError(t, err)

	got := req.Contents[0].Parts[0].FunctionResponse.Response
	content, ok := got["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 1)

	part, ok := content[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, mcpAppRenderedNotice, part["text"])

	require.NotContains(t, got, "structuredContent")

	meta, ok := got["_meta"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "ui://server-everything/weather-dashboard", meta["ui/resourceUri"])
}

func TestResultHasUIResource(t *testing.T) {
	t.Parallel()

	// Empty meta
	require.False(t, resultHasUIResource(nil))
	require.False(t, resultHasUIResource(mcpsdk.Meta{}))

	// Nested ui.resourceUri
	require.True(t, resultHasUIResource(mcpsdk.Meta{
		"ui": map[string]any{"resourceUri": "ui://server/dashboard"},
	}))

	// Flat ui/resourceUri
	require.True(t, resultHasUIResource(mcpsdk.Meta{
		"ui/resourceUri": "ui://server/dashboard",
	}))

	// Empty nested uri
	require.False(t, resultHasUIResource(mcpsdk.Meta{
		"ui": map[string]any{"resourceUri": ""},
	}))

	// Nested ui with non-map type
	require.False(t, resultHasUIResource(mcpsdk.Meta{
		"ui": "not-a-map",
	}))

	// Other unrelated meta
	require.False(t, resultHasUIResource(mcpsdk.Meta{
		"unrelated": "data",
	}))
}
