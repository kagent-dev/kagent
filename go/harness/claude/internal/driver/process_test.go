package driver

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"go.opentelemetry.io/otel/trace"

	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/harness/workspace"
)

type recordingSink struct {
	sessions []runtime.SessionStarted
	notices  []runtime.Notice
	text     strings.Builder
	models   []runtime.ModelCall
	calls    []runtime.ToolCall
	results  []runtime.ToolResult
	onText   func()
}

func (s *recordingSink) SessionStarted(event runtime.SessionStarted) error {
	s.sessions = append(s.sessions, event)
	return nil
}
func (s *recordingSink) Notice(event runtime.Notice) error {
	s.notices = append(s.notices, event)
	return nil
}
func (s *recordingSink) TextDelta(event runtime.TextDelta) error {
	s.text.WriteString(event.Text)
	if s.onText != nil {
		s.onText()
	}
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
func (s *recordingSink) ModelCall(event runtime.ModelCall) { s.models = append(s.models, event) }

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
	d := NewProcessDriver(ProcessConfig{Executable: executable, ExpectedVersion: pinnedClaudeVersion, StrictVersion: true, Workspace: dir, Model: "claude-test", AppendSystemPrompt: "extra", AgentsJSON: agentsJSON, MCPConfigPath: mcpConfigPath, PluginDirs: []string{skillRoot, filepath.Join(dir, "plugin-a")}, Environment: []string{"CAPTURE=" + capture}, MaxEventBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: time.Second})
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
	if !strings.Contains(string(args), "--plugin-dir\n"+skillRoot+"\n") {
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

// Claude Code works in the directory each turn's dispatch names, its
// configured workspace without one; a session directory with mirrors is set
// up before the turn, and a fetch its origin refuses is the turn's notice
// while the turn still runs.
func TestProcessDriverWorksInTheDispatchedDirectory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	capture := filepath.Join(dir, "pwd")
	executable := filepath.Join(dir, "claude")
	script := "#!/bin/sh\npwd > \"$CAPTURE\"\nprintf '%s\\n' \"$GIT_CONFIG_KEY_0=$GIT_CONFIG_VALUE_0\" > \"$CAPTURE.git\"\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}' '{\"type\":\"result\",\"subtype\":\"success\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}'\ncat >/dev/null\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	configured := filepath.Join(dir, "data-workspace")
	sessionDirectory := filepath.Join(dir, "session")
	mirrors := filepath.Join(dir, "mirrors")
	for _, directory := range []string{configured, sessionDirectory} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A mirror whose origin is gone: its fetch is refused.
	origin := filepath.Join(dir, "origin")
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=main", origin},
		{"-C", origin, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--quiet", "--allow-empty", "--message", "initial"},
		{"clone", "--quiet", "--mirror", "file://" + origin, filepath.Join(mirrors, "o", "r.git")},
	} {
		if output, err := exec.CommandContext(t.Context(), "git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	if err := os.RemoveAll(origin); err != nil {
		t.Fatal(err)
	}
	d := NewProcessDriver(ProcessConfig{Executable: executable, Workspace: configured, Environment: []string{"CAPTURE=" + capture}, MaxEventBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: time.Second})
	for _, test := range []struct {
		name       string
		params     map[string][]string
		want       string
		wantNotice string
		// wantGit is the git configuration Claude's environment adds.
		wantGit string
	}{
		{name: "no dispatch", want: configured},
		{name: "durable directory", params: map[string][]string{kagenta2a.WorkingDirectoryHeader: {configured}}, want: configured},
		{
			name: "session directory",
			params: map[string][]string{
				kagenta2a.WorkingDirectoryHeader: {sessionDirectory}, kagenta2a.MirrorsDirectoryHeader: {mirrors}, kagenta2a.ChangedRepositoriesHeader: {"o/r"},
			},
			want: sessionDirectory, wantNotice: "o/r: the fetch from origin failed",
			wantGit: "safe.directory=" + filepath.Join(mirrors, "*"),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			if test.params != nil {
				ctx, _ = a2asrv.NewCallContext(ctx, a2asrv.NewServiceParams(test.params))
			}
			sink := &recordingSink{}
			if _, err := d.Run(ctx, runtime.Turn{Prompt: "hello"}, sink); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			pwd, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(pwd)); got != test.want {
				t.Errorf("Claude's working directory = %q, want %q", got, test.want)
			}
			gitConfig, err := os.ReadFile(capture + ".git")
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSuffix(strings.TrimSpace(string(gitConfig)), "="); got != test.wantGit {
				t.Errorf("Claude's git configuration = %q, want %q", got, test.wantGit)
			}
			if test.wantNotice == "" {
				if len(sink.notices) != 0 {
					t.Errorf("notices = %#v, want none", sink.notices)
				}
				return
			}
			if len(sink.notices) != 1 || !strings.Contains(sink.notices[0].Text, test.wantNotice) || sink.notices[0].Kind != kagenta2a.WorkspaceSetupNotice {
				t.Errorf("notices = %#v, want one workspace-setup notice with %q", sink.notices, test.wantNotice)
			}
			for _, path := range []string{"o/r/.git/objects/info/alternates", workspaceNotes} {
				if _, err := os.Stat(filepath.Join(sessionDirectory, path)); err != nil {
					t.Errorf("the session directory misses %s: %v", path, err)
				}
			}
			// The next turn finds the directory set up, retries the refused
			// fetch alone, and reports it again.
			sink = &recordingSink{}
			if _, err := d.Run(ctx, runtime.Turn{Prompt: "again"}, sink); err != nil {
				t.Fatalf("second Run() error = %v", err)
			}
			if len(sink.notices) != 1 || !strings.Contains(sink.notices[0].Text, test.wantNotice) || strings.Contains(sink.notices[0].Text, workspace.FreshDirectoryNotice) {
				t.Errorf("second turn notices = %#v, want one with %q", sink.notices, test.wantNotice)
			}
		})
	}
}

