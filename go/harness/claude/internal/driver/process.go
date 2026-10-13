// Package driver translates Claude Code's streaming process protocol into the
// runtime-neutral events consumed by the shared A2A executor.
package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/harness/claude/config"
	"github.com/kagent-dev/kagent/go/harness/internal/utils"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"go.opentelemetry.io/otel/propagation"
)

// ProcessConfig contains validated, compiler-owned inputs for one Claude Code
// process. Actor-owned paths and environment are supplied by the adapter.
type ProcessConfig struct {
	Executable           string
	ExpectedVersion      string
	StrictVersion        bool
	Workspace            string
	Model                string
	AppendSystemPrompt   string
	AgentsJSON           string
	MCPConfigPath        string
	SettingsPath         string
	PermissionPromptTool string
	SkillRoot            string
	PluginDirs           []string
	Environment          []string
	MaxEventBytes        int
	MaxStderrBytes       int
	InterruptGrace       time.Duration
	ApprovalBroker       *ApprovalBroker
	// AwaitTelemetry holds each prompt until Claude Code telemetry has
	// initialized.
	AwaitTelemetry bool
}

// ProcessDriver supervises one Claude Code process per ordinary runtime turn
// and retains it while the permission-prompt MCP tool awaits a decision.
type ProcessDriver struct {
	config ProcessConfig
}

type parseItem struct {
	event *Event
	err   error
}

type processSession struct {
	command   *exec.Cmd
	items     <-chan parseItem
	stopEmit  chan struct{}
	wait      <-chan error
	stderr    *utils.BoundedBuffer
	terminal  *runtime.Outcome
	sessionID string
	stopOnce  sync.Once
	canceled  bool
}

// pendingTurn owns a Claude process blocked in the permission MCP hook. Resume
// resolves the hook and continues that process; Cancel terminates it.
type pendingTurn struct {
	driver  *ProcessDriver
	session *processSession
	pending *PendingApprovalRequest
}

const interruptedResponseWarning = "API Error: Connection lost mid-response. The response above may be incomplete."

// resumedEventSink removes Claude Code's synthetic connection warning after an
// intentional Actor pause. The live process and its provider stream are frozen
// while input is pending; Claude reports that interruption as assistant text
// when execution resumes even though it continues the tool call successfully.
// Keep the filter on the resume path so the same text remains visible if Claude
// emits it during an ordinary, uninterrupted turn.
type resumedEventSink struct {
	runtime.EventSink
	// declined is the call the user denied. Its result looks like a tool error,
	// so this is the only way to mark it as never run.
	declined  string
	resumedAt time.Time
}

func (s resumedEventSink) ToolResult(event runtime.ToolResult) error {
	if s.declined != "" && event.ID == s.declined {
		event.Status = runtime.ToolDeclined
	}
	return s.EventSink.ToolResult(event)
}

// ModelCall keeps a call that asked for a tool before the pause from counting as failed: Claude
// reports the stream the wait held open as a lost connection.
func (s resumedEventSink) ModelCall(call runtime.ModelCall) {
	if call.ParentToolID == "" && call.ErrorType == "server_error" && !call.End.IsZero() && call.End.Before(s.resumedAt) {
		call.StopReason, call.ErrorType = tracing.FinishReasonToolCall, ""
	}
	s.EventSink.ModelCall(call)
}

func (s resumedEventSink) TextDelta(event runtime.TextDelta) error {
	if strings.TrimSpace(event.Text) == interruptedResponseWarning {
		return nil
	}
	return s.EventSink.TextDelta(event)
}

// NewProcessDriver constructs a Claude Code process driver.
func NewProcessDriver(config ProcessConfig) *ProcessDriver {
	return &ProcessDriver{config: config}
}

// Validate checks that the configured executable is the pinned Claude version.
func (d *ProcessDriver) Validate(ctx context.Context) error {
	path, err := exec.LookPath(d.config.Executable)
	if err != nil {
		return fmt.Errorf("find Claude executable %q: %w", d.config.Executable, err)
	}
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Dir = d.config.Workspace
	cmd.Env = append([]string(nil), d.config.Environment...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("read Claude version: %w", err)
	}
	version := strings.TrimSpace(string(output))
	if d.config.StrictVersion && !strings.Contains(version, d.config.ExpectedVersion) {
		return fmt.Errorf("claude version mismatch: got %q, expected %q", version, d.config.ExpectedVersion)
	}
	return nil
}

