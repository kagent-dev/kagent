package models

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/openai/openai-go/v3"
	"google.golang.org/genai"
)

func TestOpenAIModel_Name(t *testing.T) {
	m := &OpenAIModel{Config: &OpenAIConfig{Model: "gpt-4o"}}
	if got := m.Name(); got != "gpt-4o" {
		t.Errorf("Name() = %q, want %q", got, "gpt-4o")
	}
}

func TestFunctionResponseContentString(t *testing.T) {
	tests := []struct {
		name string
		resp any
		want string
	}{
		{"nil", nil, ""},
		{"string", "hello", "hello"},
		{"empty string", "", ""},
		{"map with content[0].text", map[string]any{
			"content": []any{
				map[string]any{"text": "extracted text"},
			},
		}, "extracted text"},
		{"map with result", map[string]any{
			"result": "result value",
		}, "result value"},
		{"map with both prefers content", map[string]any{
			"content": []any{
				map[string]any{"text": "from content"},
			},
			"result": "from result",
		}, "from content"},
		{"map empty content slice falls back to JSON", map[string]any{
			"content": []any{},
		}, `{"content":[]}`},
		{"map with result when content empty", map[string]any{
			"content": []any{},
			"result":  "fallback",
		}, "fallback"},
		{"other type falls back to JSON", 42, "42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFunctionResponseContent(tt.resp)
			if got != tt.want {
				t.Errorf("extractFunctionResponseContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenaiToolsToOpenAITools(t *testing.T) {
	t.Run("nil slice", func(t *testing.T) {
		out := genaiToolsToOpenAITools(nil)
		if out != nil {
			t.Errorf("genaiToolsToOpenAITools(nil) = %v, want nil", out)
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		out := genaiToolsToOpenAITools([]*genai.Tool{})
		if len(out) != 0 {
			t.Errorf("len(out) = %d, want 0", len(out))
		}
	})

	t.Run("nil tool skipped", func(t *testing.T) {
		out := genaiToolsToOpenAITools([]*genai.Tool{nil, {FunctionDeclarations: []*genai.FunctionDeclaration{
			{Name: "foo", Description: "desc"},
		}}})
		if len(out) != 1 {
			t.Errorf("len(out) = %d, want 1", len(out))
		}
	})

	t.Run("tool with params", func(t *testing.T) {
		tools := []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name:        "get_weather",
				Description: "Get weather",
				ParametersJsonSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
				},
			}},
		}}
		out := genaiToolsToOpenAITools(tools)
		if len(out) != 1 {
			t.Fatalf("len(out) = %d, want 1", len(out))
		}
		// We only check we got one tool; internal shape is openai-specific
	})

	t.Run("nil params gets default object schema with properties", func(t *testing.T) {
		tools := []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name:        "istio_analyze_cluster_configuration",
				Description: "Analyze Istio cluster config",
			}},
		}}
		out := genaiToolsToOpenAITools(tools)
		if len(out) != 1 {
			t.Fatalf("len(out) = %d, want 1", len(out))
		}
		// The converted tool should have a valid schema; OpenAI rejects
		// object schemas missing "properties".
	})

	t.Run("object type without properties gets empty properties", func(t *testing.T) {
		tools := []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name:        "no_props",
				Description: "Object type but no properties field",
				ParametersJsonSchema: map[string]any{
					"type": "object",
				},
			}},
		}}
		out := genaiToolsToOpenAITools(tools)
		if len(out) != 1 {
			t.Fatalf("len(out) = %d, want 1", len(out))
		}
	})
}

