package driver

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

type recordingSink struct {
	sessions []runtime.SessionStarted
	text     strings.Builder
}

func (s *recordingSink) SessionStarted(event runtime.SessionStarted) error {
	s.sessions = append(s.sessions, event)
	return nil
}
func (s *recordingSink) TextDelta(event runtime.TextDelta) error {
	s.text.WriteString(event.Text)
	return nil
}
func (*recordingSink) ToolCall(runtime.ToolCall) error     { return nil }
func (*recordingSink) ToolResult(runtime.ToolResult) error { return nil }

func TestResumedEventSinkDropsOnlyInterruptedResponseWarning(t *testing.T) {
	underlying := &recordingSink{}
	sink := resumedEventSink{EventSink: underlying}

	if err := sink.TextDelta(runtime.TextDelta{Text: "\n" + interruptedResponseWarning + "\n"}); err != nil {
		t.Fatal(err)
	}
	if err := sink.TextDelta(runtime.TextDelta{Text: "continued"}); err != nil {
		t.Fatal(err)
	}
	if underlying.text.String() != "continued" {
		t.Fatalf("resumed text = %q, want continued", underlying.text.String())
	}

	if _, err := emitEvent(Event{Kind: EventTextDelta, Text: interruptedResponseWarning}, underlying, false); err != nil {
		t.Fatal(err)
	}
	if underlying.text.String() != "continued"+interruptedResponseWarning {
		t.Fatalf("ordinary text = %q, want the vendor warning preserved", underlying.text.String())
	}
}

func TestTraceEnvironment(t *testing.T) {
	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatal(err)
	}
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatal(err)
	}
	state, err := trace.ParseTraceState("vendor=value")
	if err != nil {
		t.Fatal(err)
	}
	ctxWithTraceState := trace.ContextWithSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled, TraceState: state,
	}))
	ctxWithoutTraceState := trace.ContextWithSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))
	for _, test := range []struct {
		name string
		ctx  context.Context
		want []string
	}{
		{
			name: "replace stale trace context",
			ctx:  ctxWithTraceState,
			want: []string{
				"PATH=/bin",
				"TRACEPARENT=00-0102030405060708090a0b0c0d0e0f10-0102030405060708-01",
				"TRACESTATE=vendor=value",
			},
		},
		{
			name: "remove stale trace state",
			ctx:  ctxWithoutTraceState,
			want: []string{
				"PATH=/bin",
				"TRACEPARENT=00-0102030405060708090a0b0c0d0e0f10-0102030405060708-01",
			},
		},
		{name: "remove stale trace context", ctx: t.Context(), want: []string{"PATH=/bin"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			environment := []string{"PATH=/bin", "TRACEPARENT=stale", "TRACESTATE=stale"}
			got := traceEnvironment(test.ctx, environment)
			if !slices.Equal(got, test.want) {
				t.Fatalf("trace environment = %q, want %q", got, test.want)
			}
			wantInput := []string{"PATH=/bin", "TRACEPARENT=stale", "TRACESTATE=stale"}
			if !slices.Equal(environment, wantInput) {
				t.Fatalf("input environment mutated to %q", environment)
			}
		})
	}
}

