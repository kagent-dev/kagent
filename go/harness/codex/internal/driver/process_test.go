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

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

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
read -r turn
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
trap 'printf terminated > "$EXIT_CAPTURE"; exit 0' INT TERM
read initialize
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read thread
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
read -r turn
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
read -r turn
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
read -r turn
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
case "$turn" in
  *Continue*)
    printf '%s\n' "$turn" > "$RESPONSE_CAPTURE"
    printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}}' ;;
  *)
printf '%s\n' '{"id":8,"method":"item/tool/requestUserInput","params":{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","isBlocking":true,"questions":[{"id":"namespace","header":"Namespace","question":"Which namespace?","options":[{"label":"default","description":"Default namespace"}],"isOther":true,"isSecret":false},{"id":"cluster","header":"Cluster","question":"Which cluster?","options":null,"isOther":false,"isSecret":false}]}}'
read interrupt
printf '%s\n' '{"id":4,"result":{}}'
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"interrupted"}}}' ;;
esac
while read ignored; do :; done
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(directory, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: workspace, PendingDir: directory, Model: "model", Provider: "provider",
		Environment:   append(os.Environ(), "INITIALIZE_CAPTURE="+initializeCapture, "RESPONSE_CAPTURE="+responseCapture),
		MaxFrameBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 100 * time.Millisecond,
	})
	outcome, err := driver.Run(t.Context(), runtime.Turn{TaskID: "task-1", ContextID: "context-1", Prompt: "deploy"}, &recordingSink{})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Pending == nil {
		t.Fatal("Run() did not return a pending turn")
	}
	request, ok := outcome.Pending.Request().(*runtime.AskUserRequest)
	if !ok || request.ID == "" || len(request.Questions) != 2 || request.Questions[0].ID != "namespace" || !slices.Equal(request.Questions[0].Choices, []string{"default"}) {
		t.Fatalf("ask-user request = %#v", outcome.Pending.Request())
	}
	initialize, err := os.ReadFile(initializeCapture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(initialize, []byte(`"capabilities":{"experimentalApi":true}`)) {
		t.Fatalf("experimental API was not enabled: %s", initialize)
	}

	fresh := NewProcessDriver(driver.config)
	_, _, pending, err := fresh.RestorePending()
	require.NoError(t, err)
	outcome, err = pending.Resume(t.Context(), &runtime.AskUserResponse{
		ID: request.ID, Answers: [][]string{{"default"}, {"production"}},
	}, &recordingSink{})
	if err != nil || outcome.Pending != nil || outcome.Failure != nil {
		t.Fatalf("resume outcome = %#v, error = %v", outcome, err)
	}
	// The question starts no tool item, so the call that asked it ends at the pause.
	response, err := os.ReadFile(responseCapture)
	if err != nil {
		t.Fatal(err)
	}
	var continued struct {
		Method string `json:"method"`
		Params struct {
			ThreadID string      `json:"threadId"`
			Input    []textInput `json:"input"`
		} `json:"params"`
	}
	require.NoError(t, json.Unmarshal(response, &continued))
	require.Equal(t, "turn/start", continued.Method)
	require.Equal(t, "thread-1", continued.Params.ThreadID)
	require.Len(t, continued.Params.Input, 1)
	require.Contains(t, continued.Params.Input[0].Text, `"answers":[["default"],["production"]]`)
	require.Contains(t, continued.Params.Input[0].Text, `"Question":"Which namespace?"`)
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

func TestDeferredTurnExitsBeforePublishingPendingAndReusesSavedResult(t *testing.T) {
	directory := t.TempDir()
	executable, capture := filepath.Join(directory, "codex"), filepath.Join(directory, "requests")
	exitCapture := filepath.Join(directory, "exit")
	writeDeferredAppServer(t, executable, true)
	calls := 0
	cfg := ProcessConfig{Executable: executable, Workspace: directory, Model: "model", Provider: "provider", PendingDir: directory,
		Environment: append(os.Environ(), "CAPTURE="+capture, "EXIT_CAPTURE="+exitCapture), MaxFrameBytes: 4096, MaxStderrBytes: 4096,
		InterruptGrace: time.Second, ApprovalServers: map[string]struct{}{"protected": {}}, Tools: fakeToolCaller{call: func(context.Context, string, string, map[string]any) (*mcp.CallToolResult, error) {
			calls++
			return nil, errors.New("must use cached result")
		}}}
	driver := NewProcessDriver(cfg)
	origin := runtime.NewTraceOrigin(trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled}))
	outcome, err := driver.Run(t.Context(), runtime.Turn{TaskID: "task-1", ContextID: "context-1", Prompt: "write", Origin: origin}, &recordingSink{})
	if err != nil || outcome.Pending == nil {
		t.Fatalf("defer = %#v %v", outcome, err)
	}
	exit, err := os.ReadFile(exitCapture)
	if err != nil || string(exit) != "graceful" {
		t.Fatalf("native process still running at input-required: %q %v", exit, err)
	}
	r, err := driver.loadPending()
	if err != nil {
		t.Fatal(err)
	}
	r.Decision = &runtime.ApprovalDecision{ID: r.Approval.ID, Approved: true}
	require.Equal(t, origin.SpanContext().TraceID(), r.Origin.SpanContext().TraceID())
	require.Equal(t, origin.SpanContext().SpanID(), r.Origin.SpanContext().SpanID())
	r.Result = &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "saved successful result"}}}
	r.Phase = phaseResult
	if err := driver.savePending(r); err != nil {
		t.Fatal(err)
	}
	// A known pre-launch failure must keep the saved result retryable and must
	// not publish it before a native turn can consume it.
	missing := cfg
	missing.Executable = filepath.Join(directory, "missing-codex")
	failed := NewProcessDriver(missing)
	_, _, retry, err := failed.RestorePending()
	require.NoError(t, err)
	sink := &recordingSink{}
	for range 2 {
		outcome, err := retry.Resume(t.Context(), r.Decision, sink)
		require.NoError(t, err)
		require.NotNil(t, outcome.Pending)
		require.Empty(t, sink.results)
		saved, err := failed.loadPending()
		require.NoError(t, err)
		require.Equal(t, phaseResult, saved.Phase)
		require.Equal(t, origin.SpanContext().SpanID(), saved.Origin.SpanContext().SpanID())
		retry = outcome.Pending
	}
	fresh := NewProcessDriver(cfg)
	_, _, pending, err := fresh.RestorePending()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pending.Resume(t.Context(), r.Decision, sink); err != nil || calls != 0 {
		t.Fatalf("cached result replayed tool: calls=%d err=%v", calls, err)
	}
	require.Len(t, sink.results, 1)
	require.Empty(t, sink.executions, "cached result must not look like another execution")
	requests, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(requests), `"method":"turn/interrupt"`) || !strings.Contains(string(requests), `"toolOutput"`) || !strings.Contains(string(requests), "saved successful result") {
		t.Fatalf("missing durable handoff: %s", requests)
	}
}

