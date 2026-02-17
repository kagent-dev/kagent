package a2a

import (
	"context"
	"errors"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	adkmodel "google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

// mockTaskInfoProvider implements a2atype.TaskInfoProvider for tests.
type mockTaskInfoProvider struct {
	taskID    a2atype.TaskID
	contextID string
}

func (m *mockTaskInfoProvider) TaskInfo() a2atype.TaskInfo {
	return a2atype.TaskInfo{
		TaskID:    m.taskID,
		ContextID: m.contextID,
	}
}

type mockEventWriter struct {
	events []a2atype.Event
	err    error
}

func (m *mockEventWriter) Write(ctx context.Context, event a2atype.Event) error {
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, event)
	return nil
}

// --- Constant tests ---

func TestHITLConstants(t *testing.T) {
	if KAgentHitlInterruptTypeToolApproval != "tool_approval" {
		t.Errorf("KAgentHitlInterruptTypeToolApproval = %q, want %q", KAgentHitlInterruptTypeToolApproval, "tool_approval")
	}

	if KAgentHitlDecisionTypeKey != "decision_type" {
		t.Errorf("KAgentHitlDecisionTypeKey = %q, want %q", KAgentHitlDecisionTypeKey, "decision_type")
	}
	if KAgentHitlDecisionTypeApprove != "approve" {
		t.Errorf("KAgentHitlDecisionTypeApprove = %q, want %q", KAgentHitlDecisionTypeApprove, "approve")
	}
	if KAgentHitlDecisionTypeDeny != "deny" {
		t.Errorf("KAgentHitlDecisionTypeDeny = %q, want %q", KAgentHitlDecisionTypeDeny, "deny")
	}
	if KAgentHitlDecisionTypeReject != "reject" {
		t.Errorf("KAgentHitlDecisionTypeReject = %q, want %q", KAgentHitlDecisionTypeReject, "reject")
	}

	hasApproved := false
	hasProceed := false
	for _, keyword := range KAgentHitlResumeKeywordsApprove {
		if keyword == "approved" {
			hasApproved = true
		}
		if keyword == "proceed" {
			hasProceed = true
		}
	}
	if !hasApproved {
		t.Error("KAgentHitlResumeKeywordsApprove should contain 'approved'")
	}
	if !hasProceed {
		t.Error("KAgentHitlResumeKeywordsApprove should contain 'proceed'")
	}

	hasDenied := false
	hasCancel := false
	for _, keyword := range KAgentHitlResumeKeywordsDeny {
		if keyword == "denied" {
			hasDenied = true
		}
		if keyword == "cancel" {
			hasCancel = true
		}
	}
	if !hasDenied {
		t.Error("KAgentHitlResumeKeywordsDeny should contain 'denied'")
	}
	if !hasCancel {
		t.Error("KAgentHitlResumeKeywordsDeny should contain 'cancel'")
	}
}

// --- Utility tests ---

func TestEscapeMarkdownBackticks(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{name: "single backtick", input: "foo`bar", expected: "foo\\`bar"},
		{name: "multiple backticks", input: "`code` and `more`", expected: "\\`code\\` and \\`more\\`"},
		{name: "plain text", input: "plain text", expected: "plain text"},
		{name: "empty string", input: "", expected: ""},
		{name: "non-string type", input: 123, expected: "123"},
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
		{name: "input_required state", state: a2atype.TaskStateInputRequired, expected: true},
		{name: "working state", state: a2atype.TaskStateWorking, expected: false},
		{name: "completed state", state: a2atype.TaskStateCompleted, expected: false},
		{name: "failed state", state: a2atype.TaskStateFailed, expected: false},
		{name: "empty state", state: a2atype.TaskState(""), expected: false},
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

	parts := formatToolApprovalTextParts(requests)

	textContent := ""
	for _, p := range parts {
		if tp, ok := p.(a2atype.TextPart); ok {
			textContent += tp.Text
		}
	}

	if !strings.Contains(textContent, "Approval Required") {
		t.Error("formatToolApprovalTextParts should contain 'Approval Required'")
	}
	if !strings.Contains(textContent, "search") {
		t.Error("formatToolApprovalTextParts should contain 'search'")
	}
	if !strings.Contains(textContent, "reset") {
		t.Error("formatToolApprovalTextParts should contain 'reset'")
	}
	if !strings.Contains(textContent, "\\`") {
		t.Error("formatToolApprovalTextParts should escape backticks")
	}
}

// --- Handler tests ---

