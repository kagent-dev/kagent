package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
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
	for _, fragment := range []string{`"method":"initialize"`, `"method":"initialized"`, `"method":"thread/start"`, `"approvalPolicy":{"granular":{"mcp_elicitations":true,"request_permissions":false,"rules":false,"sandbox_approval":false,"skill_approval":false}}`, `"sandbox":"danger-full-access"`, `"method":"turn/start"`, `"text":"say hello"`} {
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

func TestProcessDriverLetsAppServerExitAfterStdinEOF(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "codex")
	exitCapture := filepath.Join(directory, "exit")
	script := `#!/bin/sh
trap 'printf terminated > "$EXIT_CAPTURE"; exit 0' TERM
read initialize
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read thread
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
read turn
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}}'
while read ignored; do :; done
i=0
while [ "$i" -lt 128 ]; do
  printf '%s\n' '{"method":"thread/name/updated","params":{"threadId":"thread-1","threadName":"done"}}'
  i=$((i + 1))
done
printf graceful > "$EXIT_CAPTURE"
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(directory, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: workspace, Model: "model", Provider: "provider",
		Environment: append(os.Environ(), "EXIT_CAPTURE="+exitCapture), MaxFrameBytes: 4096,
		MaxStderrBytes: 1024, InterruptGrace: time.Second,
	})
	if _, err := driver.Run(t.Context(), runtime.Turn{Prompt: "hello"}, &recordingSink{}); err != nil {
		t.Fatal(err)
	}
	exit, err := os.ReadFile(exitCapture)
	if err != nil {
		t.Fatal(err)
	}
	if string(exit) != "graceful" {
		t.Fatalf("app-server exit = %q, want graceful stdin EOF", exit)
	}
}

func TestProcessDriverDrainsAfterInitializationFailure(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "codex")
	exitCapture := filepath.Join(directory, "exit")
	script := `#!/bin/sh
read initialize
printf '%s\n' '{"id":1,"error":{"code":-32603,"message":"initialization failed"}}'
while read ignored; do :; done
i=0
while [ "$i" -lt 128 ]; do
  printf '%s\n' '{"method":"thread/tokenUsage/updated","params":{}}'
  i=$((i + 1))
done
printf graceful > "$EXIT_CAPTURE"
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: directory,
		Environment: append(os.Environ(), "EXIT_CAPTURE="+exitCapture), MaxFrameBytes: 4096,
		MaxStderrBytes: 1024, InterruptGrace: time.Second,
	})
	if _, err := driver.Run(t.Context(), runtime.Turn{Prompt: "hello"}, &recordingSink{}); err == nil || !strings.Contains(err.Error(), "initialization failed") {
		t.Fatalf("Run() error = %v, want initialization failed", err)
	}
	exit, err := os.ReadFile(exitCapture)
	if err != nil {
		t.Fatal(err)
	}
	if string(exit) != "graceful" {
		t.Fatalf("app-server exit = %q, want graceful stdin EOF", exit)
	}
}

func TestProcessDriverSharedAgentNotifications(t *testing.T) {
	for _, tc := range []struct {
		name, fixture string
		wantChildTool bool
	}{
		{name: "spawn completes first", fixture: "app-server-shared-agent.jsonl", wantChildTool: true},
		// The child's tool starts before the spawn names its thread, reuses the spawn's ID,
		// and completes after the spawn.
		{name: "child starts early with a reused ID", fixture: "app-server-shared-agent-early.jsonl"},
	} {
		t.Run(tc.name, func(t *testing.T) { testSharedAgentNotifications(t, tc.fixture, tc.wantChildTool) })
	}
}

func testSharedAgentNotifications(t *testing.T, fixture string, wantChildTool bool) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "codex")
	notifications, err := filepath.Abs("../../testdata/" + fixture)
	if err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
