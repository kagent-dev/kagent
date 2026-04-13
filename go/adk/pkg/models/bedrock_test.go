package models

import (
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"google.golang.org/genai"
)

func TestBedrockStopReasonToGenai(t *testing.T) {
	tests := []struct {
		name     string
		reason   types.StopReason
		expected genai.FinishReason
	}{
		{
			name:     "max tokens",
			reason:   types.StopReasonMaxTokens,
			expected: genai.FinishReasonMaxTokens,
		},
		{
			name:     "end turn",
			reason:   types.StopReasonEndTurn,
			expected: genai.FinishReasonStop,
		},
		{
			name:     "stop sequence",
			reason:   types.StopReasonStopSequence,
			expected: genai.FinishReasonStop,
		},
		{
			name:     "tool use",
			reason:   types.StopReasonToolUse,
			expected: genai.FinishReasonStop,
		},
		{
			name:     "unknown reason",
			reason:   types.StopReason("unknown"),
			expected: genai.FinishReasonStop,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bedrockStopReasonToGenai(tt.reason)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestConvertGenaiContentsToBedrockMessages(t *testing.T) {
	tests := []struct {
		name               string
		contents           []*genai.Content
		expectedMsgCount   int
		expectedSystemText string
	}{
		{
			name: "simple user message",
			contents: []*genai.Content{
				{
					Role: "user",
					Parts: []*genai.Part{
						{Text: "Hello"},
					},
				},
			},
			expectedMsgCount:   1,
			expectedSystemText: "",
		},
		{
			name: "system instruction",
			contents: []*genai.Content{
				{
					Role: "system",
					Parts: []*genai.Part{
						{Text: "You are a helpful assistant"},
					},
				},
				{
					Role: "user",
					Parts: []*genai.Part{
						{Text: "Hello"},
					},
				},
			},
			expectedMsgCount:   1, // System is extracted, only user message remains
			expectedSystemText: "You are a helpful assistant",
		},
		{
			name: "user and assistant conversation",
			contents: []*genai.Content{
				{
					Role: "user",
					Parts: []*genai.Part{
						{Text: "Hello"},
					},
				},
				{
					Role: "model",
					Parts: []*genai.Part{
						{Text: "Hi there"},
					},
				},
			},
			expectedMsgCount:   2,
			expectedSystemText: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages, systemText := convertGenaiContentsToBedrockMessages(tt.contents)

			if len(messages) != tt.expectedMsgCount {
				t.Errorf("expected %d messages, got %d", tt.expectedMsgCount, len(messages))
			}

			if systemText != tt.expectedSystemText {
				t.Errorf("expected system text %q, got %q", tt.expectedSystemText, systemText)
			}
		})
	}
}

func TestConvertGenaiToolsToBedrock(t *testing.T) {
	tools := []*genai.Tool{
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        "get_weather",
					Description: "Get the weather for a location",
					Parameters: &genai.Schema{
						Type: "object",
						Properties: map[string]*genai.Schema{
							"location": {
								Type:        "string",
								Description: "The location to get weather for",
							},
						},
						Required: []string{"location"},
					},
				},
			},
		},
	}

	bedrockTools := convertGenaiToolsToBedrock(tools)

	if len(bedrockTools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(bedrockTools))
	}
}