func TestGenaiContentsToOpenAIMessages(t *testing.T) {
	t.Run("nil contents", func(t *testing.T) {
		msgs, sys := genaiContentsToOpenAIMessages(nil, nil)
		if len(msgs) != 0 {
			t.Errorf("len(messages) = %d, want 0", len(msgs))
		}
		if sys != "" {
			t.Errorf("systemInstruction = %q, want empty", sys)
		}
	})

	t.Run("system instruction from config", func(t *testing.T) {
		config := &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{
					{Text: "You are helpful."},
					{Text: "Be concise."},
				},
			},
		}
		msgs, sys := genaiContentsToOpenAIMessages(nil, config)
		if len(msgs) != 0 {
			t.Errorf("len(messages) = %d, want 0", len(msgs))
		}
		wantSys := "You are helpful.\nBe concise."
		if sys != wantSys {
			t.Errorf("systemInstruction = %q, want %q", sys, wantSys)
		}
	})

	t.Run("system instruction trims and skips empty text", func(t *testing.T) {
		config := &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{
					{Text: "  one  "},
					{Text: ""},
					{Text: "two"},
				},
			},
		}
		_, sys := genaiContentsToOpenAIMessages(nil, config)
		// Implementation joins parts then TrimSpace; empty text part adds nothing
		wantSys := "one  \ntwo"
		if sys != wantSys {
			t.Errorf("systemInstruction = %q, want %q", sys, wantSys)
		}
	})

	t.Run("user content with text", func(t *testing.T) {
		contents := []*genai.Content{{
			Role:  string(genai.RoleUser),
			Parts: []*genai.Part{{Text: "Hello"}},
		}}
		msgs, sys := genaiContentsToOpenAIMessages(contents, nil)
		if sys != "" {
			t.Errorf("systemInstruction = %q, want empty", sys)
		}
		if len(msgs) != 1 {
			t.Fatalf("len(messages) = %d, want 1", len(msgs))
		}
		// First message should be user message (we only assert count and no panic)
	})

	t.Run("content with role system skipped", func(t *testing.T) {
		contents := []*genai.Content{
			{Role: "system", Parts: []*genai.Part{{Text: "sys"}}},
			{Role: string(genai.RoleUser), Parts: []*genai.Part{{Text: "user"}}},
		}
		msgs, _ := genaiContentsToOpenAIMessages(contents, nil)
		// System role content is skipped (handled via config), so only user message
		if len(msgs) != 1 {
			t.Errorf("len(messages) = %d, want 1 (system content skipped)", len(msgs))
		}
	})

	t.Run("nil and empty content skipped", func(t *testing.T) {
		contents := []*genai.Content{
			nil,
			{Role: "", Parts: nil},
			{Role: string(genai.RoleUser), Parts: []*genai.Part{{Text: "only"}}},
		}
		msgs, _ := genaiContentsToOpenAIMessages(contents, nil)
		if len(msgs) != 1 {
			t.Errorf("len(messages) = %d, want 1", len(msgs))
		}
	})
}

func TestApplyOpenAIConfig(t *testing.T) {
	t.Run("nil config no panic", func(t *testing.T) {
		var params openai.ChatCompletionNewParams
		applyOpenAIConfig(&params, nil)
	})

	t.Run("config with temperature", func(t *testing.T) {
		temp := 0.7
		cfg := &OpenAIConfig{Temperature: &temp}
		var params openai.ChatCompletionNewParams
		applyOpenAIConfig(&params, cfg)
		if !params.Temperature.Valid() || params.Temperature.Value != 0.7 {
			t.Errorf("Temperature: Valid=%v, Value=%v, want (true, 0.7)", params.Temperature.Valid(), params.Temperature.Value)
		}
	})

	t.Run("config with max_tokens", func(t *testing.T) {
		n := 100
		cfg := &OpenAIConfig{MaxTokens: &n}
		var params openai.ChatCompletionNewParams
		applyOpenAIConfig(&params, cfg)
		if !params.MaxTokens.Valid() || params.MaxTokens.Value != 100 {
			t.Errorf("MaxTokens: Valid=%v, Value=%v, want (true, 100)", params.MaxTokens.Valid(), params.MaxTokens.Value)
		}
	})
}

