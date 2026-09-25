package driver

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

type recordingSink struct {
	text     strings.Builder
	sessions []runtime.SessionStarted
	calls    []runtime.ToolCall
	results  []runtime.ToolResult
}

func (s *recordingSink) SessionStarted(event runtime.SessionStarted) error {
	s.sessions = append(s.sessions, event)
	return nil
}
func (s *recordingSink) TextDelta(event runtime.TextDelta) error {
	s.text.WriteString(event.Text)
	return nil
}
func (s *recordingSink) ToolCall(event runtime.ToolCall) error {
	s.calls = append(s.calls, event)
	return nil
}
func (s *recordingSink) ToolResult(event runtime.ToolResult) error {
	s.results = append(s.results, event)
	return nil
}

func TestTranslatePinnedNotifications(t *testing.T) {
	sink := &recordingSink{}
	messages := []string{
		`{"jsonrpc":"2.0","method":"item/agentMessage/delta","params":{"threadId":"thread","turnId":"turn","itemId":"message","delta":"hello"}}`,
		`{"jsonrpc":"2.0","method":"item/started","params":{"threadId":"thread","turnId":"turn","item":{"type":"commandExecution","id":"cmd","command":"pwd","commandActions":[],"cwd":"/data/workspace","status":"inProgress"}}}`,
		`{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thread","turnId":"turn","item":{"type":"commandExecution","id":"cmd","command":"pwd","commandActions":[],"cwd":"/data/workspace","aggregatedOutput":"/data/workspace","exitCode":0,"status":"completed"}}}`,
		`{"jsonrpc":"2.0","method":"item/started","params":{"threadId":"thread","turnId":"turn","item":{"type":"mcpToolCall","id":"mcp","server":"tools","tool":"lookup","arguments":{"query":"safe"},"status":"inProgress"}}}`,
		`{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thread","turnId":"turn","item":{"type":"mcpToolCall","id":"mcp","server":"tools","tool":"lookup","arguments":{"query":"safe"},"result":{"content":"ok"},"status":"completed"}}}`,
		`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread","turn":{"id":"turn","status":"completed"}}}`,
	}
	terminal := 0
	translator := newEventTranslator("thread", "turn")
	for _, raw := range messages {
		var message rpcMessage
		if err := json.Unmarshal([]byte(raw), &message); err != nil {
			t.Fatal(err)
		}
		_, done, err := translator.translate(message, sink)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			terminal++
		}
	}
	if sink.text.String() != "hello" || len(sink.calls) != 2 || len(sink.results) != 2 || terminal != 1 {
		t.Fatalf("sink = %#v, text %q, terminal %d", sink, sink.text.String(), terminal)
	}
	result, ok := sink.results[0].Result.(map[string]any)
	if !ok || result["exitCode"] != 0 {
		t.Fatalf("command result = %#v, want integer exitCode 0", sink.results[0].Result)
	}
	if sink.calls[1].Name != "tools.lookup" || sink.results[1].Name != "tools.lookup" {
		t.Fatalf("MCP events = %#v, %#v, want tools.lookup", sink.calls[1], sink.results[1])
	}
}

