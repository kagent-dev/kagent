package a2a

import (
	"testing"
	"unicode/utf8"

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

// TestExtractSessionName verifies session names are truncated on rune boundaries.
// A byte-wise cut can split a multi-byte UTF-8 rune and produce an invalid-UTF-8
// name, which the Postgres session store rejects on session create. The result
// must always be valid UTF-8, and pure-ASCII behavior must be unchanged.
func TestExtractSessionName(t *testing.T) {
	textMsg := func(s string) *a2atype.Message {
		return &a2atype.Message{Parts: []a2atype.Part{a2atype.TextPart{Text: s}}}
	}

	tests := []struct {
		name string
		msg  *a2atype.Message
		want string
	}{
		{name: "nil message", msg: nil, want: ""},
		{name: "no parts", msg: &a2atype.Message{}, want: ""},
		{name: "short ascii", msg: textMsg("hello"), want: "hello"},
		{
			name: "ascii at limit is not truncated",
			msg:  textMsg("01234567890123456789"), // exactly 20 runes
			want: "01234567890123456789",
		},
		{
			name: "long ascii is truncated with ellipsis",
			msg:  textMsg("012345678901234567890"), // 21 runes
			want: "01234567890123456789...",
		},
		{
			// 7 CJK runes = 21 bytes: byte-slicing at 20 splits the 7th rune.
			name: "multibyte over byte limit stays valid utf8",
			msg:  textMsg("こんにちは世界"),
			want: "こんにちは世界",
		},
		{
			name: "long multibyte truncated on rune boundary",
			msg:  textMsg("あいうえおかきくけこさしすせそたちつてとな"), // 21 runes
			want: "あいうえおかきくけこさしすせそたちつてと...",
		},
		{
			name: "skips empty text part and uses first non-empty",
			msg: &a2atype.Message{Parts: []a2atype.Part{
				a2atype.TextPart{Text: ""},
				a2atype.TextPart{Text: "hi"},
			}},
			want: "hi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSessionName(tt.msg)
			if got != tt.want {
				t.Errorf("extractSessionName() = %q, want %q", got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("extractSessionName() returned invalid UTF-8: %q", got)
			}
		})
	}
}