type cancelingResultSink struct {
	recordingSink
	cancel context.CancelFunc
}

func (s *cancelingResultSink) ToolResult(result runtime.ToolResult) error {
	if err := s.recordingSink.ToolResult(result); err != nil {
		return err
	}
	s.cancel()
	return nil
}

func TestDeferredTurnCancellationDuringNativeContinuationAdmitsNextTurn(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "codex")
	exitCapture := filepath.Join(directory, "exit")
	script := `#!/bin/sh
trap 'printf terminated > "$EXIT_CAPTURE"; exit 0' INT TERM
read initialize
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read thread
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
read -r turn
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
case "$turn" in
  *toolOutput*)
    read interrupt
    printf '%s\n' '{"id":4,"result":{}}'
    while read ignored; do :; done
    printf graceful > "$EXIT_CAPTURE" ;;
  *)
    printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}}'
    while read ignored; do :; done
    printf graceful > "$EXIT_CAPTURE" ;;
esac
`
	require.NoError(t, os.WriteFile(executable, []byte(script), 0o700))
	d := NewProcessDriver(ProcessConfig{Executable: executable, Workspace: directory, PendingDir: directory,
		Environment: append(os.Environ(), "EXIT_CAPTURE="+exitCapture), MaxFrameBytes: 4096, MaxStderrBytes: 1024,
		InterruptGrace: 100 * time.Millisecond})
	r := waitingRecord()
	r.Phase = phaseResult
	r.Decision = &runtime.ApprovalDecision{ID: r.Approval.ID, Approved: true}
	r.Result = &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "already committed"}}}
	require.NoError(t, d.savePending(&r))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, _, pending, err := d.RestorePending()
	require.NoError(t, err)
	sink := &cancelingResultSink{cancel: cancel}
	outcome, err := pending.Resume(ctx, r.Decision, sink)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, outcome.Pending)
	require.Len(t, sink.results, 1)
	_, err = os.Stat(exitCapture)
	require.NoError(t, err, "cancellation returned before the native process terminated")
	_, _, recovered, err := d.RestorePending()
	require.NoError(t, err)
	require.Nil(t, recovered)
	// A fresh driver reusing the DATA directory must not reserve the old task.
	fresh := NewProcessDriver(d.config)
	next, err := fresh.Run(t.Context(), runtime.Turn{TaskID: "next-task", ContextID: r.ContextID, ContinuationID: r.ThreadID, Prompt: "next task"}, &recordingSink{})
	require.NoError(t, err)
	require.Nil(t, next.Pending)
	require.Nil(t, next.Failure)
}

