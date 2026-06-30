package a2a

import (
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
)

// TestNewAgentMessage_StampsContextAndTaskID verifies agent messages carry the
// request's context and task ids. A2A allows omitting them (the task is the
// canonical carrier), but stamping them lets consumers that flatten task.history
// key each message to its task without backfilling.
func TestNewAgentMessage_StampsContextAndTaskID(t *testing.T) {
	reqCtx := &a2asrv.RequestContext{
		ContextID: "ctx-xyz",
		TaskID:    a2atype.TaskID("task-xyz"),
	}

	msg := newAgentMessage(reqCtx, a2atype.TextPart{Text: "hello"})

	if msg.ContextID != "ctx-xyz" {
		t.Errorf("ContextID = %q, want %q", msg.ContextID, "ctx-xyz")
	}
	if msg.TaskID != a2atype.TaskID("task-xyz") {
		t.Errorf("TaskID = %q, want %q", msg.TaskID, a2atype.TaskID("task-xyz"))
	}
	if msg.Role != a2atype.MessageRoleAgent {
		t.Errorf("Role = %q, want %q", msg.Role, a2atype.MessageRoleAgent)
	}
}

// TestNewAgentStatusEvent_MessageCarriesIDs verifies the per-event emission seam:
// the working status event the executor writes (and that is persisted into
// task.history) carries an agent message stamped with the request's context/task
// ids. This is the property the send guard depends on — without it the persisted
// message keys differently from its locally-streamed counterpart and falsely
// blocks the next send. Mirrors the Python converter test.
func TestNewAgentStatusEvent_MessageCarriesIDs(t *testing.T) {
	reqCtx := &a2asrv.RequestContext{
		ContextID: "ctx-xyz",
		TaskID:    a2atype.TaskID("task-xyz"),
	}
	meta := map[string]any{"k": "v"}

	ev := newAgentStatusEvent(reqCtx, a2atype.ContentParts{a2atype.TextPart{Text: "hi"}}, meta)

	if ev.Status.State != a2atype.TaskStateWorking {
		t.Errorf("State = %q, want %q", ev.Status.State, a2atype.TaskStateWorking)
	}
	if ev.Status.Message == nil {
		t.Fatal("status message is nil")
	}
	if ev.Status.Message.ContextID != "ctx-xyz" {
		t.Errorf("message ContextID = %q, want %q", ev.Status.Message.ContextID, "ctx-xyz")
	}
	if ev.Status.Message.TaskID != a2atype.TaskID("task-xyz") {
		t.Errorf("message TaskID = %q, want %q", ev.Status.Message.TaskID, a2atype.TaskID("task-xyz"))
	}
	// The event itself also carries the ids (from reqCtx), matching the message.
	if ev.ContextID != "ctx-xyz" {
		t.Errorf("event ContextID = %q, want %q", ev.ContextID, "ctx-xyz")
	}
	if ev.TaskID != a2atype.TaskID("task-xyz") {
		t.Errorf("event TaskID = %q, want %q", ev.TaskID, a2atype.TaskID("task-xyz"))
	}
}
