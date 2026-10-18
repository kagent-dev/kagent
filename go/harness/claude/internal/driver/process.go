// Package driver translates Claude Code's streaming process protocol into the
// runtime-neutral events consumed by the shared A2A executor.
package driver

import (
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

	"github.com/kagent-dev/kagent/go/harness/internal/utils"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/harness/workspace"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"go.opentelemetry.io/otel/propagation"
)

// ProcessConfig contains validated, compiler-owned inputs for one Claude Code
// process. Actor-owned paths and environment are supplied by the adapter.
type ProcessConfig struct {
	Executable      string
	ExpectedVersion string
	StrictVersion   bool
	// Workspace is the working directory of a turn whose dispatch names none.
	Workspace          string
	Model              string
	AppendSystemPrompt string
	AgentsJSON         string
	MCPConfigPath      string
	SettingsPath       string
	// RunAs, when set, is the user Claude Code runs as.
	RunAs *Identity
	// PrepareCommand sets a session directory up as RunAs: the harness's own
	// executable with workspace.PrepareSubcommand. Required with RunAs.
	PrepareCommand       []string
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
	// CallerCredentials receives the caller's credential of each turn and is
	// cleared before the turn's outcome is returned, so a parked or suspended
	// Actor holds none.
	CallerCredentials CallerCredentialBinder
	// MCPForwarder fronts the MCP servers Claude reaches through the harness;
	// Close stops it.
	MCPForwarder *CredentialForwarder
	// TurnTimeout, when positive, ends every run of Claude between two inputs
	// of the person with a cap result once it has run this long.
	TurnTimeout time.Duration
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
}

func (s resumedEventSink) TextDelta(event runtime.TextDelta) error {
	if strings.TrimSpace(event.Text) == interruptedResponseWarning {
		return nil
	}
	return s.EventSink.TextDelta(event)
}

// Identity is a POSIX user and group.
type Identity struct {
	UID, GID uint32
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
	d.runAs(cmd)
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
	if d.config.SettingsPath != "" {
		// No --setting-sources: skills under --add-dir belong to the project
		// source, so closing the sources drops the compiled skills.
		args = append(args, "--settings", d.config.SettingsPath)
	}
	if d.config.ApprovalBroker != nil {
		args = append(args, "--permission-prompt-tool", d.config.PermissionPromptTool)
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

func (d *ProcessDriver) runAs(cmd *exec.Cmd) {
	if d.config.RunAs != nil {
		utils.RunAs(cmd, d.config.RunAs.UID, d.config.RunAs.GID)
	}
}

// Run supervises one Claude Code process and emits its ordered runtime events.
func (d *ProcessDriver) Run(ctx context.Context, turn runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
	if strings.TrimSpace(turn.Prompt) == "" {
		return runtime.Outcome{}, fmt.Errorf("Claude prompt is required")
	}
	defer d.bindCallerCredential(ctx)()
	bound := d.segmentBound(ctx)
	if bound.reached() {
		return runtime.Outcome{Stop: &bound.stop}, nil
	}
	message, err := userMessage(turn.Prompt)
	if err != nil {
		return runtime.Outcome{}, err
	}
	directory, err := d.prepareWorkspace(ctx, sink, turn.ContinuationID != "")
	if err != nil {
		return runtime.Outcome{}, err
	}
	// Claude's own git clones and fetches from the mirrors, which belong to
	// the volume's owner, not to Claude's user.
	// Its git and gh send placeholder credentials the egress gateway replaces
	// on a provider's host.
	dispatch := workspace.DispatchFrom(ctx)
	environment := traceEnvironment(ctx, workspace.WithCredentialPlaceholders(dispatch, workspace.TrustMirrors(dispatch, d.config.Environment)))
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
	d.runAs(cmd)
	cmd.Dir = directory
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
	if err := cmd.Start(); err != nil {
		return runtime.Outcome{}, fmt.Errorf("start Claude: %w", err)
	}
	items := make(chan parseItem)
	stopEmit := make(chan struct{})
	parseDone := make(chan struct{})
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
		// Wait closes pipes returned by StdoutPipe, so it must not run until the
		// parser has finished its final read.
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
			d.stopSession(session)
		}
	}()
	outcome, err := d.consume(ctx, session, sink, bound)
	// A pending outcome carries this same live session. Every other return path
	// leaves cleanup with this Run invocation.
	sessionOwnedByPendingTurn = err == nil && outcome.Pending != nil
	return d.withForwarderRefusals(outcome, err)
}

