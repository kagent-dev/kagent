package tools

import (
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
)

// TestContextIDForCall covers session isolation: with isolateSessions the tool
// mints a fresh context_id (== sub-agent session id) per call; without it the
// stable per-tool id is reused so calls share one sub-agent session.
func TestContextIDForCall(t *testing.T) {
	t.Run("shared reuses the same id across calls", func(t *testing.T) {
		s := &remoteA2AState{lastContextID: "stable-ctx", isolateSessions: false}
		if got := s.contextIDForCall(); got != "stable-ctx" {
			t.Errorf("first call = %q, want stable-ctx", got)
		}
		if got := s.contextIDForCall(); got != "stable-ctx" {
			t.Errorf("second call = %q, want stable-ctx", got)
		}
	})

	t.Run("isolated mints a fresh non-empty id per call", func(t *testing.T) {
		s := &remoteA2AState{lastContextID: "stable-ctx", isolateSessions: true}
		first := s.contextIDForCall()
		second := s.contextIDForCall()
		if first == "" || second == "" {
			t.Fatalf("isolated ids must be non-empty, got %q and %q", first, second)
		}
		if first == second {
			t.Errorf("isolated calls must differ, both = %q", first)
		}
		if first == "stable-ctx" || second == "stable-ctx" {
			t.Errorf("isolated calls must not reuse lastContextID, got %q / %q", first, second)
		}
	})
}

// TestNewKAgentRemoteA2AToolStampID: isolated tools expose no stable pre-known
// session id (the per-call id rides the function_response instead), while shared
// tools return their stable id for function_call stamping.
func TestNewKAgentRemoteA2AToolStampID(t *testing.T) {
	_, shared, err := NewKAgentRemoteA2ATool(RemoteA2AToolConfig{Name: "t", Description: "d", BaseURL: "http://x"})
	if err != nil {
		t.Fatalf("shared tool: %v", err)
	}
	if shared == "" {
		t.Error("shared tool must return a non-empty stamp session id")
	}

	_, isolated, err := NewKAgentRemoteA2ATool(RemoteA2AToolConfig{Name: "t", Description: "d", BaseURL: "http://x", IsolateSessions: true})
	if err != nil {
		t.Fatalf("isolated tool: %v", err)
	}
	if isolated != "" {
		t.Errorf("isolated tool must return empty stamp session id, got %q", isolated)
	}
}

func taskWithState(state a2atype.TaskState, statusText string) *a2atype.Task {
	task := &a2atype.Task{ID: "task-1", ContextID: "child-ctx"}
	task.Status.State = state
	if statusText != "" {
		task.Status.Message = a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.TextPart{Text: statusText})
	}
	return task
}

func TestProcessResult_Completed(t *testing.T) {
	s := &remoteA2AState{name: "child"}
	task := taskWithState(a2atype.TaskStateCompleted, "")
	task.Artifacts = []*a2atype.Artifact{
		{Parts: a2atype.ContentParts{a2atype.TextPart{Text: "the answer"}}},
	}

	got, err := s.processResult(nil, task, "ctx-1")
	if err != nil {
		t.Fatalf("processResult() error = %v", err)
	}
	if got.Result != "the answer" {
		t.Errorf("result = %v, want %q", got.Result, "the answer")
	}
	if got.SubagentSessionID != "ctx-1" {
		t.Errorf("subagent_session_id = %v, want ctx-1", got.SubagentSessionID)
	}
	if got.SubagentTaskID != "task-1" {
		t.Errorf("subagent_task_id = %v, want task-1", got.SubagentTaskID)
	}
	if got.Error != "" {
		t.Error("completed task must not carry an error field")
	}
}

func TestProcessResult_TerminalFailures(t *testing.T) {
	tests := []struct {
		name          string
		state         a2atype.TaskState
		wantErrorType string
	}{
		{name: "failed", state: a2atype.TaskStateFailed, wantErrorType: "subagent_failed"},
		{name: "canceled", state: a2atype.TaskStateCanceled, wantErrorType: "subagent_canceled"},
		{name: "rejected", state: a2atype.TaskStateRejected, wantErrorType: "subagent_rejected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &remoteA2AState{name: "child"}
			got, err := s.processResult(nil, taskWithState(tt.state, "boom"), "ctx-1")
			if err != nil {
				t.Fatalf("processResult() error = %v", err)
			}
			if got.Error != "boom" {
				t.Errorf("error = %v, want boom", got.Error)
			}
			if got.ErrorType != tt.wantErrorType {
				t.Errorf("error_type = %v, want %v", got.ErrorType, tt.wantErrorType)
			}
			if got.SubagentTaskID != "task-1" {
				t.Errorf("subagent_task_id = %v, want task-1", got.SubagentTaskID)
			}
			if got.SubagentTaskState != string(tt.state) {
				t.Errorf("subagent_task_state = %v, want %v", got.SubagentTaskState, tt.state)
			}
			// Task.ContextID takes precedence for linkage when present.
			if got.SubagentSessionID != "child-ctx" {
				t.Errorf("subagent_session_id = %v, want child-ctx", got.SubagentSessionID)
			}
		})
	}
}

