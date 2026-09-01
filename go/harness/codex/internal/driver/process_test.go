package driver

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

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
