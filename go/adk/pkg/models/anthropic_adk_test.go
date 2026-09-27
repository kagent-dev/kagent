package models

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestAnthropicModelGenerateContentSendsStructuredOutputWithTools(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encoded, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(encoded, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"msg-1","type":"message","role":"assistant","model":"claude-sonnet-4-5",
			"content":[{"type":"text","text":"{\"answer\":4}"}],
			"stop_reason":"end_turn","stop_sequence":null,
			"usage":{"input_tokens":1,"output_tokens":1}
		}`)
	}))
	defer server.Close()

	llm, err := newAnthropicModelFromConfig(context.Background(), &AnthropicConfig{
		Model:   "claude-sonnet-4-5",
		BaseUrl: server.URL,
	}, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"answer": map[string]any{"type": "integer"}},
		"required":             []any{"answer"},
		"additionalProperties": false,
	}
	request := &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "calculate"}}}},
		Config: &genai.GenerateContentConfig{
			ResponseJsonSchema: schema,
			Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name: "calculator", ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			}}}},
		},
	}
	for _, generateErr := range llm.GenerateContent(context.Background(), request, false) {
		if generateErr != nil {
			t.Fatalf("GenerateContent error: %v", generateErr)
		}
	}

	outputConfig, ok := body["output_config"].(map[string]any)
	if !ok {
		t.Fatalf("output_config = %#v", body["output_config"])
	}
	format, ok := outputConfig["format"].(map[string]any)
	if !ok || format["type"] != "json_schema" {
		t.Fatalf("output_config.format = %#v", outputConfig["format"])
	}
	if gotSchema, ok := format["schema"].(map[string]any); !ok || gotSchema["additionalProperties"] != false {
		t.Fatalf("output_config.format.schema = %#v", format["schema"])
	}
	if tools, ok := body["tools"].([]any); !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want one tool", body["tools"])
	}
}
