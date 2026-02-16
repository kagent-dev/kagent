package a2a

import (
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
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
		state    a2atype.TaskState
		expected bool
	}{
		{
			name:     "input_required state",
			state:    a2atype.TaskStateInputRequired,
			expected: true,
		},
		{
			name:     "working state",
			state:    a2atype.TaskStateWorking,
			expected: false,
		},
		{
			name:     "completed state",
			state:    a2atype.TaskStateCompleted,
			expected: false,
		},
		{
			name:     "failed state",
			state:    a2atype.TaskStateFailed,
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
	message := &a2atype.Message{
		ID: "test",
		Parts: a2atype.ContentParts{
			&a2atype.DataPart{
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
	message = &a2atype.Message{
		ID: "test",
		Parts: a2atype.ContentParts{
			&a2atype.DataPart{
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
	message := &a2atype.Message{
		ID: "test",
		Parts: a2atype.ContentParts{
			a2atype.TextPart{Text: "I have approved this action"},
		},
	}
	result := ExtractDecisionFromMessage(message)
	if result != DecisionApprove {
		t.Errorf("ExtractDecisionFromMessage(approve text) = %q, want %q", result, DecisionApprove)
	}

	// Test deny keyword
	message = &a2atype.Message{
		ID: "test",
		Parts: a2atype.ContentParts{
			a2atype.TextPart{Text: "Request denied, do not proceed"},
		},
	}
	result = ExtractDecisionFromMessage(message)
	if result != DecisionDeny {
		t.Errorf("ExtractDecisionFromMessage(deny text) = %q, want %q", result, DecisionDeny)
	}

	// Test case insensitive
	message = &a2atype.Message{
		ID: "test",
		Parts: a2atype.ContentParts{
			a2atype.TextPart{Text: "APPROVED"},
		},
	}
	result = ExtractDecisionFromMessage(message)
	if result != DecisionApprove {
		t.Errorf("ExtractDecisionFromMessage(APPROVED) = %q, want %q", result, DecisionApprove)
	}
}

func TestExtractDecisionFromMessage_Priority(t *testing.T) {
	// Test DataPart takes priority over TextPart
	message := &a2atype.Message{
		ID: "test",
		Parts: a2atype.ContentParts{
			a2atype.TextPart{Text: "approved"}, // Would detect as approve
			&a2atype.DataPart{
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
	message := &a2atype.Message{
		ID:    "test",
		Parts: a2atype.ContentParts{},
	}
	result = ExtractDecisionFromMessage(message)
	if result != "" {
		t.Errorf("ExtractDecisionFromMessage(empty parts) = %q, want empty string", result)
	}

	// Test message with no decision found
	message = &a2atype.Message{
		ID: "test",
		Parts: a2atype.ContentParts{
			a2atype.TextPart{Text: "This is just a comment"},
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
		var textPart *a2atype.TextPart
		if tp, ok := p.(*a2atype.TextPart); ok {
			textPart = tp
		} else if tp, ok := p.(a2atype.TextPart); ok {
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