func TestHandleToolApprovalInterrupt_SingleAction(t *testing.T) {
	eventWriter := &mockEventWriter{}
	infoProvider := &mockTaskInfoProvider{taskID: "task123", contextID: "ctx456"}

	actionRequests := []ToolApprovalRequest{
		{Name: "search", Args: map[string]any{"query": "test"}},
	}

	msg, err := HandleToolApprovalInterrupt(
		context.Background(),
		actionRequests,
		infoProvider,
		eventWriter,
		"test_app",
	)

	if err != nil {
		t.Fatalf("HandleToolApprovalInterrupt() error = %v, want nil", err)
	}
	if msg == nil {
		t.Fatal("HandleToolApprovalInterrupt() returned nil message")
	}

	if len(eventWriter.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(eventWriter.events))
	}

	event, ok := eventWriter.events[0].(*a2atype.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("Expected TaskStatusUpdateEvent, got %T", eventWriter.events[0])
	}

	if event.TaskID != "task123" {
		t.Errorf("event.TaskID = %q, want %q", event.TaskID, "task123")
	}
	if event.ContextID != "ctx456" {
		t.Errorf("event.ContextID = %q, want %q", event.ContextID, "ctx456")
	}
	if event.Status.State != a2atype.TaskStateInputRequired {
		t.Errorf("event.Status.State = %v, want %v", event.Status.State, a2atype.TaskStateInputRequired)
	}
	if event.Final {
		t.Error("event.Final = true, want false")
	}
	if event.Metadata["interrupt_type"] != KAgentHitlInterruptTypeToolApproval {
		t.Errorf("event.Metadata[interrupt_type] = %v, want %q", event.Metadata["interrupt_type"], KAgentHitlInterruptTypeToolApproval)
	}
}

func TestHandleToolApprovalInterrupt_MultipleActions(t *testing.T) {
	eventWriter := &mockEventWriter{}
	infoProvider := &mockTaskInfoProvider{taskID: "task456", contextID: "ctx789"}

	actionRequests := []ToolApprovalRequest{
		{Name: "tool1", Args: map[string]any{"a": 1}},
		{Name: "tool2", Args: map[string]any{"b": 2}},
	}

	_, err := HandleToolApprovalInterrupt(
		context.Background(),
		actionRequests,
		infoProvider,
		eventWriter,
		"",
	)

	if err != nil {
		t.Fatalf("HandleToolApprovalInterrupt() error = %v, want nil", err)
	}

	if len(eventWriter.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(eventWriter.events))
	}

	event, ok := eventWriter.events[0].(*a2atype.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("Expected TaskStatusUpdateEvent, got %T", eventWriter.events[0])
	}

	var dataPart *a2atype.DataPart
	for _, part := range event.Status.Message.Parts {
		if dp, ok := part.(*a2atype.DataPart); ok {
			dataPart = dp
			break
		}
	}

	if dataPart == nil {
		t.Fatal("Expected DataPart with action_requests, got none")
	}

	actionRequestsData, ok := dataPart.Data["action_requests"].([]map[string]any)
	if !ok {
		if arr, ok := dataPart.Data["action_requests"].([]any); ok {
			actionRequestsData = make([]map[string]any, len(arr))
			for i, v := range arr {
				if m, ok := v.(map[string]any); ok {
					actionRequestsData[i] = m
				}
			}
		} else {
			t.Fatalf("Expected action_requests to be []map[string]any, got %T", dataPart.Data["action_requests"])
		}
	}

	if len(actionRequestsData) != 2 {
		t.Errorf("Expected 2 action requests, got %d", len(actionRequestsData))
	}
}

func TestHandleToolApprovalInterrupt_EventWriterError(t *testing.T) {
	eventWriter := &mockEventWriter{
		err: errors.New("write failed"),
	}
	infoProvider := &mockTaskInfoProvider{taskID: "task123", contextID: "ctx456"}

	actionRequests := []ToolApprovalRequest{
		{Name: "test", Args: map[string]any{}},
	}

	_, err := HandleToolApprovalInterrupt(
		context.Background(),
		actionRequests,
		infoProvider,
		eventWriter,
		"",
	)

	if err == nil {
		t.Error("HandleToolApprovalInterrupt() error = nil, want error")
	}
}

// --- BuildToolApprovalMessage tests ---

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

		// Should contain text parts and one data part
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

// --- ExtractToolApprovalRequests tests ---

