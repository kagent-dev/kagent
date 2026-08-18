package a2a

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"google.golang.org/adk/v2/tool/toolconfirmation"
)

func dataPart(data map[string]any, metadata map[string]any) *a2atype.Part {
	part := a2atype.NewDataPart(data)
	part.Metadata = metadata
	return part
}

func confirmationPart(id, toolName, toolID string, args, payload map[string]any) *a2atype.Part {
	return dataPart(map[string]any{
		"name": toolconfirmation.FunctionCallName,
		"id":   id,
		"args": map[string]any{
			"originalFunctionCall": map[string]any{"name": toolName, "id": toolID, "args": args},
			"toolConfirmation":     map[string]any{"hint": "Please confirm", "payload": payload},
		},
	}, map[string]any{"adk_type": "function_call", "adk_is_long_running": true})
}

func hintedConfirmationPart(id, toolName, toolID, hint string) *a2atype.Part {
	return dataPart(map[string]any{
		"name": toolconfirmation.FunctionCallName,
		"id":   id,
		"args": map[string]any{
			"originalFunctionCall": map[string]any{"name": toolName, "id": toolID},
			"toolConfirmation":     map[string]any{"hint": hint},
		},
	}, map[string]any{"adk_type": "function_call", "adk_is_long_running": true})
}

func hintlessConfirmationPart(id, toolName, toolID string) *a2atype.Part {
	return dataPart(map[string]any{
		"name": toolconfirmation.FunctionCallName,
		"id":   id,
		"args": map[string]any{
			"originalFunctionCall": map[string]any{"name": toolName, "id": toolID},
		},
	}, map[string]any{"adk_type": "function_call", "adk_is_long_running": true})
}

func askUserConfirmationPart(id, toolID string, questions []any, hint string) *a2atype.Part {
	return dataPart(map[string]any{
		"name": toolconfirmation.FunctionCallName,
		"id":   id,
		"args": map[string]any{
			"originalFunctionCall": map[string]any{
				"name": "ask_user", "id": toolID, "args": map[string]any{"questions": questions},
			},
			"toolConfirmation": map[string]any{"hint": hint},
		},
	}, map[string]any{"adk_type": "function_call", "adk_is_long_running": true})
}

func messageText(message *a2atype.Message) string {
	var text strings.Builder
	for _, part := range message.Parts {
		if content, ok := part.Content.(a2atype.Text); ok {
			text.WriteString(string(content))
		}
	}
	return text.String()
}

func hitlDecisionMessage(payload any) *a2atype.Message {
	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Decision"))
	return AttachHitlExtension(message, payload)
}

func TestHitlExtensionAttachAndParse(t *testing.T) {
	message := hitlDecisionMessage(&ToolApprovalResponse{
		Type:      HITLTypeToolApprovalResponse,
		Approvals: []ToolApproval{{ID: "confirm-1", Approved: true}},
	})
	payload := GetToolApprovalResponse(message)
	if payload == nil || len(payload.Approvals) != 1 || !payload.Approvals[0].Approved {
		t.Fatalf("unexpected HITL payload: %#v", payload)
	}
}

func TestHitlExtensionRejectsEmptyApprovals(t *testing.T) {
	message := AttachHitlExtension(
		a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Decision")),
		map[string]any{"type": HITLTypeToolApprovalResponse, "approvals": []any{}},
	)
	if GetToolApprovalResponse(message) != nil {
		t.Fatal("empty approvals should not decode")
	}
}

func TestHitlExtensionDoesNotReadLegacyDataPart(t *testing.T) {
	legacy := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewDataPart(map[string]any{"decision_type": "approve"}))
	if IsHITLResponse(legacy) {
		t.Fatal("legacy DataPart was treated as a HITL response")
	}
}

func TestHITLActivationInterceptor(t *testing.T) {
	ctx, callCtx := a2asrv.NewCallContext(context.Background(), a2asrv.NewServiceParams(map[string][]string{
		a2atype.SvcParamExtensions: {HITLExtensionURI},
	}))
	if _, _, err := HITLActivationInterceptor().Before(ctx, callCtx, &a2asrv.Request{}); err != nil {
		t.Fatalf("Before() error = %v", err)
	}
	if !HitlActivated(ctx) {
		t.Fatal("HITL was not activated")
	}
}