func TestApprovalToolCorrelatesActiveCall(t *testing.T) {
	translator := newEventTranslator("thread", "turn")
	sink := &recordingSink{}
	for _, raw := range []string{
		`{"jsonrpc":"2.0","method":"item/started","params":{"threadId":"thread","turnId":"turn","item":{"type":"mcpToolCall","id":"read","server":"protected","tool":"read","arguments":{"path":"source"},"status":"inProgress"}}}`,
		`{"jsonrpc":"2.0","method":"item/started","params":{"threadId":"thread","turnId":"turn","item":{"type":"mcpToolCall","id":"delete","server":"protected","tool":"delete","arguments":{"path":"target"},"status":"inProgress"}}}`,
	} {
		var message rpcMessage
		if err := json.Unmarshal([]byte(raw), &message); err != nil {
			t.Fatal(err)
		}
		if _, _, err := translator.translate(message, sink); err != nil {
			t.Fatal(err)
		}
	}

	id, name, err := translator.approvalTool("protected", "", map[string]any{"path": "target"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "delete" || name != "protected.delete" {
		t.Fatalf("approval tool = %q/%q, want delete/protected.delete", id, name)
	}
	if _, _, err := translator.approvalTool("protected", "", map[string]any{"path": "target"}); err == nil {
		t.Fatal("approvalTool() accepted a duplicate approval")
	}
	translator.tools["copy"] = activeTool{name: "protected.copy", server: "protected", arguments: map[string]any{"path": "source"}}
	if _, _, err := translator.approvalTool("protected", "", map[string]any{"path": "source"}); err == nil {
		t.Fatal("approvalTool() accepted ambiguous active calls")
	}
}

func TestTranslateChildNotifications(t *testing.T) {
	for _, method := range []string{"item/started", "item/completed", "item/agentMessage/delta", "turn/completed"} {
		t.Run(method, func(t *testing.T) {
			translator := newEventTranslator("parent", "turn")
			translator.tools["active"] = activeTool{name: "Agent"}
			sink := &recordingSink{}
			outcome, done, err := translator.translate(rpcMessage{
				Method: method,
				Params: json.RawMessage(`{"threadId":"child","turnId":"child-turn","itemId":"message","delta":"private child answer","item":{"type":"commandExecution","id":"active","command":"pwd","status":"completed"},"turn":{"id":"child-turn","status":"failed"}}`),
			}, sink)
			if err != nil || done || outcome.Failure != nil || outcome.Pending != nil {
				t.Fatalf("child notification: outcome=%#v done=%t error=%v", outcome, done, err)
			}
			if sink.text.Len() != 0 || len(sink.calls) != 0 || len(sink.results) != 0 || len(translator.tools) != 1 {
				t.Fatalf("child notification changed parent state: sink=%#v tools=%#v", sink, translator.tools)
			}
		})
	}
}

func TestTranslateRejectsInvalidParentIdentity(t *testing.T) {
	for _, method := range []string{"item/started", "item/completed", "item/agentMessage/delta", "turn/completed"} {
		for _, test := range []struct {
			name   string
			params string
		}{
			{name: "missing thread", params: `{"turnId":"turn","itemId":"message","turn":{"id":"turn","status":"completed"}}`},
			{name: "wrong turn", params: `{"threadId":"parent","turnId":"wrong","itemId":"message","turn":{"id":"wrong","status":"completed"}}`},
			{name: "missing turn", params: `{"threadId":"parent","itemId":"message","turn":{"status":"completed"}}`},
			{name: "malformed params", params: `{"threadId":123}`},
		} {
			t.Run(method+"/"+test.name, func(t *testing.T) {
				translator := newEventTranslator("parent", "turn")
				_, done, err := translator.translate(rpcMessage{Method: method, Params: json.RawMessage(test.params)}, &recordingSink{})
				if err == nil || done {
					t.Fatalf("invalid parent notification: done=%t error=%v", done, err)
				}
			})
		}
	}
	_, _, err := newEventTranslator("parent", "turn").translate(rpcMessage{
		Method: "item/agentMessage/delta", Params: json.RawMessage(`{"threadId":"parent","turnId":"turn","delta":"missing item ID"}`),
	}, &recordingSink{})
	if err == nil {
		t.Fatal("accepted parent text delta without an item ID")
	}
}

func TestRejectBufferedPostTerminalActivity(t *testing.T) {
	for _, test := range []struct {
		name    string
		method  string
		params  string
		wantErr string
	}{
		{name: "child item", method: "item/started", params: `{"threadId":"child"}`},
		{name: "child delta", method: "item/agentMessage/delta", params: `{"threadId":"child"}`},
		{name: "child turn", method: "turn/started", params: `{"threadId":"child"}`},
		{name: "child terminal", method: "turn/completed", params: `{"threadId":"child"}`},
		{name: "parent terminal", method: "turn/completed", params: `{"threadId":"parent"}`, wantErr: "duplicate terminal event"},
		{name: "parent activity", method: "item/started", params: `{"threadId":"parent"}`, wantErr: "activity after its terminal event"},
		{name: "missing thread", method: "item/started", params: `{}`, wantErr: "activity after its terminal event"},
		{name: "malformed identity", method: "item/started", params: `{"threadId":123}`, wantErr: "decode"},
		{name: "additive notification", method: "account/updated", params: `{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			frames := make(chan rpcFrame, 2)
			// Ignoring a child's completion must not hide a later parent violation.
			frames <- rpcFrame{message: rpcMessage{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"child"}`)}}
			frames <- rpcFrame{message: rpcMessage{Method: test.method, Params: json.RawMessage(test.params)}}
			close(frames)
			err := newEventTranslator("parent", "turn").rejectBufferedPostTerminalActivity(frames)
			if test.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want %q", err, test.wantErr)
			}
		})
	}
	frames := make(chan rpcFrame, 1)
	wantErr := errors.New("broken protocol stream")
	frames <- rpcFrame{err: wantErr}
	if err := newEventTranslator("parent", "turn").rejectBufferedPostTerminalActivity(frames); !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}