func TestDeferredApprovalResolutionAfterRestart(t *testing.T) {
	for _, action := range []string{"approve", "deny", "cancel"} {
		t.Run(action, func(t *testing.T) {
			directory := t.TempDir()
			executable := filepath.Join(directory, "codex")
			capture := filepath.Join(directory, "requests")
			writeDeferredAppServer(t, executable, false)
			calls := 0
			cfg := ProcessConfig{Executable: executable, Workspace: directory, PendingDir: directory,
				Environment: append(os.Environ(), "CAPTURE="+capture), MaxFrameBytes: 4096, MaxStderrBytes: 1024,
				InterruptGrace: time.Second, ApprovalServers: map[string]struct{}{"protected": {}},
				Tools: fakeToolCaller{call: func(_ context.Context, server, tool string, args map[string]any) (*mcp.CallToolResult, error) {
					calls++
					require.Equal(t, "approve", action, "denied or canceled operation executed")
					require.Equal(t, "protected", server)
					require.Equal(t, "write", tool)
					require.Equal(t, float64(7), args["value"])
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "committed"}}}, nil
				}}}
			driver := NewProcessDriver(cfg)
			outcome, err := driver.Run(t.Context(), runtime.Turn{TaskID: "task-1", ContextID: "context-1", Prompt: "write"}, &recordingSink{})
			require.NoError(t, err)
			require.NotNil(t, outcome.Pending)
			require.Zero(t, calls)
			before, err := os.ReadFile(capture)
			require.NoError(t, err)
			require.Contains(t, string(before), `"method":"turn/interrupt"`)
			if action == "cancel" {
				cfg.Executable = filepath.Join(directory, "missing-codex") // Cancellation must not launch a process.
			}
			fresh := NewProcessDriver(cfg)
			taskID, contextID, pending, err := fresh.RestorePending()
			require.NoError(t, err)
			require.Equal(t, "task-1", taskID)
			require.Equal(t, "context-1", contextID)
			require.Equal(t, outcome.Pending.Request(), pending.Request())
			request := pending.Request().(*runtime.ApprovalRequest)
			require.Equal(t, "call-1", request.CallID)
			require.NotEmpty(t, request.ID)
			sink := &recordingSink{}
			if action == "cancel" {
				require.NoError(t, pending.Cancel(t.Context(), &recordingSink{}))
				after, err := os.ReadFile(capture)
				require.NoError(t, err)
				require.Equal(t, before, after)
			} else {
				outcome, err = pending.Resume(t.Context(), &runtime.ApprovalDecision{ID: request.ID, Approved: action == "approve", RejectionReason: "production is serving traffic"}, sink)
				require.NoError(t, err)
				require.Nil(t, outcome.Pending)
				require.Nil(t, outcome.Failure)
				require.Len(t, sink.results, 1)
				require.Equal(t, action == "deny", sink.results[0].IsError)
				requests, err := os.ReadFile(capture)
				require.NoError(t, err)
				require.Contains(t, string(requests), `"method":"thread/resume"`)
				require.Contains(t, string(requests), `"toolOutput"`)
				if action == "deny" {
					require.Contains(t, string(requests), "production is serving traffic")
				}
			}
			if action == "approve" {
				require.Equal(t, 1, calls)
				require.Len(t, sink.executions, 1)
				require.Len(t, sink.executed, 1)
				require.Equal(t, "protected", sink.executions[0].MCPServer)
				require.Equal(t, "write", sink.executions[0].MCPTool)
			} else {
				require.Zero(t, calls)
				require.Empty(t, sink.executions)
			}
			_, _, restored, err := fresh.RestorePending()
			require.NoError(t, err)
			require.Nil(t, restored)
		})
	}
}