func TestExtractBedrockFunctionResponseContent(t *testing.T) {
	tests := []struct {
		name     string
		response any
		expected string
	}{
		{
			name:     "nil response",
			response: nil,
			expected: "",
		},
		{
			name:     "string response",
			response: "success",
			expected: "success",
		},
		{
			name:     "map with result",
			response: map[string]any{"result": "success"},
			expected: "success",
		},
		{
			name:     "map with content",
			response: map[string]any{"content": "data"},
			expected: "data",
		},
		{
			name:     "unknown type",
			response: 123,
			expected: "123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBedrockFunctionResponseContent(tt.response)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestConvertGenaiToolsToBedrockSchemaTypes verifies that genai.Type values ("STRING", "INTEGER",
// etc.) are lowercased to the JSON Schema standard ("string", "integer", etc.) that Bedrock
// expects. Without this conversion Bedrock cannot map user input to the declared parameters and
// always returns empty tool-call arguments.
func TestConvertGenaiToolsToBedrockSchemaTypes(t *testing.T) {
	tools := []*genai.Tool{
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        "get_weather",
					Description: "Get the weather",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"location": {
								Type:        genai.TypeString,
								Description: "The location",
							},
							"count": {
								Type:        genai.TypeInteger,
								Description: "Number of days",
							},
							"detailed": {
								Type:        genai.TypeBoolean,
								Description: "Include details",
							},
						},
						Required: []string{"location"},
					},
				},
			},
		},
	}

	bedrockTools := convertGenaiToolsToBedrock(tools)
	if len(bedrockTools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(bedrockTools))
	}

	toolMember, ok := bedrockTools[0].(*types.ToolMemberToolSpec)
	if !ok {
		t.Fatal("expected *types.ToolMemberToolSpec")
	}

	schemaMember, ok := toolMember.Value.InputSchema.(*types.ToolInputSchemaMemberJson)
	if !ok {
		t.Fatal("expected *types.ToolInputSchemaMemberJson")
	}

	schemaBytes, err := schemaMember.Value.MarshalSmithyDocument()
	if err != nil {
		t.Fatalf("failed to marshal schema: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatalf("failed to unmarshal schema JSON: %v", err)
	}

	// Top-level type must be lowercase "object" (hardcoded)
	if schema["type"] != "object" {
		t.Errorf("expected top-level type %q, got %q", "object", schema["type"])
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %T", schema["properties"])
	}

	wantTypes := map[string]string{
		"location": "string",
		"count":    "integer",
		"detailed": "boolean",
	}
	for propName, wantType := range wantTypes {
		prop, ok := props[propName].(map[string]any)
		if !ok {
			t.Errorf("property %q missing or wrong type in schema", propName)
			continue
		}
		got, _ := prop["type"].(string)
		if got != wantType {
			t.Errorf("property %q: expected JSON Schema type %q, got %q (genai uses uppercase which breaks Bedrock)", propName, wantType, got)
		}
	}

	// Required array must be present and correct
	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatal("expected required array in schema")
	}
	if len(required) != 1 || required[0] != "location" {
		t.Errorf("expected required=[location], got %v", required)
	}
}

// TestStreamingToolCallParseArgs verifies that JSON fragments accumulated during streaming are
// correctly parsed into tool-call arguments.
func TestStreamingToolCallParseArgs(t *testing.T) {
	tests := []struct {
		name      string
		inputJSON string
		wantEmpty bool
		wantKeys  map[string]any
		wantRaw   string
	}{
		{
			name:      "empty input returns empty map",
			inputJSON: "",
			wantEmpty: true,
		},
		{
			name:      "valid JSON with string args",
			inputJSON: `{"location":"San Francisco","unit":"fahrenheit"}`,
			wantKeys: map[string]any{
				"location": "San Francisco",
				"unit":     "fahrenheit",
			},
		},
		{
			name:      "invalid JSON wraps raw in _raw key",
			inputJSON: `not-valid-json`,
			wantKeys:  map[string]any{"_raw": "not-valid-json"},
		},
		{
			name:      "chunked JSON assembled correctly",
			inputJSON: `{"query":` + `"hello world"}`,
			wantKeys:  map[string]any{"query": "hello world"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := &streamingToolCall{InputJSON: tt.inputJSON}
			result := tc.parseArgs()
			if tt.wantEmpty {
				if len(result) != 0 {
					t.Errorf("expected empty map, got %v", result)
				}
				return
			}
			for k, want := range tt.wantKeys {
				got, ok := result[k]
				if !ok {
					t.Errorf("key %q missing from result", k)
					continue
				}
				if got != want {
					t.Errorf("key %q: expected %v, got %v", k, want, got)
				}
			}
		})
	}
}