// withForwarderRefusals reports on an outcome what the MCP forwarder refused
// since the previous outcome. A turn that ends in an error reports nothing, so
// its refusals are reported with the next outcome rather than lost.
func (d *ProcessDriver) withForwarderRefusals(outcome runtime.Outcome, err error) (runtime.Outcome, error) {
	if err == nil && d.config.MCPForwarder != nil {
		outcome.ForwarderRefusals = d.config.MCPForwarder.TakeRefusals()
	}
	return outcome, err
}

// workspaceNotes is the file the workspace setup explains the session
// directory in: Claude Code reads it from its working directory on start.
const workspaceNotes = "CLAUDE.md"

// prepareWorkspace returns the directory the turn works in, the one its
// dispatch names, read per turn because a runtime restored from a snapshot
// predates the session's mounts. A session directory on a workspace volume is
// set up before its first turn; what could not be cloned or fetched, or a
// setup that failed, is the turn's first status message and the turn runs.
// A returning session, one that continues an earlier conversation, whose
// directory is set up again is told so. A resumed turn continues its process
// and never comes here.
func (d *ProcessDriver) prepareWorkspace(ctx context.Context, sink runtime.EventSink, returning bool) (string, error) {
	dispatch := workspace.DispatchFrom(ctx)
	if !filepath.IsAbs(dispatch.Directory) {
		return d.config.Workspace, nil
	}
	options := workspace.Options{Notes: workspaceNotes, Environment: d.config.Environment}
	var report workspace.Report
	var err error
	if d.config.RunAs != nil {
		report, err = d.prepareAsUser(ctx, dispatch, options)
	} else {
		report, err = workspace.Prepare(ctx, dispatch, options)
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if report.SetUp {
		logging.FromContext(ctx).DebugContext(ctx, "session directory set up", "directory", dispatch.Directory,
			"cloned", report.Cloned, "fetched", report.Fetched, "problems", len(report.Problems))
	}
	if notice := report.TurnNotice(err, returning); notice != "" {
		if err := sink.Notice(runtime.Notice{Text: notice}); err != nil {
			return "", err
		}
	}
	return dispatch.Directory, nil
}

// prepareAsUser sets a session directory up as Claude's user, who owns it
// and everything the setup writes: inside the sandbox root may not write
// another user's directory on a volume mount, and the user's files are what
// Claude works with. Root hands the directory over first, on every turn and
// whether or not the source mounts mirrors: the volume creates a missing
// session directory for the container's user, root, and Claude could not
// even enter it. The handover is best effort: a sub-path someone else created
// stays theirs, and the setup then reports what it could not do.
func (d *ProcessDriver) prepareAsUser(ctx context.Context, dispatch workspace.Dispatch, options workspace.Options) (workspace.Report, error) {
	if err := os.Lchown(dispatch.Directory, int(d.config.RunAs.UID), int(d.config.RunAs.GID)); err != nil {
		logging.FromContext(ctx).WarnContext(ctx, "the working directory stays with its owner", "directory", dispatch.Directory, "error", err)
	}
	if dispatch.Mirrors == "" {
		return workspace.Report{}, nil
	}
	if len(d.config.PrepareCommand) == 0 {
		return workspace.Report{}, fmt.Errorf("a harness that runs as root needs the command that prepares the workspace")
	}
	runAs := workspace.Identity{UID: d.config.RunAs.UID, GID: d.config.RunAs.GID}
	return workspace.PrepareAs(ctx, dispatch, options, &runAs, d.config.PrepareCommand...)
}

// bindCallerCredential hands the turn's caller credential and the hosts that
// receive the session Actor's JWT to the binder and returns the function that
// clears them. The credential lives in this process only while a turn runs; a
// parked turn resumes with the credential of the call that resumes it.
func (d *ProcessDriver) bindCallerCredential(ctx context.Context) func() {
	if d.config.CallerCredentials == nil {
		return func() {}
	}
	d.config.CallerCredentials.Bind(CallerCredential(ctx), ActorJWTHosts(ctx))
	return d.config.CallerCredentials.Clear
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

// segmentBound bounds the run of Claude that ctx starts now: by the time
// limit, and by the caller's bearer expiry when the harness forwards that
// bearer, since the MCP servers answer 401 once it has expired; a turn whose
// servers all receive the Actor's JWT instead forwards none. It is nil when
// nothing bounds the run.
func (d *ProcessDriver) segmentBound(ctx context.Context) *turnBound {
	var expiry time.Time
	if d.config.CallerCredentials != nil && d.config.CallerCredentials.TakesCallerCredential(ActorJWTHosts(ctx)) {
		expiry, _ = bearerExpiry(CallerCredential(ctx))
	}
	bound, ok := boundTurn(time.Now(), d.config.TurnTimeout, expiry)
	if !ok {
		return nil
	}
	return &bound
}

// reached reports whether a run bounded by b must not start.
func (b *turnBound) reached() bool {
	return b != nil && !time.Now().Before(b.at)
}

func (d *ProcessDriver) consume(ctx context.Context, session *processSession, sink runtime.EventSink, bound *turnBound) (runtime.Outcome, error) {
	var capped <-chan time.Time
	if bound != nil {
		timer := time.NewTimer(time.Until(bound.at))
		defer timer.Stop()
		capped = timer.C
	}
	var approvalPending *PendingApprovalRequest
	for {
		if approvalPending != nil && session.sessionID != "" {
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
				d.stopSession(session)
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
		case <-capped:
			// Interrupted, Claude keeps the conversation up to here, and the
			// next turn resumes it. A result Claude already reported, while a
			// backgrounded tool kept it running, keeps its usage; a failed one
			// stands.
			d.stopSession(session)
			if session.terminal != nil && session.terminal.Failure != nil {
				return *session.terminal, nil
			}
			outcome := runtime.Outcome{Stop: &bound.stop}
			if session.terminal != nil {
				outcome.Usage = session.terminal.Usage
			}
			return outcome, nil
		case <-ctx.Done():
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
			p.driver.stopSession(p.session)
		}
	}()
	decision, ok := response.(*runtime.ApprovalDecision)
	if !ok {
		return runtime.Outcome{}, fmt.Errorf("Claude is waiting for a structured tool approval response")
	}
	if decision.ID != p.pending.request.ID {
		return runtime.Outcome{}, fmt.Errorf("tool approval response ID %q does not match pending ID %q", decision.ID, p.pending.request.ID)
	}
	defer p.driver.bindCallerCredential(ctx)()
	bound := p.driver.segmentBound(ctx)
	if bound.reached() {
		_ = p.pending.resolve(runtime.ApprovalDecision{
			ID: p.pending.request.ID, Approved: false, RejectionReason: bound.stop.Message,
		})
		return runtime.Outcome{Stop: &bound.stop}, nil
	}
	if err := p.pending.resolve(*decision); err != nil {
		return runtime.Outcome{}, err
	}
	outcome, err := p.driver.consume(ctx, p.session, resumedEventSink{EventSink: sink}, bound)
	sessionOwnedByPendingTurn = err == nil && outcome.Pending != nil
	return p.driver.withForwarderRefusals(outcome, err)
}

// Cancel denies the outstanding permission MCP call and reaps the Claude process.
func (p *pendingTurn) Cancel(_ context.Context) error {
	_ = p.pending.resolve(runtime.ApprovalDecision{
		ID: p.pending.request.ID, Approved: false, RejectionReason: "The task was canceled.",
	})
	p.driver.stopSession(p.session)
	return nil
}

// Close releases the Actor-local loopback listeners. Pending process ownership is
// transferred to the runtime.PendingTurn returned by Run.
func (d *ProcessDriver) Close() error {
	var errs []error
	if d.config.ApprovalBroker != nil {
		errs = append(errs, d.config.ApprovalBroker.Close())
	}
	if d.config.MCPForwarder != nil {
		errs = append(errs, d.config.MCPForwarder.Close())
	}
	return errors.Join(errs...)
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
		return &runtime.Outcome{Usage: event.Usage}, nil
	case EventFailed:
		return &runtime.Outcome{Failure: &runtime.Failure{Message: event.SafeMessage}, Usage: event.Usage}, nil
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

var _ runtime.PendingTurn = (*pendingTurn)(nil)