// Args compiles one runtime turn into Claude Code command-line arguments.
func (d *ProcessDriver) Args(turn runtime.Turn) []string {
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--strict-mcp-config",
		"--dangerously-skip-permissions",
	}
	if d.config.ApprovalBroker != nil {
		args = append(args,
			"--setting-sources", "",
			"--settings", d.config.SettingsPath,
			"--permission-prompt-tool", d.config.PermissionPromptTool,
		)
	}
	if d.config.Model != "" {
		args = append(args, "--model", d.config.Model)
	}
	if d.config.AppendSystemPrompt != "" {
		args = append(args, "--append-system-prompt", d.config.AppendSystemPrompt)
	}
	if d.config.AgentsJSON != "" {
		args = append(args, "--agents", d.config.AgentsJSON)
	}
	if d.config.MCPConfigPath != "" {
		args = append(args, "--mcp-config", d.config.MCPConfigPath)
	}
	if d.config.SkillRoot != "" {
		// --add-dir exposes compiler-selected skills materialized beneath
		// SkillRoot/.claude/skills.
		args = append(args, "--add-dir", d.config.SkillRoot)
	}
	for _, dir := range d.config.PluginDirs {
		args = append(args, "--plugin-dir", dir)
	}
	if turn.ContinuationID != "" {
		// Resume the Actor's exact root conversation. --continue selects Claude's
		// latest session and can be redirected by subagents or interrupted attempts.
		args = append(args, "--resume", turn.ContinuationID)
	}
	return args
}

// Run supervises one Claude Code process and emits its ordered runtime events.
func (d *ProcessDriver) Run(ctx context.Context, turn runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
	if strings.TrimSpace(turn.Prompt) == "" {
		return runtime.Outcome{}, fmt.Errorf("Claude prompt is required")
	}
	message, err := userMessage(turn.Prompt)
	if err != nil {
		return runtime.Outcome{}, err
	}
	environment := traceEnvironment(ctx, d.config.Environment)
	var gate *tracingGate
	if d.config.AwaitTelemetry {
		if gate, err = newTracingGate(); err != nil {
			warnTelemetryNotReady(ctx, "port_unavailable", err)
		} else {
			environment = gate.environment(environment)
		}
	}
	cmd := exec.Command(d.config.Executable, d.Args(turn)...)
	utils.ConfigureProcessGroup(cmd)
	cmd.Dir = d.config.Workspace
	cmd.Env = environment
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return runtime.Outcome{}, fmt.Errorf("open Claude stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return runtime.Outcome{}, fmt.Errorf("open Claude stdout: %w", err)
	}
	stderr := utils.NewBoundedBuffer(d.config.MaxStderrBytes)
	cmd.Stderr = stderr
	// Read before Claude starts, since the process saves its own totals when it ends.
	startUsage, err := sessionUsage(d.config.Environment, turn.ContinuationID)
	if err != nil {
		logging.FromContext(ctx).WarnContext(ctx, "failed to read the Claude session usage, so this turn adds no tokens that only its totals show", "error", err)
	}
	if err := cmd.Start(); err != nil {
		return runtime.Outcome{}, fmt.Errorf("start Claude: %w", err)
	}
	items := make(chan parseItem)
	stopEmit := make(chan struct{})
	parseDone := make(chan struct{})
	go func() {
		defer close(items)
		parseErr := parseJSONL(ctx, stdout, d.config.MaxEventBytes, startUsage, func(event Event) error {
			select {
			case items <- parseItem{event: &event}:
				return nil
			case <-stopEmit:
				return context.Canceled
			}
		})
		// Wait closes pipes returned by StdoutPipe, so it runs only after the parser
		// returns; a read still in flight then fails and its goroutine exits.
		close(parseDone)
		select {
		case items <- parseItem{err: parseErr}:
		case <-stopEmit:
		}
	}()
	waitDone := make(chan error, 1)
	go func() {
		<-parseDone
		waitDone <- cmd.Wait()
		close(waitDone)
	}()
	session := &processSession{
		command: cmd, items: items, stopEmit: stopEmit, wait: waitDone, stderr: stderr,
	}
	go sendPrompt(ctx, stdin, message, gate, parseDone)
	sessionOwnedByPendingTurn := false
	defer func() {
		if !sessionOwnedByPendingTurn {
			d.stopSession(session, sink)
		}
	}()
	outcome, err := d.consume(ctx, session, sink)
	// A pending outcome carries this same live session. Every other return path
	// leaves cleanup with this Run invocation.
	sessionOwnedByPendingTurn = err == nil && outcome.Pending != nil
	return outcome, err
}