// writeDeferredAppServer supports both interruption orderings and always exits
// on stdin EOF. Tests decide whether to assert the graceful-exit marker.
func writeDeferredAppServer(t *testing.T, executable string, terminalFirst bool) {
	t.Helper()
	ack := `printf '%s\n' '{"id":4,"result":{}}'`
	terminal := `printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"interrupted"}}}'`
	interrupt := ack + "\n" + terminal
	if terminalFirst {
		interrupt = terminal + "\n" + ack
	}
	script := `#!/bin/sh
read initialize
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read thread
printf '%s\n' "$thread" >> "$CAPTURE"
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
read -r turn
printf '%s\n' "$turn" >> "$CAPTURE"
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
case "$turn" in
  *toolOutput*)
    printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}}' ;;
  *)
    printf '%s\n' '{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"type":"mcpToolCall","id":"call-1","server":"protected","tool":"write","arguments":{"value":7},"status":"inProgress"}}}'
    printf '%s\n' '{"id":7,"method":"mcpServer/elicitation/request","params":{"_meta":{"codex_approval_kind":"mcp_tool_call","tool_params":{"value":7}},"message":"Approve?","serverName":"protected","threadId":"thread-1","turnId":"turn-1"}}'
    read interrupt
    printf '%s\n' "$interrupt" >> "$CAPTURE"
` + interrupt + `
    ;;
esac
while read ignored; do :; done
if [ -n "$EXIT_CAPTURE" ]; then printf graceful > "$EXIT_CAPTURE"; fi
`
	require.NoError(t, os.WriteFile(executable, []byte(script), 0o700))
}

func TestProcessDriverDeclinesChildApprovalAndCompletesParent(t *testing.T) {
	directory := t.TempDir()
	executable, capture := filepath.Join(directory, "codex"), filepath.Join(directory, "response")
	script := `#!/bin/sh
read initialize
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read thread
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
read turn
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
printf '%s\n' '{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"type":"collabAgentToolCall","id":"spawn-1","tool":"spawnAgent","receiverThreadIds":["child-1"],"status":"inProgress"}}}'
printf '%s\n' '{"id":7,"method":"mcpServer/elicitation/request","params":{"threadId":"child-1","turnId":"child-turn","serverName":"protected","_meta":{"codex_approval_kind":"mcp_tool_call"}}}'
read response
printf '%s\n' "$response" > "$CAPTURE"
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}}'
while read ignored; do :; done
`
	require.NoError(t, os.WriteFile(executable, []byte(script), 0o700))
	d := NewProcessDriver(ProcessConfig{Executable: executable, Workspace: directory, PendingDir: directory,
		Environment: append(os.Environ(), "CAPTURE="+capture), MaxFrameBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: time.Second,
		ApprovalServers: map[string]struct{}{"protected": {}}})
	outcome, err := d.Run(t.Context(), runtime.Turn{TaskID: "task-1", ContextID: "context-1", Prompt: "delegate"}, &recordingSink{})
	require.NoError(t, err)
	require.Nil(t, outcome.Failure)
	require.Nil(t, outcome.Pending)
	data, err := os.ReadFile(capture)
	require.NoError(t, err)
	var response struct {
		Result struct {
			Action string `json:"action"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &response))
	require.Equal(t, "decline", response.Result.Action)
	_, _, pending, err := d.RestorePending()
	require.NoError(t, err)
	require.Nil(t, pending)
}
