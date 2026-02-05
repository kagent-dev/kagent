package core

import (
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func TestEscapeMarkdownBackticks(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{
			name:     "single backtick",
			input:    "foo`bar",
			expected: "foo\\`bar",
		},
		{
			name:     "multiple backticks",
			input:    "`code` and `more`",
			expected: "\\`code\\` and \\`more\\`",
		},
		{
			name:     "plain text",
			input:    "plain text",
			expected: "plain text",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "non-string type",
			input:    123,
			expected: "123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeMarkdownBackticks(tt.input)
			if result != tt.expected {
				t.Errorf("escapeMarkdownBackticks(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsInputRequiredTask(t *testing.T) {
	tests := []struct {
		name     string
		state    protocol.TaskState
		expected bool
	}{
		{
			name:     "input_required state",
			state:    protocol.TaskStateInputRequired,
			expected: true,
		},
		{
			name:     "working state",
			state:    protocol.TaskStateWorking,
			expected: false,
		},
		{
			name:     "completed state",
			state:    protocol.TaskStateCompleted,
			expected: false,
		},
		{
			name:     "failed state",
			state:    protocol.TaskStateFailed,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsInputRequiredTask(tt.state)
			if result != tt.expected {
				t.Errorf("IsInputRequiredTask(%v) = %v, want %v", tt.state, result, tt.expected)
			}
		})
	}
}

func TestExtractDecisionFromMessage_DataPart(t *testing.T) {
	// Test approve decision from DataPart
	approveData := map[string]interface{}{
		KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeApprove,
	}
	message := &protocol.Message{
		MessageID: "test",
		Parts: []protocol.Part{
			&protocol.DataPart{
				Data: approveData,
			},
		},
	}
	result := ExtractDecisionFromMessage(message)
	if result != DecisionApprove {
		t.Errorf("ExtractDecisionFromMessage(approve DataPart) = %q, want %q", result, DecisionApprove)
	}

	// Test deny decision from DataPart
	denyData := map[string]interface{}{
		KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeDeny,
	}
	message = &protocol.Message{
		MessageID: "test",
		Parts: []protocol.Part{
			&protocol.DataPart{
				Data: denyData,
			},
		},
	}
	result = ExtractDecisionFromMessage(message)
	if result != DecisionDeny {
		t.Errorf("ExtractDecisionFromMessage(deny DataPart) = %q, want %q", result, DecisionDeny)
	}
}

func TestExtractDecisionFromMessage_TextPart(t *testing.T) {
	// Test approve keyword
	message := &protocol.Message{
		MessageID: "test",
		Parts: []protocol.Part{
			&protocol.TextPart{Text: "I have approved this action"},
		},
	}
	result := ExtractDecisionFromMessage(message)
	if result != DecisionApprove {
		t.Errorf("ExtractDecisionFromMessage(approve text) = %q, want %q", result, DecisionApprove)
	}

	// Test deny keyword
	message = &protocol.Message{
		MessageID: "test",
		Parts: []protocol.Part{
			&protocol.TextPart{Text: "Request denied, do not proceed"},
		},
	}
	result = ExtractDecisionFromMessage(message)
	if result != DecisionDeny {
		t.Errorf("ExtractDecisionFromMessage(deny text) = %q, want %q", result, DecisionDeny)
	}

	// Test case insensitive
	message = &protocol.Message{
		MessageID: "test",
		Parts: []protocol.Part{
			&protocol.TextPart{Text: "APPROVED"},
		},
	}
	result = ExtractDecisionFromMessage(message)
	if result != DecisionApprove {
		t.Errorf("ExtractDecisionFromMessage(APPROVED) = %q, want %q", result, DecisionApprove)
	}
}

func TestExtractDecisionFromMessage_Priority(t *testing.T) {
	// Test DataPart takes priority over TextPart
	message := &protocol.Message{
		MessageID: "test",
		Parts: []protocol.Part{
			&protocol.TextPart{Text: "approved"}, // Would detect as approve
			&protocol.DataPart{
				Data: map[string]interface{}{
					KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeDeny, // But deny wins
				},
			},
		},
	}
	result := ExtractDecisionFromMessage(message)
	if result != DecisionDeny {
		t.Errorf("ExtractDecisionFromMessage(mixed parts) = %q, want %q (DataPart should take priority)", result, DecisionDeny)
	}
}

func TestExtractDecisionFromMessage_EdgeCases(t *testing.T) {
	// Test nil message
	result := ExtractDecisionFromMessage(nil)
	if result != "" {
		t.Errorf("ExtractDecisionFromMessage(nil) = %q, want empty string", result)
	}

	// Test message with no parts
	message := &protocol.Message{
		MessageID: "test",
		Parts:     []protocol.Part{},
	}
	result = ExtractDecisionFromMessage(message)
	if result != "" {
		t.Errorf("ExtractDecisionFromMessage(empty parts) = %q, want empty string", result)
	}

	// Test message with no decision found
	message = &protocol.Message{
		MessageID: "test",
		Parts: []protocol.Part{
			&protocol.TextPart{Text: "This is just a comment"},
		},
	}
	result = ExtractDecisionFromMessage(message)
	if result != "" {
		t.Errorf("ExtractDecisionFromMessage(no decision) = %q, want empty string", result)
	}
}

func TestFormatToolApprovalTextParts(t *testing.T) {
	requests := []ToolApprovalRequest{
		{Name: "search", Args: map[string]interface{}{"query": "test"}},
		{Name: "run`code`", Args: map[string]interface{}{"cmd": "echo `test`"}},
		{Name: "reset", Args: map[string]interface{}{}},
	}

	parts := formatToolApprovalTextParts(requests)

	// Convert parts to text for checking
	textContent := ""
	for _, p := range parts {
		var textPart *protocol.TextPart
		if tp, ok := p.(*protocol.TextPart); ok {
			textPart = tp
		} else if tp, ok := p.(protocol.TextPart); ok {
			textPart = &tp
		}
		if textPart != nil {
			textContent += textPart.Text
		}
	}

	// Check structure and content
	if !strings.Contains(textContent, "Approval Required") {
		t.Error("formatToolApprovalTextParts should contain 'Approval Required'")
	}
	if !strings.Contains(textContent, "search") {
		t.Error("formatToolApprovalTextParts should contain 'search'")
	}
	if !strings.Contains(textContent, "reset") {
		t.Error("formatToolApprovalTextParts should contain 'reset'")
	}
	// Check backticks are escaped
	if !strings.Contains(textContent, "\\`") {
		t.Error("formatToolApprovalTextParts should escape backticks")
	}
}