// sessionUsage reads the session totals Claude Code saved when its last process ended. A resumed
// process starts its result.modelUsage from these, so they are this process's baseline.
func sessionUsage(environment []string, sessionID string) (map[string]modelUsage, error) {
	usage := map[string]modelUsage{}
	if sessionID == "" {
		return usage, nil
	}
	var configDir string
	for _, entry := range environment {
		if value, ok := strings.CutPrefix(entry, config.ClaudeConfigDirEnvName+"="); ok {
			configDir = value
		}
	}
	if configDir == "" {
		return nil, fmt.Errorf("%s is not set", config.ClaudeConfigDirEnvName)
	}
	files, err := filepath.Glob(filepath.Join(configDir, "projects", "*", sessionID+".jsonl"))
	if err != nil {
		return nil, err
	}
	if len(files) != 1 {
		return nil, fmt.Errorf("found %d files for session %s, want 1", len(files), sessionID)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		return nil, err
	}
	for line := range bytes.Lines(raw) {
		if !bytes.Contains(line, []byte(`"cost-state"`)) {
			continue
		}
		var state struct {
			Type       string                `json:"type"`
			ModelUsage map[string]modelUsage `json:"modelUsage"`
		}
		if json.Unmarshal(line, &state) == nil && state.Type == "cost-state" && state.ModelUsage != nil {
			usage = state.ModelUsage
		}
	}
	return usage, nil
}

// traceEnvironment injects the trace context into the environment variables.
// Claude reads it once at startup, so a turn that is resumed after an approval
// keeps emitting under the trace of the request that started the process. The
// resumed A2A segment records a link to that origin rather than reparenting it.
func traceEnvironment(ctx context.Context, environment []string) []string {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	result := make([]string, 0, len(environment)+2)
	// First remove the trace context variables from the environment.
	for _, variable := range environment {
		if strings.HasPrefix(variable, "TRACEPARENT=") || strings.HasPrefix(variable, "TRACESTATE=") {
			continue
		}
		result = append(result, variable)
	}
	if traceparent := carrier.Get("traceparent"); traceparent != "" {
		result = append(result, "TRACEPARENT="+traceparent)
	}
	if tracestate := carrier.Get("tracestate"); tracestate != "" {
		result = append(result, "TRACESTATE="+tracestate)
	}
	return result
}

// userMessage encodes a prompt as one Claude Code stream-JSON input line.
func userMessage(prompt string) ([]byte, error) {
	type content struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	message, err := json.Marshal(struct {
		Type    string  `json:"type"`
		Message content `json:"message"`
	}{Type: "user", Message: content{Role: "user", Content: prompt}})
	if err != nil {
		return nil, fmt.Errorf("encode Claude prompt: %w", err)
	}
	return append(message, '\n'), nil
}

// sendPrompt writes the prompt once the gate opens, then ends the input as a
// prompt argument would, since Claude holds its result for background agents
// only once its input has ended. A gate that times out still sends the
// prompt, trading the turn's native telemetry for the turn.
func sendPrompt(ctx context.Context, stdin io.WriteCloser, message []byte, gate *tracingGate, exited <-chan struct{}) {
	defer stdin.Close()
	if gate != nil {
		err := gate.wait(ctx, exited)
		if errors.Is(err, errProcessExited) || ctx.Err() != nil {
			return
		}
		if err != nil {
			warnTelemetryNotReady(ctx, "timeout", err)
		}
	}
	// A failed write means Claude has exited, which consume reports.
	_, _ = stdin.Write(message)
}