// A returning session, one that continues an earlier conversation, whose
// directory has no setup record, such as after the volume's owner cleaned it
// up, is told in its first status message that the directory was set up
// again; a session's first-ever turn and a later turn are not.
func TestProcessDriverTellsAReturningSessionOfAFreshDirectory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	executable := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}' '{\"type\":\"result\",\"subtype\":\"success\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}'\ncat >/dev/null\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	origin := filepath.Join(dir, "origin")
	mirrors := filepath.Join(dir, "mirrors")
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=main", origin},
		{"-C", origin, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--quiet", "--allow-empty", "--message", "initial"},
		{"clone", "--quiet", "--mirror", "file://" + origin, filepath.Join(mirrors, "o", "r.git")},
	} {
		if output, err := exec.CommandContext(t.Context(), "git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	d := NewProcessDriver(ProcessConfig{Executable: executable, Workspace: filepath.Join(dir, "data-workspace"), MaxEventBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: time.Second})
	const continuation = "11111111-1111-4111-8111-111111111111"
	for _, test := range []struct {
		name  string
		turns []runtime.Turn
		// want is the first status message of each turn, empty for none.
		want []string
	}{
		{
			name:  "first-ever turn",
			turns: []runtime.Turn{{Prompt: "hello"}, {Prompt: "again", ContinuationID: continuation}},
			want:  []string{"", ""},
		},
		{
			name:  "returning session",
			turns: []runtime.Turn{{Prompt: "back", ContinuationID: continuation}, {Prompt: "again", ContinuationID: continuation}},
			want:  []string{workspace.FreshDirectoryNotice, ""},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			// The volume creates a missing session directory empty.
			sessionDirectory := t.TempDir()
			ctx, _ := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(map[string][]string{
				kagenta2a.WorkingDirectoryHeader: {sessionDirectory}, kagenta2a.MirrorsDirectoryHeader: {mirrors},
			}))
			for i, turn := range test.turns {
				sink := &recordingSink{}
				if _, err := d.Run(ctx, turn, sink); err != nil {
					t.Fatalf("turn %d: Run() error = %v", i, err)
				}
				var got []string
				for _, notice := range sink.notices {
					got = append(got, notice.Text)
				}
				if test.want[i] == "" && len(got) != 0 {
					t.Errorf("turn %d: notices = %q, want none", i, got)
				}
				if test.want[i] != "" && !slices.Equal(got, []string{test.want[i]}) {
					t.Errorf("turn %d: notices = %q, want %q", i, got, test.want[i])
				}
			}
			if _, err := os.Stat(filepath.Join(sessionDirectory, workspace.StateFile)); err != nil {
				t.Errorf("the setup is not recorded: %v", err)
			}
		})
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

func TestProcessDriverCancellationKeepsTheOpenModelCall(t *testing.T) {
	dir, executable := writeFakeClaude(t, `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"11111111-1111-4111-8111-111111111111"}'
printf '%s\n' '{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_1","model":"claude-opus-5-5","usage":{"input_tokens":5}}}}'
printf '%s\n' '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}}'
exec sleep 30
`)
	d := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: dir,
		MaxEventBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 50 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(t.Context())
	sink := &recordingSink{onText: cancel}
	if _, err := d.Run(ctx, runtime.Turn{Prompt: "hello"}, sink); err != context.Canceled {
		t.Fatalf("Run() error = %v, want only context canceled", err)
	}
	if len(sink.models) != 1 || !sink.models[0].UsagePartial || sink.models[0].InputTokens != 5 || !sink.models[0].Canceled {
		t.Fatalf("model calls = %#v, want one canceled partial call", sink.models)
	}
}

func TestProcessDriverCrashMarksTheOpenModelCallAnError(t *testing.T) {
	dir, executable := writeFakeClaude(t, `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"11111111-1111-4111-8111-111111111111"}'
printf '%s\n' '{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_1","model":"claude-opus-5-5","usage":{"input_tokens":5}}}}'
exit 1
`)
	d := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: dir,
		MaxEventBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 50 * time.Millisecond,
	})
	sink := &recordingSink{}
	if _, err := d.Run(t.Context(), runtime.Turn{Prompt: "hello"}, sink); err == nil {
		t.Fatal("Run() succeeded, want the crash reported")
	}
	if len(sink.models) != 1 || !sink.models[0].UsagePartial || sink.models[0].Canceled || sink.models[0].StopReason != "error" {
		t.Fatalf("model calls = %#v, want one failed partial call", sink.models)
	}
}

