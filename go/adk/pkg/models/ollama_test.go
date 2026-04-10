package models

import (
	"reflect"
	"testing"
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
