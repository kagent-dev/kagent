package a2a

import (
	"context"
	"encoding/json"
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

func hitlDecisionMessage(payload map[string]any) *a2atype.Message {
	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Decision"))
	return AttachHitlExtension(message, payload)
}

func TestHitlExtensionAttachAndParse(t *testing.T) {
	message := hitlDecisionMessage(map[string]any{
		"type":      HITLTypeToolApprovalResponse,
		"approvals": []any{map[string]any{"id": "confirm-1", "approved": true}},
	})
	payload := GetHitlPayload(message)
	approvals, err := ExtractApprovalResults(message)
	if err != nil || payload == nil || !approvals["confirm-1"].Approved {
		t.Fatalf("unexpected HITL payload: %#v", payload)
	}
}

func TestHitlExtensionDecisionDetails(t *testing.T) {
	message := hitlDecisionMessage(map[string]any{
		"type": HITLTypeToolApprovalResponse,
		"approvals": []any{
			map[string]any{"id": "confirm-1", "approved": true},
			map[string]any{"id": "confirm-2", "approved": false, "rejection_reason": "unsafe"},
		},
	})
	got, err := ExtractApprovalResults(message)
	if err != nil || !got["confirm-1"].Approved || got["confirm-2"].Approved || got["confirm-2"].RejectionReason != "unsafe" {
		t.Fatalf("approval results = %#v, err = %v", got, err)
	}
}

func TestHitlExtensionDoesNotReadLegacyDataPart(t *testing.T) {
	legacy := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewDataPart(map[string]any{"decision_type": "approve"}))
	if IsHITLResponse(legacy) {
		t.Fatal("legacy DataPart was treated as a HITL response")
	}
}

func TestHitlToolMapNormalizesNilArgs(t *testing.T) {
	tool := hitlToolMap(OriginalFunctionCall{ID: "call-1", Name: "get_cluster"}, "approval-1")
	args, ok := tool["args"].(map[string]any)
	if !ok || args == nil || len(args) != 0 {
		t.Fatalf("args = %#v, want an empty object", tool["args"])
	}
}

func TestHITLActivationInterceptor(t *testing.T) {
	ctx, callCtx := a2asrv.NewCallContext(context.Background(), a2asrv.NewServiceParams(map[string][]string{
		a2atype.SvcParamExtensions: {HITLExtensionURI},
	}))
	if HitlActivated(ctx) {
		t.Fatal("HITL active before interceptor")
	}
	if _, _, err := HITLActivationInterceptor().Before(ctx, callCtx, &a2asrv.Request{}); err != nil {
		t.Fatalf("Before() error = %v", err)
	}
	if !HitlActivated(ctx) {
		t.Fatal("HITL was not activated")
	}
	if got := callCtx.Extensions().ActivatedURIs(); len(got) != 1 || got[0] != HITLExtensionURI {
		t.Fatalf("activated extensions = %#v", got)
	}
}