read initialize
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read thread
printf '%s\n' '{"id":2,"result":{"thread":{"id":"parent"}}}'
read turn
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn"}}}'
cat "$NOTIFICATIONS"
while read ignored; do :; done
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: directory, Model: "model", Provider: "provider",
		Environment: append(os.Environ(), "NOTIFICATIONS="+notifications), MaxFrameBytes: 4096,
		MaxStderrBytes: 1024, InterruptGrace: time.Second,
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	sink := &recordingSink{}
	outcome, err := driver.Run(ctx, runtime.Turn{Prompt: "Translate into French: Good morning, my friend."}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Failure != nil || outcome.Pending != nil || sink.text.String() != "Bonjour, mon ami." {
		t.Fatalf("outcome = %#v, text = %q", outcome, sink.text.String())
	}
	// The child's text stays private, but its tools go under the spawn call.
	wantTools := 1
	if wantChildTool {
		wantTools = 2
	}
	if len(sink.calls) != wantTools || len(sink.results) != wantTools {
		t.Fatalf("tool events = %#v, %#v, want %d", sink.calls, sink.results, wantTools)
	}
	if wantChildTool && (sink.calls[1].ID != "child-command" || sink.calls[1].ParentToolID != "agent-1") {
		t.Fatalf("child tool = %#v, want child-command under agent-1", sink.calls[1])
	}
	if sink.calls[0].ID != "agent-1" || sink.calls[0].Name != "Agent" || sink.results[0].ID != "agent-1" || sink.results[0].Name != "Agent" || sink.results[0].IsError {
		t.Fatalf("delegation events = %#v, %#v", sink.calls[0], sink.results[0])
	}
	result, ok := sink.results[0].Result.(map[string]any)
	if !ok || result["status"] != "completed" {
		t.Fatalf("delegation result = %#v, want completed", sink.results[0].Result)
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

func TestProcessDriverParksAndResolvesMCPApproval(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "codex")
	capture := filepath.Join(directory, "approval.json")
	script := `#!/bin/sh
read initialize
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read thread
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
read turn
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
printf '%s\n' '{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"type":"mcpToolCall","id":"call-1","server":"protected","tool":"write","arguments":{"value":7},"status":"inProgress"}}}'
printf '%s\n' '{"id":7,"method":"mcpServer/elicitation/request","params":{"_meta":{"codex_approval_kind":"mcp_tool_call","tool_params":{"value":7}},"message":"Allow protected.write?","mode":"form","requestedSchema":{"type":"object","properties":{}},"serverName":"protected","threadId":"thread-1","turnId":"turn-1"}}'
read approval
printf '%s\n' "$approval" > "$CAPTURE"
printf '%s\n' '{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"type":"mcpToolCall","id":"call-1","server":"protected","tool":"write","arguments":{"value":7},"result":"ok","status":"completed"}}}'
printf '%s\n' '{"method":"thread/tokenUsage/updated","params":{"threadId":"thread-1","turnId":"turn-1","tokenUsage":{"total":{"inputTokens":10,"outputTokens":1},"last":{"inputTokens":10,"outputTokens":1}}}}'
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}}'
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
		Executable: executable, Workspace: workspace, Model: "model", Provider: "provider",
		Environment: append(os.Environ(), "CAPTURE="+capture), MaxFrameBytes: 4096, MaxStderrBytes: 1024,
		InterruptGrace: 100 * time.Millisecond, ApprovalServers: map[string]struct{}{"protected": {}},
	})
	sink := &recordingSink{}
	outcome, err := driver.Run(t.Context(), runtime.Turn{Prompt: "write"}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Pending == nil {
		t.Fatal("Run() did not return a pending turn")
	}
	request, ok := outcome.Pending.Request().(*runtime.ApprovalRequest)
	if !ok || request.ID != "7" || request.CallID != "call-1" || request.Name != "protected.write" {
		t.Fatalf("pending request = %#v", outcome.Pending.Request())
	}
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatalf("MCP call was resolved before approval: %v", err)
	}
	resumed := time.Now()
	outcome, err = outcome.Pending.Resume(t.Context(), &runtime.ApprovalDecision{
		ID: "7", Approved: false, RejectionReason: "production is still serving traffic",
	}, sink)
	if err != nil || outcome.Pending != nil || outcome.Failure != nil {
		t.Fatalf("resume outcome = %#v, error = %v", outcome, err)
	}
	// Usage arrives after the resume, but the call ended at its tool start before the pause.
	if len(sink.models) != 1 || !sink.models[0].End.Before(resumed) || sink.models[0].Start.After(sink.models[0].End) {
		t.Fatalf("model calls = %#v, resumed at %v", sink.models, resumed)
	}
	response, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(response, []byte(`"id":7`)) || !bytes.Contains(response, []byte(`"action":"decline"`)) || bytes.Contains(response, []byte(`"rejection_reason"`)) || bytes.Contains(response, []byte(`"content"`)) {
		t.Fatalf("approval response = %s", response)
	}
}

// A subagent's approval matches the subagent's own tool, which goes under the spawn call.
func TestProcessDriverParksMCPApprovalInsideASubagent(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "codex")
	script := `#!/bin/sh
read initialize
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read thread
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
read turn
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
printf '%s\n' '{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"type":"collabAgentToolCall","id":"spawn-1","tool":"spawnAgent","receiverThreadIds":[],"status":"inProgress"}}}'
printf '%s\n' '{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"type":"collabAgentToolCall","id":"spawn-1","tool":"spawnAgent","receiverThreadIds":["child"],"status":"completed"}}}'
printf '%s\n' '{"method":"item/started","params":{"threadId":"child","turnId":"child-turn","item":{"type":"mcpToolCall","id":"child-call","server":"protected","tool":"write","arguments":{"value":7},"status":"inProgress"}}}'
printf '%s\n' '{"id":7,"method":"mcpServer/elicitation/request","params":{"_meta":{"codex_approval_kind":"mcp_tool_call","tool_params":{"value":7}},"message":"Allow protected.write?","mode":"form","requestedSchema":{"type":"object","properties":{}},"serverName":"protected","threadId":"child","turnId":"child-turn"}}'
read approval
printf '%s\n' '{"method":"item/completed","params":{"threadId":"child","turnId":"child-turn","item":{"type":"mcpToolCall","id":"child-call","server":"protected","tool":"write","arguments":{"value":7},"result":"ok","status":"completed"}}}'
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}}'
sleep 5
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: directory, Model: "model", Provider: "provider",
		Environment: os.Environ(), MaxFrameBytes: 4096, MaxStderrBytes: 1024,
		InterruptGrace: 100 * time.Millisecond, ApprovalServers: map[string]struct{}{"protected": {}},
	})
	sink := &recordingSink{}
	outcome, err := driver.Run(t.Context(), runtime.Turn{Prompt: "write"}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Pending == nil {
		t.Fatal("Run() did not return a pending turn")
	}
	if request, ok := outcome.Pending.Request().(*runtime.ApprovalRequest); !ok || request.CallID != "child-call" || request.Name != "protected.write" {
		t.Fatalf("pending request = %#v", outcome.Pending.Request())
	}
	outcome, err = outcome.Pending.Resume(t.Context(), &runtime.ApprovalDecision{ID: "7", Approved: true}, sink)
	if err != nil || outcome.Pending != nil || outcome.Failure != nil {
		t.Fatalf("resume outcome = %#v, error = %v", outcome, err)
	}
	if len(sink.calls) != 2 || sink.calls[1].ID != "child-call" || sink.calls[1].ParentToolID != "spawn-1" {
		t.Fatalf("tool calls = %#v, want the child's call under spawn-1", sink.calls)
	}
	if len(sink.results) != 2 || sink.results[1].ID != "child-call" || sink.results[1].IsError {
		t.Fatalf("tool results = %#v", sink.results)
	}
}

