package driver

import (
	"encoding/json"
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