func TestBuildHITLStatusMessage(t *testing.T) {
	t.Run("tool approval", func(t *testing.T) {
		internal := a2atype.NewMessage(a2atype.MessageRoleAgent,
			confirmationPart("confirm-1", "delete_file", "call-1", map[string]any{"path": "/tmp/x"}, nil))
		public := BuildHITLStatusMessage(internal, true)
		payload := GetHitlPayload(public)
		if payload == nil || payload["type"] != HITLTypeToolApprovalRequest {
			t.Fatalf("payload = %#v", payload)
		}
		if len(public.Parts) != 1 || public.Parts[0].Text() != "Please confirm" {
			t.Fatalf("public parts = %#v, want text only", public.Parts)
		}
		tools := anySlice(payload["tools"])
		if len(tools) != 1 || tools[0].(map[string]any)["name"] != "delete_file" || tools[0].(map[string]any)["id"] != "confirm-1" {
			t.Fatalf("tools = %#v", tools)
		}
	})

	t.Run("ask user", func(t *testing.T) {
		questions := []any{map[string]any{"question": "Which database?", "choices": []any{"PostgreSQL", "MySQL"}}}
		internal := a2atype.NewMessage(a2atype.MessageRoleAgent,
			confirmationPart("confirm-2", "ask_user", "call-2", map[string]any{"questions": questions}, nil))
		payload := GetHitlPayload(BuildHITLStatusMessage(internal, true))
		if payload == nil || payload["type"] != HITLTypeAskUserRequest || len(anySlice(payload["questions"])) != 1 {
			t.Fatalf("payload = %#v", payload)
		}
	})

	t.Run("nested subagent", func(t *testing.T) {
		child := HitlPartInfo{Name: toolconfirmation.FunctionCallName, ID: "child-confirm", OriginalFunctionCall: OriginalFunctionCall{
			Name: "delete_pod", ID: "child-call", Args: map[string]any{"name": "api"},
		}}
		internal := a2atype.NewMessage(a2atype.MessageRoleAgent,
			confirmationPart("parent-confirm", "k8s_agent", "parent-call", nil, HitlConfirmationPayload{
				TaskID: "child-task", ContextID: "child-context", SubagentName: "k8s_agent", HitlParts: []HitlPartInfo{child},
			}.ToMap()))
		payload := GetHitlPayload(BuildHITLStatusMessage(internal, true))
		nested, _ := payload["nested"].(map[string]any)
		tools := anySlice(nested["tools"])
		if nested["task_id"] != "child-task" || len(tools) != 1 || tools[0].(map[string]any)["call_id"] != "child-call" || tools[0].(map[string]any)["id"] != "child-confirm" {
			t.Fatalf("nested = %#v", nested)
		}
	})

	t.Run("nested ask user", func(t *testing.T) {
		questions := []any{map[string]any{"question": "Which namespace?"}}
		child := HitlPartInfo{Name: toolconfirmation.FunctionCallName, ID: "child-confirm", OriginalFunctionCall: OriginalFunctionCall{
			Name: "ask_user", ID: "child-call", Args: map[string]any{"questions": questions},
		}}
		internal := a2atype.NewMessage(a2atype.MessageRoleAgent,
			confirmationPart("parent-confirm", "k8s_agent", "parent-call", nil, HitlConfirmationPayload{
				TaskID: "child-task", ContextID: "child-context", SubagentName: "k8s_agent", HitlParts: []HitlPartInfo{child},
			}.ToMap()))
		payload := GetHitlPayload(BuildHITLStatusMessage(internal, true))
		if payload["type"] != HITLTypeAskUserRequest || len(anySlice(payload["questions"])) != 1 || payload["nested"] == nil {
			t.Fatalf("payload = %#v", payload)
		}
	})

	t.Run("not activated", func(t *testing.T) {
		internal := a2atype.NewMessage(a2atype.MessageRoleAgent,
			confirmationPart("confirm-1", "delete_file", "call-1", nil, nil))
		public := BuildHITLStatusMessage(internal, false)
		if GetHitlPayload(public) != nil || len(public.Parts) != 1 || public.Parts[0].Text() == "" {
			t.Fatalf("public message = %#v", public)
		}
	})
}

func TestBuildResumeHITLMessageAskUser(t *testing.T) {
	incoming := hitlDecisionMessage(map[string]any{
		"type":    HITLTypeAskUserResponse,
		"id":      "confirm-1",
		"answers": []any{map[string]any{"answer": []any{"PostgreSQL"}}},
	})
	stored := &a2atype.Task{Status: a2atype.TaskStatus{
		State: a2atype.TaskStateInputRequired,
		Message: AttachHitlExtension(a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("Answer required")), map[string]any{
			"type": HITLTypeAskUserRequest, "id": "confirm-1",
		}),
	}}
	resume, err := BuildResumeHITLMessage(stored, incoming)
	if err != nil {
		t.Fatalf("BuildResumeHITLMessage() error = %v", err)
	}
	if resume == nil || len(resume.Parts) != 1 {
		t.Fatalf("resume = %#v", resume)
	}
	response := asDataPart(resume.Parts[0])[PartKeyResponse].(map[string]any)["response"].(string)
	var confirmation toolconfirmation.ToolConfirmation
	if err := json.Unmarshal([]byte(response), &confirmation); err != nil {
		t.Fatalf("confirmation JSON: %v", err)
	}
	payload, _ := confirmation.Payload.(map[string]any)
	if !confirmation.Confirmed || len(anySlice(payload["answers"])) != 1 {
		t.Fatalf("confirmation = %#v", confirmation)
	}
}