func TestBuildHITLStatusMessage(t *testing.T) {
	t.Run("tool approval", func(t *testing.T) {
		internal := a2atype.NewMessage(a2atype.MessageRoleAgent,
			confirmationPart("confirm-1", "delete_file", "call-1", map[string]any{"path": "/tmp/x"}, nil))
		payload := GetToolApprovalRequest(BuildHITLStatusMessage(internal, true))
		if payload == nil || payload.Tools[0].ID != "confirm-1" {
			t.Fatalf("payload = %#v", payload)
		}
	})

	t.Run("ask user", func(t *testing.T) {
		questions := []any{map[string]any{"question": "Which database?"}}
		internal := a2atype.NewMessage(a2atype.MessageRoleAgent,
			confirmationPart("confirm-2", "ask_user", "call-2", map[string]any{"questions": questions}, nil))
		payload := GetAskUserRequest(BuildHITLStatusMessage(internal, true))
		if payload == nil || len(payload.Questions) != 1 {
			t.Fatalf("payload = %#v", payload)
		}
	})

	t.Run("nested subagent", func(t *testing.T) {
		remote := RemoteHitlState{
			TaskID: "child-task", ContextID: "child-context", SubagentName: "k8s_agent",
			ToolApprovalRequest: &ToolApprovalRequest{
				Type: HITLTypeToolApprovalRequest,
				Tools: []HitlTool{{
					ID: "child-confirm", CallID: "child-call", Name: "delete_pod",
					Args: map[string]any{"name": "api"},
				}},
			},
		}
		payload := GetToolApprovalRequest(BuildHITLStatusMessage(
			a2atype.NewMessage(a2atype.MessageRoleAgent,
				confirmationPart("parent-confirm", "k8s_agent", "parent-call", nil, remote.ToMap())),
			true,
		))
		if payload == nil || payload.Nested == nil || payload.Nested.Tools[0].CallID != "child-call" {
			t.Fatalf("payload = %#v", payload)
		}
	})

	t.Run("not activated", func(t *testing.T) {
		public := BuildHITLStatusMessage(a2atype.NewMessage(a2atype.MessageRoleAgent,
			confirmationPart("confirm-1", "delete_file", "call-1", nil, nil)), false)
		if GetToolApprovalRequest(public) != nil {
			t.Fatalf("unexpected payload on inactive client")
		}
		if want := "Please confirm (delete_file)"; messageText(public) != want {
			t.Errorf("text = %q, want %q", messageText(public), want)
		}
	})

	t.Run("not activated without a tool hint names the tool", func(t *testing.T) {
		public := BuildHITLStatusMessage(a2atype.NewMessage(a2atype.MessageRoleAgent,
			hintlessConfirmationPart("confirm-1", "delete_file", "call-1")), false)
		if want := "Approval is required for tool(s): delete_file"; messageText(public) != want {
			t.Errorf("text = %q, want %q", messageText(public), want)
		}
	})

	t.Run("several tools keep every hint and every name", func(t *testing.T) {
		internal := a2atype.NewMessage(a2atype.MessageRoleAgent,
			confirmationPart("confirm-1", "delete_file", "call-1", nil, nil),
			hintlessConfirmationPart("confirm-2", "restart_pod", "call-2"))
		want := "Please confirm (delete_file, restart_pod)"
		if text := messageText(BuildHITLStatusMessage(internal, false)); text != want {
			t.Errorf("text = %q, want %q", text, want)
		}
	})

	t.Run("hints join in tool order", func(t *testing.T) {
		internal := a2atype.NewMessage(a2atype.MessageRoleAgent,
			hintedConfirmationPart("confirm-1", "delete_file", "call-1", "First hint"),
			hintedConfirmationPart("confirm-2", "restart_pod", "call-2", "Second hint"))
		want := "First hint; Second hint (delete_file, restart_pod)"
		if text := messageText(BuildHITLStatusMessage(internal, false)); text != want {
			t.Errorf("text = %q, want %q", text, want)
		}
	})

	t.Run("activated payload hint repeats the text", func(t *testing.T) {
		public := BuildHITLStatusMessage(a2atype.NewMessage(a2atype.MessageRoleAgent,
			hintlessConfirmationPart("confirm-1", "delete_file", "call-1")), true)
		payload := GetToolApprovalRequest(public)
		if payload == nil {
			t.Fatal("payload = nil, want a tool approval request")
		}
		if payload.Hint != messageText(public) {
			t.Errorf("hint = %q, text = %q, want them identical", payload.Hint, messageText(public))
		}
	})

	t.Run("not activated ask_user carries the question", func(t *testing.T) {
		questions := []any{map[string]any{"question": "Which database?"}}
		internal := a2atype.NewMessage(a2atype.MessageRoleAgent,
			askUserConfirmationPart("confirm-2", "call-2", questions, "Which database?"))
		public := BuildHITLStatusMessage(internal, false)
		if GetAskUserRequest(public) != nil {
			t.Fatal("unexpected payload on inactive client")
		}
		if want := "Which database?"; messageText(public) != want {
			t.Errorf("text = %q, want the question verbatim %q", messageText(public), want)
		}
	})

	t.Run("non-confirmation long-running call", func(t *testing.T) {
		public := BuildHITLStatusMessage(a2atype.NewMessage(a2atype.MessageRoleAgent,
			dataPart(map[string]any{"name": "auth_required"}, map[string]any{
				"adk_type": "function_call", "adk_is_long_running": true,
			})), true)
		if GetToolApprovalRequest(public) != nil {
			t.Fatal("non-confirmation long-running call produced HITL payload")
		}
	})
}

