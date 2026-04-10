package models

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
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

func TestBedrockConfigCreation(t *testing.T) {
	config := &BedrockConfig{
		Model:       "anthropic.claude-3-sonnet-20240229-v1:0",
		Region:      "us-east-1",
		MaxTokens:   aws.Int(1024),
		Temperature: aws.Float64(0.7),
	}

	if config.Model != "anthropic.claude-3-sonnet-20240229-v1:0" {
		t.Errorf("expected model 'anthropic.claude-3-sonnet-20240229-v1:0', got %s", config.Model)
	}

	if config.Region != "us-east-1" {
		t.Errorf("expected region 'us-east-1', got %s", config.Region)
	}

	if config.MaxTokens == nil || *config.MaxTokens != 1024 {
		t.Error("expected MaxTokens to be 1024")
	}

	if config.Temperature == nil || *config.Temperature != 0.7 {
		t.Error("expected Temperature to be 0.7")
	}
}