func (d *ProcessDriver) consume(ctx context.Context, session *processSession, sink runtime.EventSink) (runtime.Outcome, error) {
	var approvalPending *PendingApprovalRequest
	observedToolCalls := make(map[string]struct{})
	for {
		// MCP can deliver an approval before stdout parsing catches up. Park only after
		// the matching tool_use has been observed so the call boundary is timestamped first.
		approvalToolObserved := false
		if approvalPending != nil {
			_, approvalToolObserved = observedToolCalls[approvalPending.request.CallID]
		}
		if approvalPending != nil && session.sessionID != "" && approvalToolObserved {
			if !approvalPending.waiting() {
				approvalPending = nil
				continue
			}
			return runtime.Outcome{Pending: &pendingTurn{
				driver: d, session: session, pending: approvalPending,
			}}, nil
		}
		var approvals <-chan *PendingApprovalRequest
		if d.config.ApprovalBroker != nil && approvalPending == nil {
			approvals = d.config.ApprovalBroker.Requests()
		}
		select {
		case request := <-approvals:
			if session.terminal != nil {
				return runtime.Outcome{}, fmt.Errorf("Claude requested approval after its terminal result")
			}
			approvalPending = request
		case item, ok := <-session.items:
			if !ok {
				return runtime.Outcome{}, fmt.Errorf("claude parser stopped without a result")
			}
			if item.event != nil {
				outcome, err := emitEvent(*item.event, sink, session.terminal != nil)
				if err == nil {
					if item.event.Kind == EventToolActivity && item.event.ToolPhase == "started" {
						observedToolCalls[item.event.ToolID] = struct{}{}
					}
					if item.event.Kind == EventSessionStarted {
						if session.sessionID != "" && session.sessionID != item.event.SessionID {
							return runtime.Outcome{}, fmt.Errorf("Claude changed session ID during an active process")
						}
						session.sessionID = item.event.SessionID
					}
					if outcome != nil {
						session.terminal = outcome
					}
					continue
				}
				return runtime.Outcome{}, err
			}
			if item.err != nil {
				// A process that exits before its result can explain the failure
				// only on stderr. Reap it to finish draining stderr; malformed
				// output can also stop the parser while the process is still alive.
				d.stopSession(session, sink)
				if stderr := strings.TrimSpace(session.stderr.String()); stderr != "" {
					return runtime.Outcome{}, fmt.Errorf("%w: %s", item.err, stderr)
				}
				return runtime.Outcome{}, item.err
			}
			if waitErr := <-session.wait; waitErr != nil {
				return runtime.Outcome{}, fmt.Errorf("claude exited with an error: %w: %s", waitErr, session.stderr.String())
			}
			if session.terminal == nil {
				return runtime.Outcome{}, fmt.Errorf("claude process exited without a terminal result")
			}
			return *session.terminal, nil
		case <-ctx.Done():
			session.canceled = true
			return runtime.Outcome{}, ctx.Err()
		}
	}
}

func (p *pendingTurn) Request() runtime.InputRequest { return p.pending.approvalRequest() }

// Resume resolves the permission MCP call and continues consuming the same process
// until it completes, fails, or returns another PendingTurn.
func (p *pendingTurn) Resume(ctx context.Context, response runtime.InputResponse, sink runtime.EventSink) (runtime.Outcome, error) {
	sessionOwnedByPendingTurn := false
	defer func() {
		if !sessionOwnedByPendingTurn {
			p.driver.stopSession(p.session, sink)
		}
	}()
	decision, ok := response.(*runtime.ApprovalDecision)
	if !ok {
		return runtime.Outcome{}, fmt.Errorf("Claude is waiting for a structured tool approval response")
	}
	if decision.ID != p.pending.request.ID {
		return runtime.Outcome{}, fmt.Errorf("tool approval response ID %q does not match pending ID %q", decision.ID, p.pending.request.ID)
	}
	resumedAt := time.Now()
	if err := p.pending.resolve(*decision); err != nil {
		return runtime.Outcome{}, err
	}
	resumed := resumedEventSink{EventSink: sink, resumedAt: resumedAt}
	if !decision.Approved {
		resumed.declined = p.pending.request.CallID
	}
	outcome, err := p.driver.consume(ctx, p.session, resumed)
	sessionOwnedByPendingTurn = err == nil && outcome.Pending != nil
	return outcome, err
}