func TestProcessDriverAskUserThreads(t *testing.T) {
	for _, tc := range []struct {
		name, thread, turn string
		wantErr            bool
	}{
		{name: "a spawned subagent", thread: "child", turn: "child-turn"},
		{name: "an unknown thread", thread: "stranger", turn: "child-turn", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			executable := filepath.Join(directory, "codex")
			script := `#!/bin/sh
read initialize
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read thread
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
read turn
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
printf '%s\n' '{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"type":"collabAgentToolCall","id":"spawn-1","tool":"spawnAgent","receiverThreadIds":[],"status":"inProgress"}}}'
printf '%s\n' '{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"type":"collabAgentToolCall","id":"spawn-1","tool":"spawnAgent","receiverThreadIds":["child"],"status":"completed"}}}'
printf '%s\n' '{"id":8,"method":"item/tool/requestUserInput","params":{"threadId":"` + tc.thread + `","turnId":"` + tc.turn + `","itemId":"item-1","isBlocking":true,"questions":[{"id":"namespace","header":"Namespace","question":"Which namespace?","options":null,"isOther":true,"isSecret":false}]}}'
read response
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}}'
sleep 5
`
			if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			driver := NewProcessDriver(ProcessConfig{
				Executable: executable, Workspace: directory, Model: "model", Provider: "provider",
				Environment: os.Environ(), MaxFrameBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 100 * time.Millisecond,
			})
			outcome, err := driver.Run(t.Context(), runtime.Turn{Prompt: "clean up"}, &recordingSink{})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Run() outcome = %#v, want an error for a question from %s", outcome, tc.thread)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if request, ok := outcome.Pending.Request().(*runtime.AskUserRequest); !ok || request.ID != "8" {
				t.Fatalf("pending request = %#v", outcome.Pending.Request())
			}
			outcome, err = outcome.Pending.Resume(t.Context(), &runtime.AskUserResponse{ID: "8", Answers: [][]string{{"default"}}}, &recordingSink{})
			if err != nil || outcome.Pending != nil || outcome.Failure != nil {
				t.Fatalf("resume outcome = %#v, error = %v", outcome, err)
			}
		})
	}
}

