package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	tools := map[string]string{}
	for _, raw := range messages {
		var message rpcMessage
		if err := json.Unmarshal([]byte(raw), &message); err != nil {
			t.Fatal(err)
		}
		_, done, err := translateNotification(message, "thread", "turn", sink, tools)
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

func TestRPCClientRejectsOversizedAndUnexpectedRequests(t *testing.T) {
	for _, test := range []struct {
		name, input, want string
		max               int
	}{
		{"oversized", strings.Repeat("x", 20) + "\n", "exceeds", 10},
		{"invalid JSON-RPC version", `{"jsonrpc":"1.0","id":1,"result":{}}` + "\n", "unsupported Codex JSON-RPC version", 1024},
		{"server request", `{"jsonrpc":"2.0","id":9,"method":"item/tool/requestUserInput","params":{}}` + "\n", "unsupported Codex server request", 1024},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newRPCClient(nopWriteCloser{Buffer: &bytes.Buffer{}}, strings.NewReader(test.input), test.max)
			_, err := client.call(context.Background(), 1, "initialize", map[string]any{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("call() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRPCClientAcceptsOmittedJSONRPCVersion(t *testing.T) {
	client := newRPCClient(
		nopWriteCloser{Buffer: &bytes.Buffer{}},
		strings.NewReader(`{"id":1,"result":{"serverInfo":{"name":"codex-app-server"}}}`+"\n"),
		1024,
	)
	result, err := client.call(context.Background(), 1, "initialize", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result, []byte(`"name":"codex-app-server"`)) {
		t.Fatalf("initialize result = %s", result)
	}
}

type nopWriteCloser struct{ *bytes.Buffer }

func (n nopWriteCloser) Close() error { return nil }

func TestProcessDriverRunsPinnedProtocol(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "codex")
	capture := filepath.Join(directory, "requests.jsonl")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "codex-cli 0.148.0"
  exit 0
fi
read initialize
printf '%s\n' "$initialize" >> "$CAPTURE"
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"codex-app-server","version":"0.148.0"}}}'
read initialized
printf '%s\n' "$initialized" >> "$CAPTURE"
read thread
printf '%s\n' "$thread" >> "$CAPTURE"
printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"01900000-0000-7000-8000-000000000001"}}}'
read turn
printf '%s\n' "$turn" >> "$CAPTURE"
printf '%s\n' '{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"01900000-0000-7000-8000-000000000002","status":"inProgress"}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"item/agentMessage/delta","params":{"threadId":"01900000-0000-7000-8000-000000000001","turnId":"01900000-0000-7000-8000-000000000002","itemId":"message-1","delta":"hello"}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"01900000-0000-7000-8000-000000000001","turn":{"id":"01900000-0000-7000-8000-000000000002","status":"completed"}}}'
sleep 5
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(directory, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, ExpectedVersion: "0.148.0", StrictVersion: true, Workspace: workspace,
		Model: "gpt-5.2-codex", Provider: "kagent-openai", DeveloperInstruction: "help",
		Environment: append(os.Environ(), "CAPTURE="+capture), MaxFrameBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 100 * time.Millisecond,
	})
	if err := driver.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	outcome, err := driver.Run(context.Background(), runtime.Turn{Prompt: "say hello"}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Failure != nil || sink.text.String() != "hello" || len(sink.sessions) != 1 || sink.sessions[0].ContinuationID != "01900000-0000-7000-8000-000000000001" {
		t.Fatalf("outcome = %#v, sink = %#v, text = %q", outcome, sink, sink.text.String())
	}
	requests, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"method":"initialize"`, `"method":"initialized"`, `"method":"thread/start"`, `"approvalPolicy":"never"`, `"sandbox":"danger-full-access"`, `"method":"turn/start"`, `"text":"say hello"`} {
		if !bytes.Contains(requests, []byte(fragment)) {
			t.Errorf("requests omit %s:\n%s", fragment, requests)
		}
	}
	resumed := &recordingSink{}
	if _, err := driver.Run(context.Background(), runtime.Turn{Prompt: "resume", ContinuationID: "01900000-0000-7000-8000-000000000001"}, resumed); err != nil {
		t.Fatal(err)
	}
	requests, err = os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(requests, []byte(`"method":"thread/resume"`)) || !bytes.Contains(requests, []byte(`"threadId":"01900000-0000-7000-8000-000000000001"`)) {
		t.Fatalf("resume did not select the exact native thread:\n%s", requests)
	}
}

func TestProcessDriverRejectsWorkspaceConfiguration(t *testing.T) {
	workspace := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".codex", "config.toml"), []byte("model = \"injected\""), 0o600); err != nil {
		t.Fatal(err)
	}
	driver := NewProcessDriver(ProcessConfig{Workspace: workspace})
	_, err := driver.Run(context.Background(), runtime.Turn{Prompt: "hello"}, &recordingSink{})
	if err == nil || !strings.Contains(err.Error(), "workspace Codex configuration") {
		t.Fatalf("Run() error = %v", err)
	}
}