func TestExtractToolApprovalRequests(t *testing.T) {
	tests := []struct {
		name     string
		event    *adksession.Event
		wantLen  int
		wantName string // name of first request, if any
	}{
		{
			name:    "nil event",
			event:   nil,
			wantLen: 0,
		},
		{
			name: "partial event is skipped",
			event: &adksession.Event{
				LLMResponse: adkmodel.LLMResponse{
					Partial: true,
					Content: &genai.Content{
						Parts: []*genai.Part{
							genai.NewPartFromFunctionCall("my_tool", map[string]any{"a": 1}),
						},
					},
				},
				LongRunningToolIDs: []string{"call_1"},
			},
			wantLen: 0,
		},
		{
			name: "no long-running tool IDs",
			event: &adksession.Event{
				LLMResponse: adkmodel.LLMResponse{
					Content: &genai.Content{
						Parts: []*genai.Part{
							genai.NewPartFromFunctionCall("my_tool", map[string]any{"a": 1}),
						},
					},
				},
			},
			wantLen: 0,
		},
		{
			name: "no content",
			event: &adksession.Event{
				LongRunningToolIDs: []string{"call_1"},
			},
			wantLen: 0,
		},
		{
			name: "function call matches long-running ID",
			event: func() *adksession.Event {
				part := genai.NewPartFromFunctionCall("search", map[string]any{"q": "test"})
				part.FunctionCall.ID = "call_1"
				return &adksession.Event{
					LongRunningToolIDs: []string{"call_1"},
					LLMResponse: adkmodel.LLMResponse{
						Content: &genai.Content{
							Parts: []*genai.Part{part},
						},
					},
				}
			}(),
			wantLen:  1,
			wantName: "search",
		},
		{
			name: "function call ID not in long-running set",
			event: func() *adksession.Event {
				part := genai.NewPartFromFunctionCall("search", map[string]any{"q": "test"})
				part.FunctionCall.ID = "call_99"
				return &adksession.Event{
					LongRunningToolIDs: []string{"call_1"},
					LLMResponse: adkmodel.LLMResponse{
						Content: &genai.Content{
							Parts: []*genai.Part{part},
						},
					},
				}
			}(),
			wantLen: 0,
		},
		{
			name: "request_euc is excluded",
			event: func() *adksession.Event {
				part := genai.NewPartFromFunctionCall(requestEucFunctionCallName, map[string]any{})
				part.FunctionCall.ID = "call_1"
				return &adksession.Event{
					LongRunningToolIDs: []string{"call_1"},
					LLMResponse: adkmodel.LLMResponse{
						Content: &genai.Content{
							Parts: []*genai.Part{part},
						},
					},
				}
			}(),
			wantLen: 0,
		},
		{
			name: "multiple function calls with mixed matching",
			event: func() *adksession.Event {
				p1 := genai.NewPartFromFunctionCall("tool_a", map[string]any{"x": 1})
				p1.FunctionCall.ID = "call_1"
				p2 := genai.NewPartFromFunctionCall("tool_b", map[string]any{"y": 2})
				p2.FunctionCall.ID = "call_2"
				p3 := genai.NewPartFromFunctionCall("tool_c", map[string]any{"z": 3})
				p3.FunctionCall.ID = "call_3"
				return &adksession.Event{
					LongRunningToolIDs: []string{"call_1", "call_3"},
					LLMResponse: adkmodel.LLMResponse{
						Content: &genai.Content{
							Parts: []*genai.Part{p1, p2, p3},
						},
					},
				}
			}(),
			wantLen:  2,
			wantName: "tool_a",
		},
		{
			name: "nil content returns nothing",
			event: &adksession.Event{
				LongRunningToolIDs: []string{"call_1"},
				LLMResponse: adkmodel.LLMResponse{
					Content: nil,
				},
			},
			wantLen: 0,
		},
		{
			name: "function call without ID is skipped",
			event: &adksession.Event{
				LongRunningToolIDs: []string{"call_1"},
				LLMResponse: adkmodel.LLMResponse{
					Content: &genai.Content{
						Parts: []*genai.Part{
							genai.NewPartFromFunctionCall("no_id_tool", map[string]any{}),
						},
					},
				},
			},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractToolApprovalRequests(tt.event)
			if len(got) != tt.wantLen {
				t.Errorf("ExtractToolApprovalRequests() returned %d requests, want %d", len(got), tt.wantLen)
			}
			if tt.wantName != "" && len(got) > 0 && got[0].Name != tt.wantName {
				t.Errorf("first request name = %q, want %q", got[0].Name, tt.wantName)
			}
		})
	}
}