func TestProcessDriverParksAndResolvesAskUserWithExperimentalAPI(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "codex")
	initializeCapture := filepath.Join(directory, "initialize.json")
	responseCapture := filepath.Join(directory, "response.json")
	script := `#!/bin/sh
read initialize
printf '%s\n' "$initialize" > "$INITIALIZE_CAPTURE"
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read thread
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
read turn
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
printf '%s\n' '{"id":8,"method":"item/tool/requestUserInput","params":{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","isBlocking":true,"questions":[{"id":"namespace","header":"Namespace","question":"Which namespace?","options":[{"label":"default","description":"Default namespace"}],"isOther":true,"isSecret":false},{"id":"cluster","header":"Cluster","question":"Which cluster?","options":null,"isOther":false,"isSecret":false}]}}'
read response
printf '%s\n' "$response" > "$RESPONSE_CAPTURE"
printf '%s\n' '{"method":"thread/tokenUsage/updated","params":{"threadId":"thread-1","turnId":"turn-1","tokenUsage":{"total":{"inputTokens":10,"outputTokens":1},"last":{"inputTokens":10,"outputTokens":1}}}}'
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}}'
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
		Executable: executable, Workspace: workspace, Model: "model", Provider: "provider",
		Environment:   append(os.Environ(), "INITIALIZE_CAPTURE="+initializeCapture, "RESPONSE_CAPTURE="+responseCapture),
		MaxFrameBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 100 * time.Millisecond,
	})
	outcome, err := driver.Run(t.Context(), runtime.Turn{Prompt: "deploy"}, &recordingSink{})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Pending == nil {
		t.Fatal("Run() did not return a pending turn")
	}
	request, ok := outcome.Pending.Request().(*runtime.AskUserRequest)
	if !ok || request.ID != "8" || len(request.Questions) != 2 || request.Questions[0].ID != "namespace" || !slices.Equal(request.Questions[0].Choices, []string{"default"}) {
		t.Fatalf("ask-user request = %#v", outcome.Pending.Request())
	}
	initialize, err := os.ReadFile(initializeCapture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(initialize, []byte(`"capabilities":{"experimentalApi":true}`)) {
		t.Fatalf("experimental API was not enabled: %s", initialize)
	}

	resumed, sink := time.Now(), &recordingSink{}
	outcome, err = outcome.Pending.Resume(t.Context(), &runtime.AskUserResponse{
		ID: "8", Answers: [][]string{{"default"}, {"production"}},
	}, sink)
	if err != nil || outcome.Pending != nil || outcome.Failure != nil {
		t.Fatalf("resume outcome = %#v, error = %v", outcome, err)
	}
	// The question starts no tool item, so the call that asked it ends at the pause.
	if len(sink.models) != 1 || !sink.models[0].End.Before(resumed) || sink.models[0].Start.After(sink.models[0].End) {
		t.Fatalf("model calls = %#v, resumed at %v", sink.models, resumed)
	}
	response, err := os.ReadFile(responseCapture)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"id":8`, `"answers"`, `"namespace":{"answers":["default"]}`, `"cluster":{"answers":["production"]}`} {
		if !bytes.Contains(response, []byte(fragment)) {
			t.Fatalf("ask-user response omits %s: %s", fragment, response)
		}
	}
}

func TestDecodeAskUserRequestRejectsDuplicateQuestionIDs(t *testing.T) {
	message := rpcMessage{ID: json.RawMessage(`8`), Params: json.RawMessage(`{
		"threadId":"thread-1","turnId":"turn-1","questions":[
			{"id":"target","header":"One","question":"First?"},
			{"id":"target","header":"Two","question":"Second?"}
		]
	}`)}
	if _, err := decodeAskUserRequest(message, newEventTranslator("thread-1", "turn-1")); err == nil || !strings.Contains(err.Error(), "duplicate question ID") {
		t.Fatalf("decodeAskUserRequest() error = %v", err)
	}
}

