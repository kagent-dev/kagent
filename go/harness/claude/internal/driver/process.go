// Package driver translates Claude Code's streaming process protocol into the
// runtime-neutral events consumed by the shared A2A executor.
package driver

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/harness/internal/utils"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

// ProcessConfig contains validated, compiler-owned inputs for one Claude Code
// process. Actor-owned paths and environment are supplied by the adapter.
type ProcessConfig struct {
	Executable         string
	ExpectedVersion    string
	StrictVersion      bool
	Workspace          string
	Model              string
	AppendSystemPrompt string
	AgentsJSON         string
	MCPConfigPath      string
	SettingsPath       string
	SkillRoot          string
	Environment        []string
	MaxEventBytes      int
	MaxStderrBytes     int
	InterruptGrace     time.Duration
	ApprovalBroker     *ApprovalBroker
}

// ProcessDriver supervises one Claude Code process per ordinary runtime turn
// and retains it while a synchronous approval hook awaits a decision.
type ProcessDriver struct {
	config ProcessConfig
	mu     sync.Mutex
	parked *processSession
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
	pending   *PendingHookRequest
	stopOnce  sync.Once
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
		"-p", turn.Prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--strict-mcp-config",
	}
	if d.config.ApprovalBroker == nil {
		// Bare mode has faster startup time
		args = append(args, "--bare", "--dangerously-skip-permissions")
	} else {
		// Bare mode disables hooks, including hooks explicitly supplied through
		// --settings. Keep setting discovery empty instead so the private
		// PreToolUse approval hook remains active without loading ambient config.
		args = append(args, "--setting-sources", "", "--settings", d.config.SettingsPath, "--permission-mode", "dontAsk")
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
		// Bare mode skips implicit skill discovery. --add-dir loads only the
		// compiler-selected skills materialized beneath SkillRoot/.claude/skills.
		args = append(args, "--add-dir", d.config.SkillRoot)
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
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.parked != nil {
		return d.resumeParked(ctx, turn, sink)
	}
	if strings.TrimSpace(turn.Prompt) == "" {
		return runtime.Outcome{}, fmt.Errorf("Claude prompt is required")
	}
	cmd := exec.Command(d.config.Executable, d.Args(turn)...)
	utils.ConfigureProcessGroup(cmd)
	cmd.Dir = d.config.Workspace
	cmd.Env = append([]string(nil), d.config.Environment...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return runtime.Outcome{}, fmt.Errorf("open Claude stdout: %w", err)
	}
	stderr := utils.NewBoundedBuffer(d.config.MaxStderrBytes)
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return runtime.Outcome{}, fmt.Errorf("start Claude: %w", err)
	}
	items := make(chan parseItem)
	stopEmit := make(chan struct{})
	go func() {
		defer close(items)
		parseErr := ParseJSONL(stdout, d.config.MaxEventBytes, func(event Event) error {
			select {
			case items <- parseItem{event: &event}:
				return nil
			case <-stopEmit:
				return context.Canceled
			}
		})
		select {
		case items <- parseItem{err: parseErr}:
		case <-stopEmit:
		}
	}()
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait(); close(waitDone) }()
	session := &processSession{
		command: cmd, items: items, stopEmit: stopEmit, wait: waitDone, stderr: stderr,
	}
	parked := false
	defer func() {
		if !parked {
			d.stopSession(session)
		}
	}()
	outcome, err := d.consume(ctx, session, sink)
	if err == nil && outcome.InputRequired != nil {
		d.parked, parked = session, true
	}
	return outcome, err
}

func (d *ProcessDriver) consume(ctx context.Context, session *processSession, sink runtime.EventSink) (runtime.Outcome, error) {
	var hookPending *PendingHookRequest
	for {
		if hookPending != nil && session.sessionID != "" {
			if !hookPending.waiting() {
				hookPending = nil
				continue
			}
			if hookPending.sessionID != session.sessionID {
				return runtime.Outcome{}, fmt.Errorf("Claude approval hook session does not match the active process")
			}
			session.pending = hookPending
			return runtime.Outcome{InputRequired: hookPending.approvalRequest()}, nil
		}
		var approvals <-chan *PendingHookRequest
		if d.config.ApprovalBroker != nil && hookPending == nil {
			approvals = d.config.ApprovalBroker.Requests()
		}
		select {
		case request := <-approvals:
			if session.terminal != nil {
				return runtime.Outcome{}, fmt.Errorf("Claude requested approval after its terminal result")
			}
			hookPending = request
		case item, ok := <-session.items:
			if !ok {
				return runtime.Outcome{}, fmt.Errorf("claude parser stopped without a result")
			}
			if item.event != nil {
				outcome, err := emitEvent(*item.event, sink, session.terminal != nil)
				if err == nil {
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
			return runtime.Outcome{}, ctx.Err()
		}
	}
}

func (d *ProcessDriver) resumeParked(ctx context.Context, turn runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
	session := d.parked
	decision, ok := turn.InputResponse.(*runtime.ApprovalDecision)
	if !ok {
		return runtime.Outcome{}, fmt.Errorf("Claude is waiting for a structured tool approval response")
	}
	if decision.ID != session.pending.request.ID {
		return runtime.Outcome{}, fmt.Errorf("tool approval response ID %q does not match pending ID %q", decision.ID, session.pending.request.ID)
	}
	if err := session.pending.resolve(*decision); err != nil {
		d.parked = nil
		d.stopSession(session)
		return runtime.Outcome{}, err
	}
	session.pending = nil
	outcome, err := d.consume(ctx, session, sink)
	if err == nil && outcome.InputRequired != nil {
		return outcome, nil
	}
	d.parked = nil
	d.stopSession(session)
	return outcome, err
}

// CancelParked denies the outstanding hook request and reaps the retained
// Claude process. Process termination remains the authority if the HTTP hook
// connection has already disappeared.
func (d *ProcessDriver) CancelParked(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.parked == nil {
		return fmt.Errorf("Claude has no parked turn to cancel")
	}
	session := d.parked
	d.parked = nil
	_ = session.pending.resolve(runtime.ApprovalDecision{
		ID: session.pending.request.ID, Approved: false, RejectionReason: "The task was canceled.",
	})
	d.stopSession(session)
	return nil
}

// Close releases the Actor-local hook listener and any retained process.
func (d *ProcessDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.parked != nil {
		d.stopSession(d.parked)
		d.parked = nil
	}
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
			return nil, sink.ToolCall(runtime.ToolCall{
				ID: event.ToolID, Name: event.ToolName, Arguments: event.Metadata,
			})
		case "completed":
			return nil, sink.ToolResult(runtime.ToolResult{
				ID: event.ToolID, Name: event.ToolName, Result: event.ToolResult, IsError: event.ToolError,
			})
		default:
			return nil, fmt.Errorf("claude tool activity has unsupported phase %q", event.ToolPhase)
		}
	case EventCompleted:
		return &runtime.Outcome{}, nil
	case EventFailed:
		return &runtime.Outcome{Failure: &runtime.Failure{Message: event.SafeMessage}}, nil
	default:
		return nil, fmt.Errorf("unsupported Claude event kind %q", event.Kind)
	}
}

func (d *ProcessDriver) stopSession(session *processSession) {
	session.stopOnce.Do(func() {
		close(session.stopEmit)
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
		for range session.items {
		}
	})
}
