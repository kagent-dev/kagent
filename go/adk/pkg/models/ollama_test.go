package models

import (
	"reflect"
	"sort"
	"testing"

	"google.golang.org/genai"
)

func TestConvertOllamaOptions(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected map[string]any
	}{
		{
			name:     "nil options returns nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty options returns empty map",
			input:    map[string]string{},
			expected: map[string]any{},
		},
		{
			name: "integer options converted",
			input: map[string]string{
				"num_ctx":     "4096",
				"top_k":       "40",
				"seed":        "123",
				"num_predict": "512",
			},
			expected: map[string]any{
				"num_ctx":     4096,
				"top_k":       40,
				"seed":        123,
				"num_predict": 512,
			},
		},
		{
			name: "float options converted",
			input: map[string]string{
				"temperature":       "0.8",
				"top_p":             "0.95",
				"repeat_penalty":    "1.1",
				"presence_penalty":  "0.5",
				"frequency_penalty": "0.5",
			},
			expected: map[string]any{
				"temperature":       0.8,
				"top_p":             0.95,
				"repeat_penalty":    1.1,
				"presence_penalty":  0.5,
				"frequency_penalty": 0.5,
			},
		},
		{
			name: "boolean options converted",
			input: map[string]string{
				"penalize_newline": "true",
				"low_vram":         "false",
				"f16_kv":           "True",
				"vocab_only":       "FALSE",
			},
			expected: map[string]any{
				"penalize_newline": true,
				"low_vram":         false,
				"f16_kv":           true,
				"vocab_only":       false,
			},
		},
		{
			name: "mixed options",
			input: map[string]string{
				"temperature":      "0.7",
				"num_ctx":          "2048",
				"penalize_newline": "true",
				"stop":             "[\"END\", \"STOP\"]", // unknown option stays string
			},
			expected: map[string]any{
				"temperature":      0.7,
				"num_ctx":          2048,
				"penalize_newline": true,
				"stop":             "[\"END\", \"STOP\"]",
			},
		},
		{
			name: "invalid numbers fall back to string",
			input: map[string]string{
				"temperature": "invalid",      // should stay as string
				"num_ctx":     "not_a_number", // should stay as string
			},
			expected: map[string]any{
				"temperature": "invalid",
				"num_ctx":     "not_a_number",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertOllamaOptions(tt.input)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d keys, got %d", len(tt.expected), len(result))
			}

			for key, expectedVal := range tt.expected {
				resultVal, ok := result[key]
				if !ok {
					t.Errorf("missing expected key %q", key)
					continue
				}

				// Check type and value
				if !reflect.DeepEqual(resultVal, expectedVal) {
					t.Errorf("key %q: expected %v (type %T), got %v (type %T)",
						key, expectedVal, expectedVal, resultVal, resultVal)
				}
			}
		})
	}
}

func TestOllamaConfigDefaults(t *testing.T) {
	// Test that OllamaModel uses correct default values
	config := &OllamaConfig{
		Model: "llama3.2",
		Host:  "",
		Options: map[string]string{
			"temperature": "0.8",
		},
	}

	if config.Model != "llama3.2" {
		t.Errorf("expected model 'llama3.2', got %s", config.Model)
	}

	if config.Host != "" {
		t.Errorf("expected empty host, got %s", config.Host)
	}

	// Verify options are preserved and convertible
	converted := convertOllamaOptions(config.Options)
	if v, ok := converted["temperature"].(float64); !ok || v != 0.8 {
		t.Errorf("expected temperature 0.8, got %v", converted["temperature"])
	}
}

func TestConvertGenaiContentsToOllamaMessages(t *testing.T) {
	tests := []struct {
		name           string
		contents       []*genai.Content
		config         *genai.GenerateContentConfig
		wantMsgCount   int
		wantSystemText string
	}{
		{
			name: "simple user message",
			contents: []*genai.Content{
				{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
			},
			wantMsgCount: 1,
		},
		{
			name: "system instruction from config",
			contents: []*genai.Content{
				{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
			},
			config: &genai.GenerateContentConfig{
				SystemInstruction: &genai.Content{
					Parts: []*genai.Part{
						{Text: "You are a test agent."},
						{Text: "Begin EVERY reply with ZEBRA9."},
					},
				},
			},
			wantMsgCount:   1,
			wantSystemText: "You are a test agent.\nBegin EVERY reply with ZEBRA9.",
		},
		{
			name: "system instruction from config merges with content role system",
			contents: []*genai.Content{
				{Role: "system", Parts: []*genai.Part{{Text: "From contents"}}},
				{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
			},
			config: &genai.GenerateContentConfig{
				SystemInstruction: &genai.Content{
					Parts: []*genai.Part{{Text: "From config"}},
				},
			},
			wantMsgCount:   1,
			wantSystemText: "From contents\nFrom config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs, systemText := convertGenaiContentsToOllamaMessages(tt.contents, tt.config)
			if len(msgs) != tt.wantMsgCount {
				t.Errorf("expected %d messages, got %d", tt.wantMsgCount, len(msgs))
			}
			if systemText != tt.wantSystemText {
				t.Errorf("expected system text %q, got %q", tt.wantSystemText, systemText)
			}
		})
	}
}

// TestConvertGenaiToolsToOllamaPropertyOrderIsStable guards prompt prefix
// caching. api.ToolPropertiesMap preserves insertion order, so if the
// conversion ranges over the Go map of properties directly, every call emits a
// different property order. Servers that cache on the prompt prefix then
// re-process the whole prompt on every request.
func TestConvertGenaiToolsToOllamaPropertyOrderIsStable(t *testing.T) {
	props := map[string]*genai.Schema{
		"project": {Type: genai.TypeString, Description: "project slug"},
		"family":  {Type: genai.TypeString, Description: "change family"},
		"bucket":  {Type: genai.TypeString, Description: "severity bucket"},
		"since":   {Type: genai.TypeString, Description: "lower bound"},
		"limit":   {Type: genai.TypeInteger, Description: "max rows"},
	}
	tools := []*genai.Tool{{
		FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name:       "list_changes",
			Parameters: &genai.Schema{Type: genai.TypeObject, Properties: props},
		}},
	}}

	want := names(t, tools)
	for i := 0; i < 100; i++ {
		if got := names(t, tools); !reflect.DeepEqual(got, want) {
			t.Fatalf("property order changed between calls:\n first: %v\n call %d: %v", want, i, got)
		}
	}

	if !sort.StringsAreSorted(want) {
		t.Errorf("property order is not deterministic across processes: %v", want)
	}
}

// names returns the parameter property names of the first converted tool, in
// the order they would be serialized.
func names(t *testing.T, tools []*genai.Tool) []string {
	t.Helper()
	converted := convertGenaiToolsToOllama(tools)
	if len(converted) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(converted))
	}
	var out []string
	for name := range converted[0].Function.Parameters.Properties.All() {
		out = append(out, name)
	}
	return out
}