func TestProcessResult_NonTerminalIsNotSuccess(t *testing.T) {
	for _, state := range []a2atype.TaskState{
		a2atype.TaskStateWorking,
		a2atype.TaskStateSubmitted,
		a2atype.TaskStateAuthRequired,
	} {
		t.Run(string(state), func(t *testing.T) {
			s := &remoteA2AState{name: "child"}
			got, err := s.processResult(nil, taskWithState(state, ""), "ctx-1")
			if err != nil {
				t.Fatalf("processResult() error = %v", err)
			}
			if got.ErrorType != "subagent_non_terminal" {
				t.Errorf("error_type = %v, want subagent_non_terminal", got.ErrorType)
			}
			if got.SubagentSessionID != "ctx-1" {
				t.Errorf("subagent_session_id = %v, want ctx-1", got.SubagentSessionID)
			}
			if got.Result != "" {
				t.Error("non-terminal task must not be treated as success")
			}
		})
	}
}

func TestProcessResult_MessageAndNil(t *testing.T) {
	s := &remoteA2AState{name: "child"}
	msg := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.TextPart{Text: "direct"})
	got, err := s.processResult(nil, msg, "ctx-1")
	if err != nil {
		t.Fatalf("processResult(message) error = %v", err)
	}
	if got.Result != "direct" {
		t.Errorf("result = %v, want direct", got.Result)
	}
	if got.SubagentSessionID != "ctx-1" {
		t.Errorf("message subagent_session_id = %v, want ctx-1", got.SubagentSessionID)
	}

	got, err = s.processResult(nil, nil, "ctx-1")
	if err != nil {
		t.Fatalf("processResult(nil) error = %v", err)
	}
	if got.Error == "" {
		t.Error("nil result must produce an error")
	}
	if got.SubagentSessionID != "ctx-1" {
		t.Errorf("no-result subagent_session_id = %v, want ctx-1", got.SubagentSessionID)
	}
}

// TestHandleInputRequired_NilTaskReportsSession covers the early-return branch
// (no ctx interaction) and asserts the session id is reported.
func TestHandleInputRequired_NilTaskReportsSession(t *testing.T) {
	s := &remoteA2AState{name: "child"}
	got := s.handleInputRequired(nil, nil, "ctx-1")
	if got.Error == "" {
		t.Error("input_required without task must produce an error")
	}
	if got.SubagentSessionID != "ctx-1" {
		t.Errorf("subagent_session_id = %v, want ctx-1", got.SubagentSessionID)
	}
}

func TestFailureResult_StructuredPassthrough(t *testing.T) {
	t.Run("from data part, child value wins", func(t *testing.T) {
		s := &remoteA2AState{name: "child"}
		task := taskWithState(a2atype.TaskStateFailed, "deploy failed")
		task.Status.Message.Parts = append(task.Status.Message.Parts, a2atype.DataPart{
			Data: map[string]any{
				"error_type":       "helm_upgrade_failed",
				"error_step":       "upgrade",
				"error_message":    "release stuck",
				"remediation_hint": "rollback first",
				"unrelated":        "dropped",
			},
		})
		got := s.failureResult(task, "ctx-1")
		if got.ErrorType != "helm_upgrade_failed" {
			t.Errorf("error_type = %v, want helm_upgrade_failed", got.ErrorType)
		}
		if got.ErrorStep != "upgrade" || got.ErrorMessage != "release stuck" || got.RemediationHint != "rollback first" {
			t.Errorf("structured fields not passed through: %+v", got)
		}
	})

	t.Run("from fenced JSON failure text", func(t *testing.T) {
		s := &remoteA2AState{name: "child"}
		task := taskWithState(a2atype.TaskStateFailed,
			"The step failed:\n```json\n{\"error_type\":\"quota_exceeded\",\"remediation_hint\":\"increase quota\"}\n```")
		got := s.failureResult(task, "ctx-1")
		if got.ErrorType != "quota_exceeded" {
			t.Errorf("error_type = %v, want quota_exceeded", got.ErrorType)
		}
		if got.RemediationHint != "increase quota" {
			t.Errorf("remediation_hint = %v, want increase quota", got.RemediationHint)
		}
	})

	t.Run("plain text keeps default error_type", func(t *testing.T) {
		s := &remoteA2AState{name: "child"}
		got := s.failureResult(taskWithState(a2atype.TaskStateFailed, "plain failure"), "ctx-1")
		if got.ErrorType != "subagent_failed" {
			t.Errorf("error_type = %v, want subagent_failed", got.ErrorType)
		}
	})
}

func TestParseJSONObject(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "plain object", in: `{"error_type":"x"}`, want: true},
		{name: "fenced object", in: "```json\n{\"error_type\":\"x\"}\n```", want: true},
		{name: "fenced without lang", in: "```\n{\"error_type\":\"x\"}\n```", want: true},
		{name: "prose", in: "it broke", want: false},
		{name: "malformed json", in: "{not json", want: false},
		{name: "empty", in: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseJSONObject(tt.in); (got != nil) != tt.want {
				t.Errorf("parseJSONObject(%q) present = %v, want %v", tt.in, got != nil, tt.want)
			}
		})
	}
}