func TestBuildResumeHITLMessageAskUser(t *testing.T) {
	incoming := hitlDecisionMessage(&AskUserResponse{
		Type: HITLTypeAskUserResponse, ID: "confirm-1",
		Answers: []AskUserAnswer{{Answer: []string{"PostgreSQL"}}},
	})
	stored := &a2atype.Task{Status: a2atype.TaskStatus{
		State: a2atype.TaskStateInputRequired,
		Message: AttachHitlExtension(a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("Answer required")), &AskUserRequest{
			Type: HITLTypeAskUserRequest, ID: "confirm-1",
		}),
	}}
	resume, err := BuildResumeHITLMessage(stored, incoming)
	if err != nil || len(resume.Parts) != 1 {
		t.Fatalf("resume = %#v, err = %v", resume, err)
	}
}

func TestBuildResumeHITLMessageBatchFlattensApprovals(t *testing.T) {
	stored := &a2atype.Task{Status: a2atype.TaskStatus{
		State: a2atype.TaskStateInputRequired,
		Message: AttachHitlExtension(a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("Approval required")), &ToolApprovalRequest{
			Type: HITLTypeToolApprovalRequest,
			Tools: []HitlTool{
				{ID: "confirm-1", CallID: "call-1", Name: "delete_file", Args: map[string]any{}},
				{ID: "confirm-2", CallID: "call-2", Name: "restart_pod", Args: map[string]any{}},
			},
		}),
	}}
	incoming := hitlDecisionMessage(&ToolApprovalResponse{
		Type: HITLTypeToolApprovalResponse,
		Approvals: []ToolApproval{
			{ID: "confirm-1", Approved: true},
			{ID: "confirm-2", Approved: false, RejectionReason: "not now"},
		},
	})
	resume, err := BuildResumeHITLMessage(stored, incoming)
	if err != nil || len(resume.Parts) != 2 {
		t.Fatalf("resume parts = %v, err = %v", len(resume.Parts), err)
	}
}

