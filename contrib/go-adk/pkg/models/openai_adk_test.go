package models

import (
	"testing"

	"github.com/openai/openai-go/v3"
	"google.golang.org/genai"
)

func TestOpenAIModel_Name(t *testing.T) {
	m := &OpenAIModel{}
	if got := m.Name(); got != "openai" {
		t.Errorf("Name() = %q, want %q", got, "openai")
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
		{"map with content[0].text", map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{"text": "extracted text"},
			},
		}, "extracted text"},
		{"map with result", map[string]interface{}{
			"result": "result value",
		}, "result value"},
		{"map with both prefers content", map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{"text": "from content"},
			},
			"result": "from result",
		}, "from content"},
		{"map empty content slice falls back to JSON", map[string]interface{}{
			"content": []interface{}{},
		}, `{"content":[]}`},
		{"map with result when content empty", map[string]interface{}{
			"content": []interface{}{},
			"result":  "fallback",
		}, "fallback"},
		{"other type falls back to JSON", 42, "42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := functionResponseContentString(tt.resp)
			if got != tt.want {
				t.Errorf("functionResponseContentString() = %q, want %q", got, tt.want)
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
				ParametersJsonSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"city": map[string]interface{}{"type": "string"},
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
