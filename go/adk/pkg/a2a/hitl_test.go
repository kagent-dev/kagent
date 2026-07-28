package a2a

import (
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

func dataPart(data map[string]any, metadata map[string]any) *a2atype.Part {
	p := a2atype.NewDataPart(data)
	if metadata != nil {
		p.Metadata = metadata
	}
	return p
}

// ---------------------------------------------------------------------------
// ExtractDecisionFromMessage
// ---------------------------------------------------------------------------

func TestExtractDecisionFromMessage_DataPart(t *testing.T) {
	approveData := map[string]any{KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeApprove}
	msg := a2atype.NewMessage(a2atype.MessageRoleUser, dataPart(approveData, nil))
	if got := ExtractDecisionFromMessage(msg); got != DecisionApprove {
		t.Errorf("approve DataPart = %q, want %q", got, DecisionApprove)
	}

	rejectData := map[string]any{KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeReject}
	msg = a2atype.NewMessage(a2atype.MessageRoleUser, dataPart(rejectData, nil))
	if got := ExtractDecisionFromMessage(msg); got != DecisionReject {
		t.Errorf("reject DataPart = %q, want %q", got, DecisionReject)
	}

	batchData := map[string]any{KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeBatch}
	msg = a2atype.NewMessage(a2atype.MessageRoleUser, dataPart(batchData, nil))
	if got := ExtractDecisionFromMessage(msg); got != DecisionBatch {
		t.Errorf("batch DataPart = %q, want %q", got, DecisionBatch)
	}
}

func TestExtractDecisionFromMessage_EdgeCases(t *testing.T) {
	if got := ExtractDecisionFromMessage(nil); got != "" {
		t.Errorf("nil message = %q, want empty", got)
	}
	msg := a2atype.NewMessage(a2atype.MessageRoleUser)
	if got := ExtractDecisionFromMessage(msg); got != "" {
		t.Errorf("empty parts = %q, want empty", got)
	}
	// Text-only message — no decision (text extraction removed)
	msg = a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("approve"))
	if got := ExtractDecisionFromMessage(msg); got != "" {
		t.Errorf("text-only message = %q, want empty (text extraction removed)", got)
	}
	// Unknown decision type
	msg = a2atype.NewMessage(a2atype.MessageRoleUser,
		dataPart(map[string]any{KAgentHitlDecisionTypeKey: "unknown"}, nil))
	if got := ExtractDecisionFromMessage(msg); got != "" {
		t.Errorf("unknown decision = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// ReadMetadataValue
// ---------------------------------------------------------------------------

func TestReadMetadataValue(t *testing.T) {
	tests := []struct {
		name      string
		metadata  map[string]any
		key       string
		wantValue any
		wantFound bool
	}{
		{
			name:      "adk_ prefix takes priority",
			metadata:  map[string]any{"adk_type": "adk_val", "kagent_type": "kagent_val"},
			key:       "type",
			wantValue: "adk_val",
			wantFound: true,
		},
		{
			name:      "kagent_ fallback when adk_ missing",
			metadata:  map[string]any{"kagent_type": "kagent_val"},
			key:       "type",
			wantValue: "kagent_val",
			wantFound: true,
		},
		{
			name:      "nil metadata returns not found",
			metadata:  nil,
			key:       "type",
			wantFound: false,
		},
		{
			name:      "missing key returns not found",
			metadata:  map[string]any{"other_key": "val"},
			key:       "type",
			wantFound: false,
		},
		{
			name:      "bool value",
			metadata:  map[string]any{"adk_is_long_running": true},
			key:       "is_long_running",
			wantValue: true,
			wantFound: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVal, gotFound := ReadMetadataValue(tt.metadata, tt.key)
			if gotFound != tt.wantFound {
				t.Errorf("found = %v, want %v", gotFound, tt.wantFound)
			}
			if gotFound && gotVal != tt.wantValue {
				t.Errorf("value = %v, want %v", gotVal, tt.wantValue)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ExtractBatchDecisionsFromMessage
// ---------------------------------------------------------------------------

func TestExtractBatchDecisionsFromMessage(t *testing.T) {
	tests := []struct {
		name    string
		message *a2atype.Message
		want    map[string]DecisionType
	}{
		{name: "nil message", message: nil, want: nil},
		{
			name: "valid batch",
			message: a2atype.NewMessage(a2atype.MessageRoleUser, dataPart(map[string]any{
				KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeBatch,
				KAgentHitlDecisionsKey:    map[string]any{"call_1": "approve", "call_2": "reject"},
			}, nil)),
			want: map[string]DecisionType{"call_1": DecisionApprove, "call_2": DecisionReject},
		},
		{
			name: "invalid values filtered",
			message: a2atype.NewMessage(a2atype.MessageRoleUser, dataPart(map[string]any{
				KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeBatch,
				KAgentHitlDecisionsKey:    map[string]any{"call_1": "approve", "call_2": "bad"},
			}, nil)),
			want: map[string]DecisionType{"call_1": DecisionApprove},
		},
		{
			name: "non-batch type returns nil",
			message: a2atype.NewMessage(a2atype.MessageRoleUser, dataPart(map[string]any{
				KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeApprove,
			}, nil)),
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractBatchDecisionsFromMessage(tt.message)
			if len(got) != len(tt.want) {
				t.Errorf("len = %d, want %d", len(got), len(tt.want))
				return
			}
			for k, wantV := range tt.want {
				if gotV, ok := got[k]; !ok || gotV != wantV {
					t.Errorf("[%q] = %q, want %q", k, gotV, wantV)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ExtractRejectionReasonsFromMessage
// ---------------------------------------------------------------------------

func TestExtractRejectionReasonsFromMessage(t *testing.T) {
	tests := []struct {
		name    string
		message *a2atype.Message
		want    map[string]string
	}{
		{name: "nil message", message: nil, want: nil},
		{
			name: "uniform reject with reason",
			message: a2atype.NewMessage(a2atype.MessageRoleUser, dataPart(map[string]any{
				KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeReject,
				"rejection_reason":        "too dangerous",
			}, nil)),
			want: map[string]string{"*": "too dangerous"},
		},
		{
			name: "uniform reject without reason returns nil",
			message: a2atype.NewMessage(a2atype.MessageRoleUser, dataPart(map[string]any{
				KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeReject,
			}, nil)),
			want: nil,
		},
		{
			name: "batch with reasons",
			message: a2atype.NewMessage(a2atype.MessageRoleUser, dataPart(map[string]any{
				KAgentHitlDecisionTypeKey:     KAgentHitlDecisionTypeBatch,
				KAgentHitlRejectionReasonsKey: map[string]any{"call_1": "policy violation"},
			}, nil)),
			want: map[string]string{"call_1": "policy violation"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractRejectionReasonsFromMessage(tt.message)
			if len(got) != len(tt.want) {
				t.Errorf("len = %d, want %d", len(got), len(tt.want))
				return
			}
			for k, wantV := range tt.want {
				if gotV, ok := got[k]; !ok || gotV != wantV {
					t.Errorf("[%q] = %q, want %q", k, gotV, wantV)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ExtractAskUserAnswersFromMessage
// ---------------------------------------------------------------------------

func TestExtractAskUserAnswersFromMessage(t *testing.T) {
	msg := a2atype.NewMessage(a2atype.MessageRoleUser, dataPart(map[string]any{
		KAgentAskUserAnswersKey: []any{map[string]any{"answer": []any{"yes"}}},
	}, nil))
	got := ExtractAskUserAnswersFromMessage(msg)
	if len(got) != 1 {
		t.Errorf("len = %d, want 1", len(got))
	}

	// Non-list value returns nil
	msg = a2atype.NewMessage(a2atype.MessageRoleUser, dataPart(map[string]any{
		KAgentAskUserAnswersKey: "not a list",
	}, nil))
	if got := ExtractAskUserAnswersFromMessage(msg); got != nil {
		t.Errorf("non-list = %v, want nil", got)
	}

	// Missing key returns nil
	if got := ExtractAskUserAnswersFromMessage(nil); got != nil {
		t.Errorf("nil message = %v, want nil", got)
	}
}

// ---------------------------------------------------------------------------
// HitlPartInfoFromDataPartData
// ---------------------------------------------------------------------------

func TestHitlPartInfoFromDataPartData(t *testing.T) {
	data := map[string]any{
		"name": "adk_request_confirmation",
		"id":   "confirm_1",
		"args": map[string]any{
			"originalFunctionCall": map[string]any{
				"name": "delete_file",
				"args": map[string]any{"path": "/tmp"},
				"id":   "orig_1",
			},
		},
	}
	info := HitlPartInfoFromDataPartData(data)
	if info.Name != "adk_request_confirmation" {
		t.Errorf("Name = %q", info.Name)
	}
	if info.ID != "confirm_1" {
		t.Errorf("ID = %q", info.ID)
	}
	if info.OriginalFunctionCall.Name != "delete_file" {
		t.Errorf("OriginalFunctionCall.Name = %q", info.OriginalFunctionCall.Name)
	}
	if info.OriginalFunctionCall.ID != "orig_1" {
		t.Errorf("OriginalFunctionCall.ID = %q", info.OriginalFunctionCall.ID)
	}
}

// ---------------------------------------------------------------------------
// ParseHitlConfirmationPayload
// ---------------------------------------------------------------------------

func TestParseHitlConfirmationPayload(t *testing.T) {
	raw := map[string]any{
		"task_id":       "task-1",
		"context_id":    "ctx-1",
		"subagent_name": "k8s_agent",
		"hitl_parts": []any{
			map[string]any{
				"name": "adk_request_confirmation",
				"id":   "confirm-1",
				"originalFunctionCall": map[string]any{
					"name": "delete_file",
					"args": map[string]any{"path": "/tmp/x"},
					"id":   "call-1",
				},
			},
		},
		"batch_decisions": map[string]any{
			"call-1": "approve",
			"call-2": "reject",
		},
		"rejection_reasons": map[string]any{
			"call-2": "Too broad",
		},
		"rejection_reason": "Too broad",
		"answers": []any{
			map[string]any{"answer": []any{"PostgreSQL"}},
		},
	}

	payload := ParseHitlConfirmationPayload(raw)
	if payload.TaskID != "task-1" || payload.ContextID != "ctx-1" || payload.SubagentName != "k8s_agent" {
		t.Fatalf("unexpected base payload fields: %#v", payload)
	}
	if !payload.HasSubagentHitl() || len(payload.HitlParts) != 1 {
		t.Fatalf("expected one subagent hitl part, got %#v", payload.HitlParts)
	}
	if payload.HitlParts[0].OriginalFunctionCall.Name != "delete_file" {
		t.Fatalf("unexpected original function call: %#v", payload.HitlParts[0].OriginalFunctionCall)
	}
	if payload.BatchDecisions["call-1"] != DecisionApprove || payload.BatchDecisions["call-2"] != DecisionReject {
		t.Fatalf("unexpected batch decisions: %#v", payload.BatchDecisions)
	}
	if payload.RejectionReasons["call-2"] != "Too broad" {
		t.Fatalf("unexpected rejection reasons: %#v", payload.RejectionReasons)
	}
	if payload.RejectionReason != "Too broad" {
		t.Fatalf("unexpected rejection reason: %q", payload.RejectionReason)
	}
	if len(payload.Answers) != 1 || len(payload.Answers[0].Answer) != 1 || payload.Answers[0].Answer[0] != "PostgreSQL" {
		t.Fatalf("unexpected answers: %#v", payload.Answers)
	}

	roundTripped := payload.ToMap()
	if roundTripped["task_id"] != "task-1" {
		t.Fatalf("round-tripped task_id = %#v", roundTripped["task_id"])
	}
}

// ---------------------------------------------------------------------------
// BuildConfirmationPayload
// ---------------------------------------------------------------------------

func TestBuildConfirmationPayload(t *testing.T) {
	if got := BuildConfirmationPayload(nil, nil); got != nil {
		t.Errorf("nil+nil = %v, want nil", got)
	}
	got := BuildConfirmationPayload(map[string]any{"a": 1}, map[string]any{"b": 2})
	if got["a"] != 1 || got["b"] != 2 {
		t.Errorf("merge = %v", got)
	}
	// Extra overwrites original
	got = BuildConfirmationPayload(map[string]any{"k": "orig"}, map[string]any{"k": "new"})
	if got["k"] != "new" {
		t.Errorf("overwrite: k = %v, want new", got["k"])
	}
}

// ---------------------------------------------------------------------------
// ExtractPendingConfirmationsFromParts
// ---------------------------------------------------------------------------

func TestExtractPendingConfirmationsFromParts(t *testing.T) {
	parts := a2atype.ContentParts{
		dataPart(
			map[string]any{
				"name": "adk_request_confirmation",
				"id":   "confirm_1",
				"args": map[string]any{
					"originalFunctionCall": map[string]any{
						"name": "delete_file",
						"args": map[string]any{"path": "/tmp/x"},
						"id":   "call_1",
					},
					"toolConfirmation": map[string]any{
						"hint":      "Approve?",
						"confirmed": false,
						"payload": map[string]any{
							"task_id":       "subtask_1",
							"context_id":    "subctx_1",
							"subagent_name": "k8s_agent",
						},
					},
				},
			},
			map[string]any{
				"kagent_type":            "function_call",
				"kagent_is_long_running": true,
			},
		),
	}

	pending := ExtractPendingConfirmationsFromParts(parts)
	if len(pending) != 1 {
		t.Fatalf("ExtractPendingConfirmationsFromParts() len = %d, want 1", len(pending))
	}

	pc, ok := pending["confirm_1"]
	if !ok {
		t.Fatalf("pending confirmation confirm_1 missing: %#v", pending)
	}
	if pc.OriginalID != "call_1" {
		t.Fatalf("OriginalID = %q, want call_1", pc.OriginalID)
	}
	if pc.OriginalPayload["task_id"] != "subtask_1" {
		t.Fatalf("OriginalPayload = %#v", pc.OriginalPayload)
	}
}

// ---------------------------------------------------------------------------
// ExtractHitlInfoFromParts
// ---------------------------------------------------------------------------

func TestExtractHitlInfoFromParts_PointerDataPart(t *testing.T) {
	parts := a2atype.ContentParts{
		dataPart(
			map[string]any{
				"name": "adk_request_confirmation",
				"id":   "confirm_1",
				"args": map[string]any{
					"originalFunctionCall": map[string]any{
						"name": "delete_file",
						"args": map[string]any{"path": "/tmp/x"},
						"id":   "call_1",
					},
				},
			},
			map[string]any{
				"kagent_type":            "function_call",
				"kagent_is_long_running": true,
			},
		),
	}

	got := ExtractHitlInfoFromParts(parts)
	if len(got) != 1 {
		t.Fatalf("ExtractHitlInfoFromParts() len = %d, want 1", len(got))
	}
	if got[0].OriginalFunctionCall.Name != "delete_file" {
		t.Fatalf("tool name = %q, want delete_file", got[0].OriginalFunctionCall.Name)
	}
}

// ---------------------------------------------------------------------------
// BuildResumeHITLMessage
// ---------------------------------------------------------------------------

func TestBuildResumeHITLMessage(t *testing.T) {
	storedTask := &a2atype.Task{
		ID:        "task_1",
		ContextID: "ctx_1",
		Status: a2atype.TaskStatus{
			State: a2atype.TaskStateInputRequired,
			Message: a2atype.NewMessage(
				a2atype.MessageRoleAgent,
				dataPart(
					map[string]any{
						"name": "adk_request_confirmation",
						"id":   "confirm_1",
						"args": map[string]any{
							"originalFunctionCall": map[string]any{
								"name": "delete_file",
								"args": map[string]any{"path": "/tmp/x"},
								"id":   "call_1",
							},
							"toolConfirmation": map[string]any{
								"hint":      "Approve?",
								"confirmed": false,
							},
						},
					},
					map[string]any{
						"kagent_type":            "function_call",
						"kagent_is_long_running": true,
					},
				),
			),
		},
	}

	incoming := a2atype.NewMessage(
		a2atype.MessageRoleUser,
		dataPart(map[string]any{KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeApprove}, nil),
	)

	resume := BuildResumeHITLMessage(storedTask, incoming)
	if resume == nil {
		t.Fatal("BuildResumeHITLMessage() returned nil")
		return
	}
	if len(resume.Parts) != 1 {
		t.Fatalf("resume parts len = %d, want 1", len(resume.Parts))
	}
	dp := asDataPart(resume.Parts[0])
	if dp == nil {
		t.Fatal("resume part is not a DataPart")
		return
	}
	if dp[PartKeyName] != "adk_request_confirmation" {
		t.Fatalf("resume FunctionResponse name = %#v", dp[PartKeyName])
	}
	if dp[PartKeyID] != "confirm_1" {
		t.Fatalf("resume FunctionResponse id = %#v", dp[PartKeyID])
	}
}

// ---------------------------------------------------------------------------
// ProcessHitlDecision
// ---------------------------------------------------------------------------

func TestProcessHitlDecision(t *testing.T) {
	pending := map[string]PendingConfirmation{
		"fc_1": {OriginalID: "orig_1"},
	}

	t.Run("uniform approve", func(t *testing.T) {
		msg := a2atype.NewMessage(a2atype.MessageRoleUser,
			dataPart(map[string]any{KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeApprove}, nil))
		parts := ProcessHitlDecision(pending, DecisionApprove, msg)
		if len(parts) != 1 {
			t.Fatalf("len = %d, want 1", len(parts))
		}
		dp := asDataPart(parts[0])
		if dp == nil {
			t.Fatal("part is not DataPart")
			return
		}
		if dp[PartKeyName] != "adk_request_confirmation" {
			t.Errorf("name = %v", dp[PartKeyName])
		}
	})

	t.Run("uniform reject with reason", func(t *testing.T) {
		msg := a2atype.NewMessage(a2atype.MessageRoleUser,
			dataPart(map[string]any{
				KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeReject,
				"rejection_reason":        "not safe",
			}, nil))
		parts := ProcessHitlDecision(pending, DecisionReject, msg)
		if len(parts) != 1 {
			t.Fatalf("len = %d, want 1", len(parts))
		}
	})

	t.Run("empty pending returns nil", func(t *testing.T) {
		msg := a2atype.NewMessage(a2atype.MessageRoleUser,
			dataPart(map[string]any{KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeApprove}, nil))
		if parts := ProcessHitlDecision(map[string]PendingConfirmation{}, DecisionApprove, msg); parts != nil {
			t.Errorf("empty pending = %v, want nil", parts)
		}
	})

	t.Run("ask-user answers take priority", func(t *testing.T) {
		msg := a2atype.NewMessage(a2atype.MessageRoleUser,
			dataPart(map[string]any{
				KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeApprove,
				KAgentAskUserAnswersKey:   []any{map[string]any{"answer": []any{"yes"}}},
			}, nil))
		parts := ProcessHitlDecision(pending, DecisionApprove, msg)
		if len(parts) != 1 {
			t.Fatalf("ask-user len = %d, want 1", len(parts))
		}
	})

	t.Run("batch decisions", func(t *testing.T) {
		pendingBatch := map[string]PendingConfirmation{
			"fc_1": {OriginalID: "orig_1"},
			"fc_2": {OriginalID: "orig_2"},
		}
		msg := a2atype.NewMessage(a2atype.MessageRoleUser,
			dataPart(map[string]any{
				KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeBatch,
				KAgentHitlDecisionsKey:    map[string]any{"orig_1": "approve", "orig_2": "reject"},
			}, nil))
		parts := ProcessHitlDecision(pendingBatch, DecisionBatch, msg)
		if len(parts) != 2 {
			t.Fatalf("batch len = %d, want 2", len(parts))
		}
	})
}
