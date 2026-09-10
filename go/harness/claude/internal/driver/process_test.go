package driver

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
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
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo '2.1.260 (Claude Code)'; exit 0; fi\nprintf '%s\\n' \"$@\" > \"$CAPTURE\"\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}' '{\"type\":\"result\",\"subtype\":\"success\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}'\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	agentsJSON := `{"reviewer":{"description":"Reviews changes","prompt":"Review carefully","tools":["Read"]}}`
	mcpConfigPath := filepath.Join(dir, "mcp.json")
	skillRoot := filepath.Join(dir, "generated-skills")
	d := NewProcessDriver(ProcessConfig{Executable: executable, ExpectedVersion: pinnedClaudeVersion, StrictVersion: true, Workspace: dir, Model: "claude-test", AppendSystemPrompt: "extra", AgentsJSON: agentsJSON, MCPConfigPath: mcpConfigPath, SkillRoot: skillRoot, Environment: []string{"CAPTURE=" + capture}, MaxEventBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: time.Second})
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
	for _, required := range []string{
		"--bare\n",
		"--dangerously-skip-permissions\n",
		"--strict-mcp-config\n",
		"--tools\n" + bareBuiltinTools + "\n",
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
		t.Error("arguments do not expose compiler-owned skills to bare mode")
	}
	if strings.Contains(string(args), "--permission-prompt-tool\n") {
		t.Error("arguments unexpectedly configure Claude's native permission bridge")
	}
	if len(sink.sessions) != 1 || sink.sessions[0].ContinuationID != turn.ContinuationID {
		t.Errorf("session events = %#v", sink.sessions)
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