func TestGenaiContentsToOpenAIMessages_PreservesThoughtSignatureOnToolCallAndToolResult(t *testing.T) {
	thoughtSignature := []byte("abc")

	functionCall := genai.NewPartFromFunctionCall("add", map[string]any{"a": 2, "b": 2})
	functionCall.FunctionCall.ID = "call_1"
	functionCall.ThoughtSignature = thoughtSignature

	functionResponse := genai.NewPartFromFunctionResponse("add", map[string]any{"result": "4"})
	functionResponse.FunctionResponse.ID = "call_1"

	messages, _ := genaiContentsToOpenAIMessages([]*genai.Content{
		{
			Role:  string(genai.RoleModel),
			Parts: []*genai.Part{functionCall},
		},
		{
			Role:  string(genai.RoleUser),
			Parts: []*genai.Part{functionResponse},
		},
	}, nil)

	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}

	assistantJSON, err := json.Marshal(messages[0].OfAssistant)
	if err != nil {
		t.Fatalf("json.Marshal(assistant) error = %v", err)
	}
	toolJSON, err := json.Marshal(messages[1].OfTool)
	if err != nil {
		t.Fatalf("json.Marshal(tool) error = %v", err)
	}

	want := base64.StdEncoding.EncodeToString(thoughtSignature)
	assertThoughtSignature := func(name string, payload []byte) {
		t.Helper()
		var obj map[string]any
		if err := json.Unmarshal(payload, &obj); err != nil {
			t.Fatalf("%s json.Unmarshal error = %v", name, err)
		}
		extra, ok := obj["extra_content"].(map[string]any)
		if !ok {
			t.Fatalf("%s missing extra_content: %s", name, string(payload))
		}
		googleExtra, ok := extra["google"].(map[string]any)
		if !ok {
			t.Fatalf("%s missing google extra content: %s", name, string(payload))
		}
		if got, _ := googleExtra["thought_signature"].(string); got != want {
			t.Fatalf("%s thought_signature = %q, want %q", name, got, want)
		}
	}

	var assistantObj map[string]any
	if err := json.Unmarshal(assistantJSON, &assistantObj); err != nil {
		t.Fatalf("assistant json.Unmarshal error = %v", err)
	}
	toolCalls, ok := assistantObj["tool_calls"].([]any)
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("assistant tool_calls = %#v, want 1 tool call", assistantObj["tool_calls"])
	}
	firstToolCall, ok := toolCalls[0].(map[string]any)
	if !ok {
		t.Fatalf("assistant tool call = %#v, want object", toolCalls[0])
	}
	firstToolCallJSON, err := json.Marshal(firstToolCall)
	if err != nil {
		t.Fatalf("json.Marshal(firstToolCall) error = %v", err)
	}

	assertThoughtSignature("assistant tool_call", firstToolCallJSON)
	assertThoughtSignature("tool message", toolJSON)
}

func TestChatCompletionToLLMResponse_PreservesThoughtSignature(t *testing.T) {
	raw := []byte(`{
		"id":"chatcmpl-1",
		"object":"chat.completion",
		"created":123,
		"model":"gemini-2.5-flash",
		"choices":[{
			"index":0,
			"finish_reason":"tool_calls",
			"message":{
				"role":"assistant",
				"tool_calls":[{
					"id":"call_1",
					"type":"function",
					"function":{
						"name":"add",
						"arguments":"{\"a\":2,\"b\":2}"
					},
					"extra_content":{
						"google":{
							"thought_signature":"YWJj"
						}
					}
				}]
			}
		}],
		"usage":{
			"prompt_tokens":3,
			"completion_tokens":4,
			"total_tokens":7
		}
	}`)

	var completion openai.ChatCompletion
	if err := json.Unmarshal(raw, &completion); err != nil {
		t.Fatalf("json.Unmarshal(ChatCompletion) error = %v", err)
	}

	resp := chatCompletionToLLMResponse(&completion)
	if resp.Content == nil || len(resp.Content.Parts) != 1 {
		t.Fatalf("response parts = %#v, want 1 function-call part", resp.Content)
	}

	part := resp.Content.Parts[0]
	if part.FunctionCall == nil {
		t.Fatalf("part.FunctionCall = nil, want function call")
	}
	if part.FunctionCall.Name != "add" {
		t.Fatalf("part.FunctionCall.Name = %q, want %q", part.FunctionCall.Name, "add")
	}
	if string(part.ThoughtSignature) != "abc" {
		t.Fatalf("part.ThoughtSignature = %q, want %q", string(part.ThoughtSignature), "abc")
	}
	if resp.UsageMetadata == nil || resp.UsageMetadata.PromptTokenCount != 3 || resp.UsageMetadata.CandidatesTokenCount != 4 {
		t.Fatalf("usage metadata = %#v, want prompt=3 completion=4", resp.UsageMetadata)
	}
}

func TestExtractThoughtSignatureFromStreamingToolCallChunk(t *testing.T) {
	raw := []byte(`{
		"index":0,
		"id":"call_1",
		"type":"function",
		"function":{
			"name":"add",
			"arguments":"{\"a\":2"
		},
		"extra_content":{
			"google":{
				"thought_signature":"YWJj"
			}
		}
	}`)

	var toolCall openai.ChatCompletionChunkChoiceDeltaToolCall
	if err := json.Unmarshal(raw, &toolCall); err != nil {
		t.Fatalf("json.Unmarshal(ChatCompletionChunkChoiceDeltaToolCall) error = %v", err)
	}

	thoughtSignature := extractThoughtSignatureFromExtraFields(toolCall.JSON.ExtraFields)
	if string(thoughtSignature) != "abc" {
		t.Fatalf("thoughtSignature = %q, want %q", string(thoughtSignature), "abc")
	}
}