func TestBuildResumeHITLMessageNestedAskUser(t *testing.T) {
	stored := &a2atype.Task{Status: a2atype.TaskStatus{
		State: a2atype.TaskStateInputRequired,
		Message: AttachHitlExtension(a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("Answer required")), &AskUserRequest{
			Type: HITLTypeAskUserRequest, ID: "parent-confirm",
			Questions: []map[string]any{{"question": "Which namespace?"}},
			Nested: &NestedHitlRequest{
				TaskID: "child-task", ContextID: "child-context", SubagentName: "child",
				Tools: []HitlTool{{ID: "child-confirm", CallID: "child-call", Name: "ask_user", Args: map[string]any{}}},
			},
		}),
	}}
	incoming := hitlDecisionMessage(&AskUserResponse{
		Type: HITLTypeAskUserResponse, ID: "child-confirm",
		Answers: []AskUserAnswer{{Answer: []string{"default"}}},
	})
	resume, err := BuildResumeHITLMessage(stored, incoming)
	if err != nil {
		t.Fatalf("BuildResumeHITLMessage() error = %v", err)
	}
	response := asDataPart(resume.Parts[0])[PartKeyResponse].(map[string]any)["response"].(string)
	var confirmation toolconfirmation.ToolConfirmation
	if err := json.Unmarshal([]byte(response), &confirmation); err != nil {
		t.Fatalf("confirmation JSON: %v", err)
	}
	state := ParseRemoteHitlState(confirmation.Payload.(map[string]any))
	if state == nil || state.AskUserResponse == nil || len(state.AskUserResponse.Answers) != 1 {
		t.Fatalf("nested confirmation payload = %#v", confirmation.Payload)
	}
}

func TestBuildResumeHITLMessageNestedApprovals(t *testing.T) {
	stored := &a2atype.Task{Status: a2atype.TaskStatus{
		State: a2atype.TaskStateInputRequired,
		Message: AttachHitlExtension(a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("Approval required")), &ToolApprovalRequest{
			Type:  HITLTypeToolApprovalRequest,
			Tools: []HitlTool{{ID: "parent-confirm", CallID: "parent-call", Name: "child", Args: map[string]any{}}},
			Nested: &NestedHitlRequest{
				TaskID: "child-task", ContextID: "child-context", SubagentName: "child",
				Tools: []HitlTool{
					{ID: "child-confirm-1", CallID: "child-call-1", Name: "delete_pod", Args: map[string]any{}},
					{ID: "child-confirm-2", CallID: "child-call-2", Name: "restart_pod", Args: map[string]any{}},
				},
			},
		}),
	}}
	incoming := hitlDecisionMessage(&ToolApprovalResponse{
		Type: HITLTypeToolApprovalResponse,
		Approvals: []ToolApproval{
			{ID: "child-confirm-1", Approved: true},
			{ID: "child-confirm-2", Approved: false, RejectionReason: "not now"},
		},
	})
	resume, err := BuildResumeHITLMessage(stored, incoming)
	if err != nil {
		t.Fatalf("BuildResumeHITLMessage() error = %v", err)
	}
	response := asDataPart(resume.Parts[0])[PartKeyResponse].(map[string]any)["response"].(string)
	var confirmation toolconfirmation.ToolConfirmation
	_ = json.Unmarshal([]byte(response), &confirmation)
	state := ParseRemoteHitlState(confirmation.Payload.(map[string]any))
	if confirmation.Confirmed || state == nil || state.ToolApprovalResponse == nil ||
		state.ToolApprovalResponse.Approvals[1].RejectionReason != "not now" {
		t.Fatalf("nested confirmation = %#v", confirmation)
	}
}

func TestBuildRemoteHitlStateAndHint(t *testing.T) {
	task := &a2atype.Task{
		ID: "child-task", ContextID: "child-context",
		Status: a2atype.TaskStatus{
			Message: AttachHitlExtension(a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("pause")), &ToolApprovalRequest{
				Type: HITLTypeToolApprovalRequest,
				Tools: []HitlTool{{
					ID: "child-confirm", CallID: "child-call", Name: "delete_pod", Args: map[string]any{},
				}},
			}),
		},
	}
	state := BuildRemoteHitlState(task, "k8s_agent")
	if state == nil || state.ToolApprovalRequest == nil {
		t.Fatalf("state = %#v", state)
	}
	if got := RemoteHitlHint(state); got != "Remote agent 'k8s_agent' requires approval for tool(s): delete_pod" {
		t.Fatalf("hint = %q", got)
	}
}