func writeFakeClaude(t *testing.T, script string) (dir, executable string) {
	t.Helper()
	dir = t.TempDir()
	executable = filepath.Join(dir, "claude")
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir, executable
}

func TestToolCallSplitsTheMCPName(t *testing.T) {
	for _, tc := range []struct {
		name, server, tool string
	}{
		{name: "mcp__kagent__k8s_get", server: "kagent", tool: "k8s_get"},
		{name: "mcp__kagent__k8s__get", server: "kagent", tool: "k8s__get"},
		{name: "mcp__kagent_tools__write", server: "kagent_tools", tool: "write"},
		{name: "Bash"},
		{name: "mcp__kagent"},
		{name: "mcp____write"},
		{name: "mcp__kagent__"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			if _, err := emitEvent(Event{Kind: EventToolActivity, ToolID: "toolu_1", ToolName: tc.name, ToolPhase: "started"}, sink, false); err != nil {
				t.Fatal(err)
			}
			if len(sink.calls) != 1 {
				t.Fatalf("calls = %+v, want one", sink.calls)
			}
			if call := sink.calls[0]; call.Name != tc.name || call.MCPServer != tc.server || call.MCPTool != tc.tool {
				t.Errorf("call = %+v, want server %q and tool %q", call, tc.server, tc.tool)
			}
		})
	}
}