// Cancel denies the outstanding permission MCP call and reaps the Claude process.
func (p *pendingTurn) Cancel(_ context.Context, sink runtime.ModelCallSink) error {
	_ = p.pending.resolve(runtime.ApprovalDecision{
		ID: p.pending.request.ID, Approved: false, RejectionReason: "The task was canceled.",
	})
	p.session.canceled = true
	p.driver.stopSession(p.session, sink)
	return nil
}

// Close releases the Actor-local permission MCP listener. Pending process ownership is
// transferred to the runtime.PendingTurn returned by Run.
func (d *ProcessDriver) Close() error {
	if d.config.ApprovalBroker != nil {
		return d.config.ApprovalBroker.Close()
	}
	return nil
}

// emitEvent translates a Claude event to a runtime event and emits it to the
// provided event sink, which is then consumed by the shared A2A executor.
func emitEvent(event Event, sink runtime.EventSink, terminal bool) (*runtime.Outcome, error) {
	if terminal {
		return nil, fmt.Errorf("claude emitted activity after its terminal result")
	}
	switch event.Kind {
	case EventSessionStarted:
		return nil, sink.SessionStarted(runtime.SessionStarted{ContinuationID: event.SessionID})
	case EventTextDelta:
		return nil, sink.TextDelta(runtime.TextDelta{Text: event.Text})
	case EventToolActivity:
		switch event.ToolPhase {
		case "started":
			call := runtime.ToolCall{ID: event.ToolID, Name: event.ToolName, Arguments: event.Metadata, ParentToolID: event.ParentToolID}
			call.MCPServer, call.MCPTool = splitMCPName(event.ToolName)
			return nil, sink.ToolCall(call)
		case "completed":
			return nil, sink.ToolResult(runtime.ToolResult{
				ID: event.ToolID, Name: event.ToolName, Result: event.ToolResult, IsError: event.ToolError,
			})
		default:
			return nil, fmt.Errorf("claude tool activity has unsupported phase %q", event.ToolPhase)
		}
	case EventModelCall:
		sink.ModelCall(*event.ModelCall)
		return nil, nil
	case EventCompleted:
		return &runtime.Outcome{}, nil
	case EventFailed:
		return &runtime.Outcome{Failure: &runtime.Failure{Message: event.SafeMessage}}, nil
	default:
		return nil, fmt.Errorf("unsupported Claude event kind %q", event.Kind)
	}
}

func (d *ProcessDriver) stopSession(session *processSession, sink runtime.ModelCallSink) {
	session.stopOnce.Do(func() {
		drained := make(chan struct{})
		go func() {
			defer close(drained)
			for item := range session.items {
				if item.event != nil && item.event.ModelCall != nil {
					call := *item.event.ModelCall
					call.Canceled = session.canceled && call.UsagePartial
					sink.ModelCall(call)
				}
			}
		}()
		_ = utils.InterruptProcessGroup(session.command.Process)
		timer := time.NewTimer(d.config.InterruptGrace)
		defer timer.Stop()
		select {
		case <-session.wait:
			// The group leader can exit on the interrupt while a descendant that
			// ignores it remains alive. Kill any processes still in the group.
			_ = utils.KillProcessGroup(session.command.Process)
		case <-timer.C:
			_ = utils.KillProcessGroup(session.command.Process)
			<-session.wait
		}
		<-drained
		close(session.stopEmit)
	})
}

var _ runtime.PendingTurn = (*pendingTurn)(nil)

// splitMCPName splits Claude's mcp__<server>__<tool> name at the first "__". Agent server
// names come from Kubernetes names, which never contain "__"; plugin servers are best effort.
func splitMCPName(name string) (server, tool string) {
	rest, ok := strings.CutPrefix(name, "mcp__")
	if !ok {
		return "", ""
	}
	server, tool, ok = strings.Cut(rest, "__")
	if !ok || server == "" || tool == "" {
		return "", ""
	}
	return server, tool
}