func TestDecodeAskUserRequestRejectsSecretQuestions(t *testing.T) {
	message := rpcMessage{ID: json.RawMessage(`8`), Params: json.RawMessage(`{
		"threadId":"thread-1","turnId":"turn-1","questions":[
			{"id":"token","header":"Token","question":"Enter a token","isSecret":true}
		]
	}`)}
	if _, err := decodeAskUserRequest(message, newEventTranslator("thread-1", "turn-1")); err == nil || !strings.Contains(err.Error(), "secret ask-user") {
		t.Fatalf("decodeAskUserRequest() error = %v", err)
	}
}

func TestProcessDriverCancelsParkedMCPApproval(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "codex")
	approvalCapture := filepath.Join(directory, "approval.json")
	interruptCapture := filepath.Join(directory, "interrupt.json")
	script := `#!/bin/sh
read initialize
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read thread
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
read turn
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
printf '%s\n' '{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"type":"mcpToolCall","id":"call-1","server":"protected","tool":"write","arguments":{},"status":"inProgress"}}}'
printf '%s\n' '{"id":7,"method":"mcpServer/elicitation/request","params":{"_meta":{"codex_approval_kind":"mcp_tool_call","tool_params":{}},"message":"Allow protected.write?","mode":"form","requestedSchema":{"type":"object","properties":{}},"serverName":"protected","threadId":"thread-1","turnId":"turn-1"}}'
read approval
printf '%s\n' "$approval" > "$APPROVAL_CAPTURE"
printf '%s\n' '{"method":"thread/tokenUsage/updated","params":{"threadId":"thread-1","turnId":"turn-1","tokenUsage":{"total":{"inputTokens":10,"outputTokens":1},"last":{"inputTokens":10,"outputTokens":1}}}}'
read interrupt
printf '%s\n' "$interrupt" > "$INTERRUPT_CAPTURE"
printf '%s\n' '{"method":"thread/tokenUsage/updated","params":{"threadId":"thread-1","turnId":"turn-1","tokenUsage":{"total":{"inputTokens":20,"outputTokens":2},"last":{"inputTokens":10,"outputTokens":1}}}}'
printf '%s\n' '{"id":4,"result":{}}'
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
		Executable: executable, Workspace: workspace, Model: "model", Provider: "provider",
		Environment:   append(os.Environ(), "APPROVAL_CAPTURE="+approvalCapture, "INTERRUPT_CAPTURE="+interruptCapture),
		MaxFrameBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 100 * time.Millisecond,
		ApprovalServers: map[string]struct{}{"protected": {}},
	})
	outcome, err := driver.Run(t.Context(), runtime.Turn{Prompt: "write"}, &recordingSink{})
	if err != nil || outcome.Pending == nil {
		t.Fatalf("Run() = %#v, %v", outcome, err)
	}
	sink := &recordingSink{}
	if err := outcome.Pending.Cancel(t.Context(), sink); err != nil {
		t.Fatal(err)
	}
	// One usage was waiting before the interrupt and one arrived while Codex answered it.
	if len(sink.models) != 2 {
		t.Fatalf("model calls = %d, want the 2 Codex reported", len(sink.models))
	}

	approval, err := os.ReadFile(approvalCapture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(approval, []byte(`"id":7`)) || !bytes.Contains(approval, []byte(`"action":"cancel"`)) || !bytes.Contains(approval, []byte(`"content":null`)) {
		t.Fatalf("cancel response = %s", approval)
	}
	interrupt, err := os.ReadFile(interruptCapture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(interrupt, []byte(`"method":"turn/interrupt"`)) || !bytes.Contains(interrupt, []byte(`"threadId":"thread-1"`)) || !bytes.Contains(interrupt, []byte(`"turnId":"turn-1"`)) {
		t.Fatalf("interrupt request = %s", interrupt)
	}
}

func TestProcessDriverCancellationKeepsReportedUsage(t *testing.T) {
	directory := t.TempDir()
	executable, started := filepath.Join(directory, "codex"), filepath.Join(directory, "started")
	script := `#!/bin/sh