func TestProcessDriverArgumentsAndStream(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "args")
	executable := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo '2.1.285 (Claude Code)'; exit 0; fi\nprintf '%s\\n' \"$@\" > \"$CAPTURE\"\nIFS= read -r line\nprintf '%s\\n' \"$line\" > \"$CAPTURE.stdin\"\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}' '{\"type\":\"result\",\"subtype\":\"success\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}'\ncat >/dev/null\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	agentsJSON := `{"reviewer":{"description":"Reviews changes","prompt":"Review carefully","tools":["Read"]}}`
	mcpConfigPath := filepath.Join(dir, "mcp.json")
	skillRoot := filepath.Join(dir, "generated-skills")
	d := NewProcessDriver(ProcessConfig{Executable: executable, ExpectedVersion: pinnedClaudeVersion, StrictVersion: true, Workspace: dir, Model: "claude-test", AppendSystemPrompt: "extra", AgentsJSON: agentsJSON, MCPConfigPath: mcpConfigPath, SkillRoot: skillRoot, PluginDirs: []string{filepath.Join(dir, "plugin-a")}, Environment: []string{"CAPTURE=" + capture}, MaxEventBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: time.Second})
	if err := d.Validate(t.Context()); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	sink := &recordingSink{}
	turn := runtime.Turn{Prompt: "hello", ContinuationID: "11111111-1111-4111-8111-111111111111"}
	outcome, err := d.Run(t.Context(), turn, sink)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if outcome.Failure != nil {
		t.Fatalf("Run() outcome = %#v", outcome)
	}
	args, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join(d.Args(turn), "\n") + "\n"
	if string(args) != want {
		t.Errorf("arguments = %q, want %q", args, want)
	}
	if strings.Contains(string(args), turn.Prompt) {
		t.Error("arguments carry the prompt, which belongs on stdin")
	}
	input, err := os.ReadFile(capture + ".stdin")
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"type":"user","message":{"role":"user","content":"hello"}}` + "\n"; string(input) != want {
		t.Errorf("stdin = %q, want %q", input, want)
	}
	for _, required := range []string{
		"--dangerously-skip-permissions\n",
		"--strict-mcp-config\n",
		"--input-format\nstream-json\n",
	} {
		if !strings.Contains(string(args), required) {
			t.Errorf("arguments do not contain required fixed policy flag %q", strings.TrimSpace(required))
		}
	}
	if !strings.Contains(string(args), "--agents\n"+agentsJSON+"\n") {
		t.Error("arguments do not contain compiler-owned local agents JSON")
	}
	if !strings.Contains(string(args), "--mcp-config\n"+mcpConfigPath+"\n") {
		t.Error("arguments do not contain compiler-owned MCP configuration")
	}
	if !strings.Contains(string(args), "--add-dir\n"+skillRoot+"\n") {
		t.Error("arguments do not expose compiler-owned skills")
	}
	if !strings.Contains(string(args), "--plugin-dir\n"+filepath.Join(dir, "plugin-a")+"\n") {
		t.Error("arguments do not load the native plugin directory")
	}
	if strings.Contains(string(args), "--permission-prompt-tool\n") {
		t.Error("arguments unexpectedly configure Claude's native permission bridge")
	}
	if len(sink.sessions) != 1 || sink.sessions[0].ContinuationID != turn.ContinuationID {
		t.Errorf("session events = %#v", sink.sessions)
	}
}

func TestProcessDriverParserFailureIncludesStderr(t *testing.T) {
	for _, test := range []struct {
		name   string
		script string
	}{
		{name: "exit before result", script: "echo 'resume failed' >&2\nexit 17\n"},
		{name: "malformed output from live process", script: "echo 'resume failed' >&2\necho 'invalid json'\nexec sleep 30\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			executable := filepath.Join(dir, "claude")
			if err := os.WriteFile(executable, []byte("#!/bin/sh\n"+test.script), 0o700); err != nil {
				t.Fatal(err)
			}
			d := NewProcessDriver(ProcessConfig{
				Executable: executable, Workspace: dir,
				MaxEventBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 50 * time.Millisecond,
			})
			started := time.Now()
			_, err := d.Run(t.Context(), runtime.Turn{Prompt: "hello"}, &recordingSink{})
			if err == nil || !strings.Contains(err.Error(), "resume failed") {
				t.Fatalf("Run() error = %v, want subprocess stderr", err)
			}
			if time.Since(started) > time.Second {
				t.Fatal("parser failure waited for the live subprocess to exit")
			}
		})
	}
}

func TestProcessDriverCancellation(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}'\nwhile :; do :; done\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	d := NewProcessDriver(ProcessConfig{Executable: executable, Workspace: dir, MaxEventBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 50 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	_, err := d.Run(ctx, runtime.Turn{Prompt: "hello"}, &recordingSink{})
	if err != context.Canceled {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("cancellation took too long")
	}
}

type recordingBinder struct {
	mu     sync.Mutex
	events []string
}

func (b *recordingBinder) Bind(credential string) {
	b.mu.Lock()
	b.events = append(b.events, "bind:"+credential)
	b.mu.Unlock()
}

func (b *recordingBinder) Clear() {
	b.mu.Lock()
	b.events = append(b.events, "clear")
	b.mu.Unlock()
}

func callerContext(t *testing.T, credential string) context.Context {
	t.Helper()
	ctx, _ := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(map[string][]string{"authorization": {credential}}))
	return ctx
}

func TestProcessDriverBindsTheCallerCredentialPerTurn(t *testing.T) {
	dir := t.TempDir()
	decision := filepath.Join(dir, "decision")
	executable := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}'\nwhile [ ! -f \"$DECISION\" ]; do sleep 0.01; done\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}'\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	broker := &ApprovalBroker{requests: make(chan *PendingApprovalRequest, 1)}
	binder := &recordingBinder{}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: dir, Environment: []string{"DECISION=" + decision},
		MaxEventBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 100 * time.Millisecond,
		ApprovalBroker: broker, SettingsPath: filepath.Join(dir, "settings.json"), CallerCredentials: binder,
	})
	pending := newTestPending("approval-1", "call-1")
	broker.requests <- pending
	bridgeDecisionToFile(t, pending, decision)

	outcome, err := driver.Run(callerContext(t, "Bearer first-sender"), runtime.Turn{Prompt: "write"}, &recordingSink{})
	if err != nil || outcome.Pending == nil {
		t.Fatalf("Run() = %#v, %v", outcome, err)
	}
	if got := strings.Join(binder.events, ","); got != "bind:Bearer first-sender,clear" {
		t.Fatalf("a parked turn must hold no credential: binder events = %q", got)
	}
	outcome, err = outcome.Pending.Resume(callerContext(t, "Bearer second-sender"), &runtime.ApprovalDecision{ID: "approval-1", Approved: true}, &recordingSink{})
	if err != nil || outcome.Pending != nil || outcome.Failure != nil {
		t.Fatalf("Resume() = %#v, %v", outcome, err)
	}
	if got := strings.Join(binder.events, ","); got != "bind:Bearer first-sender,clear,bind:Bearer second-sender,clear" {
		t.Fatalf("the resuming call's credential must replace the first: binder events = %q", got)
	}
}

// A root harness carries its permission rules in Claude Code's managed
// settings, so an approval turn passes the prompt tool without --settings.
func TestArgsPassTheSettingsFileOnlyWhenThereIsOne(t *testing.T) {
	broker, err := NewApprovalBroker([]string{"production_db"}, 4096)
	require.NoError(t, err)
	t.Cleanup(func() { _ = broker.Close() })
	driver := NewProcessDriver(ProcessConfig{ApprovalBroker: broker, PermissionPromptTool: "mcp__kagent_hitl__approve"})
	args := driver.Args(runtime.Turn{Prompt: "test"})
	require.NotContains(t, args, "--settings")
	require.Subset(t, args, []string{"--permission-prompt-tool", "mcp__kagent_hitl__approve"})
}
func TestProcessDriverPassesTheTurnLimits(t *testing.T) {
	args := strings.Join(NewProcessDriver(ProcessConfig{Executable: "claude", Workspace: t.TempDir(), MaxBudgetUSD: "2.50", MaxTurns: 40}).Args(runtime.Turn{Prompt: "go"}), "\n") + "\n"
	for _, want := range []string{"--max-budget-usd\n2.50\n", "--max-turns\n40\n"} {
		if !strings.Contains(args, want) {
			t.Errorf("arguments lack %q: %s", strings.TrimSpace(want), args)
		}
	}
	if args := strings.Join(NewProcessDriver(ProcessConfig{Executable: "claude", Workspace: t.TempDir()}).Args(runtime.Turn{Prompt: "go"}), "\n"); strings.Contains(args, "--max-") {
		t.Fatalf("no limit configured, yet the arguments bound the turn: %s", args)
	}
}

func TestProcessDriverCompletesALimitedTurnDespiteTheExitStatus(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}' '{\"type\":\"result\",\"subtype\":\"error_max_turns\",\"is_error\":true,\"result\":\"turn limit\",\"total_cost_usd\":0.02,\"num_turns\":3,\"session_id\":\"11111111-1111-4111-8111-111111111111\"}'\nexit 1\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	d := NewProcessDriver(ProcessConfig{Executable: executable, Workspace: dir, MaxTurns: 3, MaxEventBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 50 * time.Millisecond})
	outcome, err := d.Run(t.Context(), runtime.Turn{Prompt: "go"}, &recordingSink{})
	if err != nil {
		t.Fatalf("Run() error = %v; a limit is not a crash", err)
	}
	if outcome.Failure != nil || outcome.StoppedBy != runtime.LimitTurns || outcome.Usage == nil || outcome.Usage.NumTurns != 3 {
		t.Fatalf("Run() outcome = %#v", outcome)
	}
	script = "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}' '{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"done\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}'\nexit 1\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Run(t.Context(), runtime.Turn{Prompt: "go"}, &recordingSink{}); err == nil {
		t.Fatal("a non-zero exit after a successful result is still an error")
	}
}
