package a2a

import (
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
)

func TestEscapeMarkdownBackticks(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "single backtick", input: "foo`bar", expected: "foo\\`bar"},
		{name: "multiple backticks", input: "`code` and `more`", expected: "\\`code\\` and \\`more\\`"},
		{name: "plain text", input: "plain text", expected: "plain text"},
		{name: "empty string", input: "", expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeMarkdownBackticks(tt.input)
			if result != tt.expected {
				t.Errorf("escapeMarkdownBackticks(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExtractDecisionFromMessage_DataPart(t *testing.T) {
	approveData := map[string]any{
		KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeApprove,
	}
	message := a2atype.NewMessage(a2atype.MessageRoleUser,
		&a2atype.DataPart{Data: approveData},
	)
	result := ExtractDecisionFromMessage(message)
	if result != DecisionApprove {
		t.Errorf("ExtractDecisionFromMessage(approve DataPart) = %q, want %q", result, DecisionApprove)
	}

	denyData := map[string]any{
		KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeDeny,
	}
	message = a2atype.NewMessage(a2atype.MessageRoleUser,
		&a2atype.DataPart{Data: denyData},
	)
	result = ExtractDecisionFromMessage(message)
	if result != DecisionDeny {
		t.Errorf("ExtractDecisionFromMessage(deny DataPart) = %q, want %q", result, DecisionDeny)
	}
}

func TestExtractDecisionFromMessage_TextPart(t *testing.T) {
	message := a2atype.NewMessage(a2atype.MessageRoleUser,
		a2atype.TextPart{Text: "I have approved this action"},
	)
	result := ExtractDecisionFromMessage(message)
	if result != DecisionApprove {
		t.Errorf("ExtractDecisionFromMessage(approve text) = %q, want %q", result, DecisionApprove)
	}

	message = a2atype.NewMessage(a2atype.MessageRoleUser,
		a2atype.TextPart{Text: "Request denied, do not proceed"},
	)
	result = ExtractDecisionFromMessage(message)
	if result != DecisionDeny {
		t.Errorf("ExtractDecisionFromMessage(deny text) = %q, want %q", result, DecisionDeny)
	}

	message = a2atype.NewMessage(a2atype.MessageRoleUser,
		a2atype.TextPart{Text: "APPROVED"},
	)
	result = ExtractDecisionFromMessage(message)
	if result != DecisionApprove {
		t.Errorf("ExtractDecisionFromMessage(APPROVED) = %q, want %q", result, DecisionApprove)
	}
}

func TestExtractDecisionFromMessage_Priority(t *testing.T) {
	message := a2atype.NewMessage(a2atype.MessageRoleUser,
		a2atype.TextPart{Text: "approved"},
		&a2atype.DataPart{
			Data: map[string]any{
				KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeDeny,
			},
		},
	)
	result := ExtractDecisionFromMessage(message)
	if result != DecisionDeny {
		t.Errorf("ExtractDecisionFromMessage(mixed parts) = %q, want %q (DataPart should take priority)", result, DecisionDeny)
	}
}

func TestExtractDecisionFromMessage_EdgeCases(t *testing.T) {
	result := ExtractDecisionFromMessage(nil)
	if result != "" {
		t.Errorf("ExtractDecisionFromMessage(nil) = %q, want empty string", result)
	}

	message := a2atype.NewMessage(a2atype.MessageRoleUser)
	result = ExtractDecisionFromMessage(message)
	if result != "" {
		t.Errorf("ExtractDecisionFromMessage(empty parts) = %q, want empty string", result)
	}

	message = a2atype.NewMessage(a2atype.MessageRoleUser,
		a2atype.TextPart{Text: "This is just a comment"},
	)
	result = ExtractDecisionFromMessage(message)
	if result != "" {
		t.Errorf("ExtractDecisionFromMessage(no decision) = %q, want empty string", result)
	}
}

func TestExtractDecisionFromText_WordBoundary(t *testing.T) {
	tests := []struct {
		name string
		text string
		want DecisionType
	}{
		{name: "no inside know should not match", text: "I know what you want, approved", want: DecisionApprove},
		{name: "yes inside yesterday should not match", text: "yesterday was fine", want: ""},
		{name: "stop inside unstoppable should not match", text: "unstoppable progress", want: ""},
		{name: "cancel inside cancellation should not match", text: "the cancellation policy", want: ""},
		{name: "standalone no matches", text: "no, I do not agree", want: DecisionDeny},
		{name: "standalone yes matches", text: "yes, go ahead", want: DecisionApprove},
		{name: "standalone stop matches", text: "stop the process", want: DecisionDeny},
		{name: "case insensitive whole word", text: "NO", want: DecisionDeny},
		{name: "keyword at end of sentence", text: "the answer is no", want: DecisionDeny},
		{name: "keyword with punctuation", text: "no!", want: DecisionDeny},
		{name: "continue inside discontinue should not match", text: "I will discontinue", want: ""},
		{name: "approve as standalone", text: "I approve", want: DecisionApprove},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractDecisionFromText(tt.text)
			if got != tt.want {
				t.Errorf("ExtractDecisionFromText(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestFormatToolApprovalTextParts(t *testing.T) {
	requests := []ToolApprovalRequest{
		{Name: "search", Args: map[string]any{"query": "test"}},
		{Name: "run`code`", Args: map[string]any{"cmd": "echo `test`"}},
		{Name: "reset", Args: map[string]any{}},
	}

	part := formatToolApprovalTextParts(requests)

	if !strings.Contains(part.Text, "Approval Required") {
		t.Error("should contain 'Approval Required'")
	}
	if !strings.Contains(part.Text, "search") {
		t.Error("should contain 'search'")
	}
	if !strings.Contains(part.Text, "reset") {
		t.Error("should contain 'reset'")
	}
	if !strings.Contains(part.Text, "\\`") {
		t.Error("should escape backticks")
	}
}

func TestBuildToolApprovalMessage(t *testing.T) {
	t.Run("single action", func(t *testing.T) {
		requests := []ToolApprovalRequest{
			{Name: "search", Args: map[string]any{"query": "test"}, ID: "call_1"},
		}
		msg := BuildToolApprovalMessage(requests)

		if msg == nil {
			t.Fatal("BuildToolApprovalMessage() returned nil")
		}
		if len(msg.Parts) == 0 {
			t.Fatal("BuildToolApprovalMessage() returned message with no parts")
		}

		var textContent string
		var dataPart *a2atype.DataPart
		for _, part := range msg.Parts {
			switch p := part.(type) {
			case a2atype.TextPart:
				textContent += p.Text
			case *a2atype.DataPart:
				dataPart = p
			}
		}

		if !strings.Contains(textContent, "Approval Required") {
			t.Error("message should contain 'Approval Required' text")
		}
		if !strings.Contains(textContent, "search") {
			t.Error("message should contain tool name 'search'")
		}
		if dataPart == nil {
			t.Fatal("message should contain a DataPart with interrupt data")
		}
		if dataPart.Data["interrupt_type"] != KAgentHitlInterruptTypeToolApproval {
			t.Errorf("DataPart interrupt_type = %v, want %q", dataPart.Data["interrupt_type"], KAgentHitlInterruptTypeToolApproval)
		}
		if dataPart.Metadata[GetKAgentMetadataKey("type")] != "interrupt_data" {
			t.Errorf("DataPart metadata type = %v, want %q", dataPart.Metadata[GetKAgentMetadataKey("type")], "interrupt_data")
		}

		actionRequestsData, ok := dataPart.Data["action_requests"].([]map[string]any)
		if !ok {
			t.Fatalf("action_requests type = %T, want []map[string]any", dataPart.Data["action_requests"])
		}
		if len(actionRequestsData) != 1 {
			t.Fatalf("action_requests length = %d, want 1", len(actionRequestsData))
		}
		if actionRequestsData[0]["name"] != "search" {
			t.Errorf("action_requests[0].name = %v, want %q", actionRequestsData[0]["name"], "search")
		}
		if actionRequestsData[0]["id"] != "call_1" {
			t.Errorf("action_requests[0].id = %v, want %q", actionRequestsData[0]["id"], "call_1")
		}
	})

	t.Run("omits empty ID", func(t *testing.T) {
		requests := []ToolApprovalRequest{
			{Name: "reset", Args: map[string]any{}},
		}
		msg := BuildToolApprovalMessage(requests)

		var dataPart *a2atype.DataPart
		for _, part := range msg.Parts {
			if dp, ok := part.(*a2atype.DataPart); ok {
				dataPart = dp
				break
			}
		}
		if dataPart == nil {
			t.Fatal("expected DataPart")
		}
		actionRequestsData := dataPart.Data["action_requests"].([]map[string]any)
		if _, hasID := actionRequestsData[0]["id"]; hasID {
			t.Error("action_requests[0] should not have 'id' key when ID is empty")
		}
	})
}

func TestExtractApprovalRequestsFromA2AParts(t *testing.T) {
	tests := []struct {
		name     string
		parts    []a2atype.Part
		wantLen  int
		wantName string
	}{
		{
			name:    "nil parts",
			parts:   nil,
			wantLen: 0,
		},
		{
			name:    "empty parts",
			parts:   []a2atype.Part{},
			wantLen: 0,
		},
		{
			name: "text part only",
			parts: []a2atype.Part{
				a2atype.TextPart{Text: "hello"},
			},
			wantLen: 0,
		},
		{
			name: "data part without kagent_type",
			parts: []a2atype.Part{
				a2atype.DataPart{Data: map[string]any{"foo": "bar"}},
			},
			wantLen: 0,
		},
		{
			name: "data part with kagent_type=function_call and is_long_running=true",
			parts: []a2atype.Part{
				a2atype.DataPart{
					Data: map[string]any{
						"name": "search_tool",
						"args": map[string]any{"q": "test"},
						"id":   "call_1",
					},
					Metadata: map[string]any{
						"kagent_type":            "function_call",
						"kagent_is_long_running": true,
					},
				},
			},
			wantLen:  1,
			wantName: "search_tool",
		},
		{
			name: "data part with function_call but is_long_running=false",
			parts: []a2atype.Part{
				a2atype.DataPart{
					Data: map[string]any{
						"name": "search_tool",
						"args": map[string]any{"q": "test"},
						"id":   "call_1",
					},
					Metadata: map[string]any{
						"kagent_type":            "function_call",
						"kagent_is_long_running": false,
					},
				},
			},
			wantLen: 0,
		},
		{
			name: "request_euc is excluded",
			parts: []a2atype.Part{
				a2atype.DataPart{
					Data: map[string]any{
						"name": requestEucFunctionCallName,
						"args": map[string]any{},
						"id":   "call_1",
					},
					Metadata: map[string]any{
						"kagent_type":            "function_call",
						"kagent_is_long_running": true,
					},
				},
			},
			wantLen: 0,
		},
		{
			name: "multiple parts with mixed types",
			parts: []a2atype.Part{
				a2atype.DataPart{
					Data: map[string]any{
						"name": "tool_a",
						"args": map[string]any{"x": 1},
						"id":   "call_1",
					},
					Metadata: map[string]any{
						"kagent_type":            "function_call",
						"kagent_is_long_running": true,
					},
				},
				a2atype.TextPart{Text: "some text"},
				a2atype.DataPart{
					Data: map[string]any{
						"name": "tool_b",
						"args": map[string]any{"y": 2},
						"id":   "call_2",
					},
					Metadata: map[string]any{
						"kagent_type":            "function_call",
						"kagent_is_long_running": true,
					},
				},
			},
			wantLen:  2,
			wantName: "tool_a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractApprovalRequestsFromA2AParts(tt.parts)
			if len(got) != tt.wantLen {
				t.Errorf("extractApprovalRequestsFromA2AParts() returned %d requests, want %d", len(got), tt.wantLen)
			}
			if tt.wantName != "" && len(got) > 0 && got[0].Name != tt.wantName {
				t.Errorf("first request name = %q, want %q", got[0].Name, tt.wantName)
			}
		})
	}
}