read initialize
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read thread
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
read turn
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
: > "$STARTED"
read interrupt
printf '%s\n' '{"method":"thread/tokenUsage/updated","params":{"threadId":"thread-1","turnId":"turn-1","tokenUsage":{"total":{"inputTokens":10,"outputTokens":1},"last":{"inputTokens":10,"outputTokens":1}}}}'
printf '%s\n' '{"id":4,"result":{}}'
# Flush the final usage reports only when the driver requests graceful shutdown.
while read ignored; do :; done
# Exceed the RPC frame buffer so the reader must keep draining after process exit.
i=2
while [ "$i" -le 128 ]; do
  printf '{"method":"thread/tokenUsage/updated","params":{"threadId":"thread-1","turnId":"turn-1","tokenUsage":{"total":{"inputTokens":%s,"outputTokens":%s},"last":{"inputTokens":10,"outputTokens":1}}}}\n' "$((i * 10))" "$i"
  i=$((i + 1))
done
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(directory, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: workspace, Model: "model", Provider: "provider",
		Environment: append(os.Environ(), "STARTED="+started), MaxFrameBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: time.Second,
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		// Cancel only once the turn is running, so the cancel reaches consume.
		for _, err := os.Stat(started); err != nil; _, err = os.Stat(started) {
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
	}()
	sink := &recordingSink{}
	if _, err := driver.Run(ctx, runtime.Turn{Prompt: "work"}, sink); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	// Codex reports usage while it answers the interrupt and after it.
	if len(sink.models) != 128 {
		t.Fatalf("model calls = %d, want the 128 Codex reported", len(sink.models))
	}
	for i, call := range sink.models {
		if call.InputTokens != 10 || call.OutputTokens != 1 || call.UsagePartial {
			t.Fatalf("model call %d = %+v, want the complete reported usage", i, call)
		}
	}
}

func TestProcessDriverCancellationRecordsTheCallCodexNeverReported(t *testing.T) {
	directory := t.TempDir()
	executable, started := filepath.Join(directory, "codex"), filepath.Join(directory, "started")
	script := `#!/bin/sh
read initialize
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read thread
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
read turn
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
: > "$STARTED"
read interrupt
printf '%s\n' '{"id":4,"result":{}}'
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(directory, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: workspace, Model: "model", Provider: "provider",
		Environment: append(os.Environ(), "STARTED="+started), MaxFrameBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: time.Second,
	})
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		for _, err := os.Stat(started); err != nil; _, err = os.Stat(started) {
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
	}()
	sink := &recordingSink{}
	if _, err := driver.Run(ctx, runtime.Turn{Prompt: "work"}, sink); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if len(sink.models) != 1 {
		t.Fatalf("model calls = %+v, want the 1 call the cancel cut short", sink.models)
	}
	call := sink.models[0]
	if call.StopReason != tracing.FinishReasonError || !call.UsagePartial || !call.Canceled || call.InputTokens != 0 {
		t.Fatalf("model call = %+v, want a canceled partial call with no counts", call)
	}
}

func TestProcessDriverCancellationAfterTheTurnCompletedRecordsNoCall(t *testing.T) {
	directory := t.TempDir()
	executable, started := filepath.Join(directory, "codex"), filepath.Join(directory, "started")
	script := `#!/bin/sh
read initialize
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read thread
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
read turn
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
: > "$STARTED"
read interrupt
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}}'
printf '%s\n' '{"id":4,"result":{}}'
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(directory, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: workspace, Model: "model", Provider: "provider",
		Environment: append(os.Environ(), "STARTED="+started), MaxFrameBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: time.Second,
	})
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		for _, err := os.Stat(started); err != nil; _, err = os.Stat(started) {
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
	}()
	sink := &recordingSink{}
	if _, err := driver.Run(ctx, runtime.Turn{Prompt: "work"}, sink); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if len(sink.models) != 0 {
		t.Fatalf("model calls = %+v, want none: the turn completed before the cancel", sink.models)
	}
}

func TestApprovalResponseAcceptsWithEmptyRequestedContent(t *testing.T) {
	response, err := codexInputResponse(
		&runtime.ApprovalRequest{ID: "7"},
		&runtime.ApprovalDecision{ID: "7", Approved: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if response["action"] != "accept" {
		t.Fatalf("approval response = %#v", response)
	}
	if content, ok := response["content"].(map[string]any); !ok || len(content) != 0 {
		t.Fatalf("approval content = %#v", response["content"])
	}
}