func TestBuildResumeHITLMessageBatchFlattensApprovals(t *testing.T) {
	stored := &a2atype.Task{Status: a2atype.TaskStatus{
		State: a2atype.TaskStateInputRequired,
		Message: AttachHitlExtension(a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("Approval required")), map[string]any{
			"type": HITLTypeToolApprovalRequest,
			"tools": []any{
				map[string]any{"id": "confirm-1", "call_id": "call-1", "name": "delete_file", "args": map[string]any{}},
				map[string]any{"id": "confirm-2", "call_id": "call-2", "name": "restart_pod", "args": map[string]any{}},
			},
		}),
	}}
	incoming := hitlDecisionMessage(map[string]any{
		"type": HITLTypeToolApprovalResponse,
		"approvals": []any{
			map[string]any{"id": "confirm-1", "approved": true},
			map[string]any{"id": "confirm-2", "approved": false, "rejection_reason": "not now"},
		},
	})
	resume, err := BuildResumeHITLMessage(stored, incoming)
	if err != nil {
		t.Fatalf("BuildResumeHITLMessage() error = %v", err)
	}
	if len(resume.Parts) != 2 {
		t.Fatalf("resume parts = %d, want 2", len(resume.Parts))
	}
	for index, wantID := range []string{"confirm-1", "confirm-2"} {
		if got := asDataPart(resume.Parts[index])[PartKeyID]; got != wantID {
			t.Fatalf("part %d id = %#v, want %q", index, got, wantID)
		}
	}
}

func TestBuildResumeHITLMessageNestedAskUser(t *testing.T) {
	stored := &a2atype.Task{Status: a2atype.TaskStatus{
		State: a2atype.TaskStateInputRequired,
		Message: AttachHitlExtension(a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("Answer required")), map[string]any{
			"type": HITLTypeAskUserRequest, "id": "parent-confirm",
			"nested": map[string]any{
				"task_id": "child-task", "context_id": "child-context", "subagent_name": "child",
				"tools": []any{map[string]any{
					"id": "child-confirm", "call_id": "child-call", "name": "ask_user",
					"args": map[string]any{"questions": []any{map[string]any{"question": "Which namespace?"}}},
				}},
			},
		}),
	}}
	incoming := hitlDecisionMessage(map[string]any{
		"type":    HITLTypeAskUserResponse,
		"id":      "child-confirm",
		"answers": []any{map[string]any{"answer": []any{"default"}}},
	})
	resume, err := BuildResumeHITLMessage(stored, incoming)
	if err != nil {
		t.Fatalf("BuildResumeHITLMessage() error = %v", err)
	}
	if got := asDataPart(resume.Parts[0])[PartKeyID]; got != "parent-confirm" {
		t.Fatalf("parent response id = %#v", got)
	}
	response := asDataPart(resume.Parts[0])[PartKeyResponse].(map[string]any)["response"].(string)
	var confirmation toolconfirmation.ToolConfirmation
	if err := json.Unmarshal([]byte(response), &confirmation); err != nil {
		t.Fatalf("confirmation JSON: %v", err)
	}
	payload, _ := confirmation.Payload.(map[string]any)
	if payload["task_id"] != "child-task" || len(anySlice(payload["answers"])) != 1 || len(anySlice(payload["hitl_parts"])) != 1 {
		t.Fatalf("nested confirmation payload = %#v", payload)
	}
}

func TestBuildResumeHITLMessageNestedApprovals(t *testing.T) {
	stored := &a2atype.Task{Status: a2atype.TaskStatus{
		State: a2atype.TaskStateInputRequired,
		Message: AttachHitlExtension(a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("Approval required")), map[string]any{
			"type":  HITLTypeToolApprovalRequest,
			"tools": []any{map[string]any{"id": "parent-confirm", "call_id": "parent-call", "name": "child", "args": map[string]any{}}},
			"nested": map[string]any{
				"task_id": "child-task", "context_id": "child-context", "subagent_name": "child",
				"tools": []any{
					map[string]any{"id": "child-confirm-1", "call_id": "child-call-1", "name": "delete_pod", "args": map[string]any{}},
					map[string]any{"id": "child-confirm-2", "call_id": "child-call-2", "name": "restart_pod", "args": map[string]any{}},
				},
			},
		}),
	}}
	incoming := hitlDecisionMessage(map[string]any{
		"type": HITLTypeToolApprovalResponse,
		"approvals": []any{
			map[string]any{"id": "child-confirm-1", "approved": true},
			map[string]any{"id": "child-confirm-2", "approved": false, "rejection_reason": "not now"},
		},
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
	payload, _ := confirmation.Payload.(map[string]any)
	approvals := anySlice(payload["approvals"])
	if confirmation.Confirmed || len(approvals) != 2 || approvals[1].(map[string]any)["rejection_reason"] != "not now" {
		t.Fatalf("nested confirmation = %#v", confirmation)
	}
}
