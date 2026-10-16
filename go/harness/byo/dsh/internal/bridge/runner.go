// Package bridge supervises a pinned DSH ACP process behind the existing A2A executor.
package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/config"
	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/supervisor"
	"github.com/kagent-dev/kagent/go/harness/internal/utils"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	runtimea2a "github.com/kagent-dev/kagent/go/harness/runtime/a2a"
)

const (
	setupTimeout = 30 * time.Second
	stopTimeout  = 5 * time.Second
	promptID     = "3"
)

type Runner struct {
	config       *config.Config
	executable   string
	dataDir      string
	workspace    string
	environment  []string
	servers      []config.MCPServer
	approvalMu   sync.Mutex
	approvalBusy atomic.Bool
}

var _ runtimea2a.Runner = (*Runner)(nil)

func New(cfg *config.Config, executable, dataDir string, environment []string) (*Runner, error) {
	if cfg == nil || !filepath.IsAbs(dataDir) {
		return nil, fmt.Errorf("configuration and an absolute data directory are required")
	}
	for _, path := range []string{dataDir, filepath.Join(dataDir, "workspace"), filepath.Join(dataDir, "dsh", "sessions"), filepath.Join(dataDir, "dsh", "storages")} {
		if err := utils.EnsurePrivateDir(path); err != nil {
			return nil, err
		}
	}
	servers, err := cfg.Bindings(environment)
	if err != nil {
		return nil, err
	}
	return &Runner{config: cfg, executable: executable, dataDir: dataDir, workspace: filepath.Join(dataDir, "workspace"), environment: append([]string(nil), environment...), servers: servers}, nil
}

func (r *Runner) Validate(ctx context.Context) error {
	command, err := supervisor.Command(r.executable, "--version")
	if err != nil {
		return err
	}
	command.Env = r.environment
	var output bytes.Buffer
	command.Stdout = &output
	control, err := supervisor.Start(command)
	if err != nil {
		return fmt.Errorf("failed to start DSH version check: %w", err)
	}
	defer control.Close()
	done := make(chan error, 1)
	go func() { done <- control.Wait() }()
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = control.Close()
		err = errors.Join(ctx.Err(), <-done)
	}
	if err != nil {
		return fmt.Errorf("failed to check DSH version: %w", err)
	}
	if strings.TrimSpace(output.String()) != config.Version {
		return fmt.Errorf("DSH BYO requires version %s", config.Version)
	}
	return nil
}

type process struct {
	stdin       io.WriteCloser
	control     io.Closer
	wire        *wire
	done        chan struct{}
	waitErr     error
	home        string
	events      events
	mcp         *mcpProxy
	runner      *Runner
	cleanupOnce sync.Once
	cleanupErr  error
}

func (r *Runner) start(ctx context.Context) (*process, error) {
	home, err := os.MkdirTemp("", "kagent-dsh-")
	if err != nil {
		return nil, fmt.Errorf("failed to create DSH launch directory: %w", err)
	}
	patch, err := r.config.Patch(r.dataDir)
	if err != nil {
		return nil, errors.Join(err, os.RemoveAll(home))
	}
	path := filepath.Join(home, "compiled.patch.json")
	if err := utils.ReplacePrivateFile(path, patch); err != nil {
		return nil, errors.Join(err, os.RemoveAll(home))
	}
	cmd, err := supervisor.Command(r.executable, "--profile", "acp", "--patch", path)
	if err != nil {
		return nil, errors.Join(err, os.RemoveAll(home))
	}
	// A nil stderr connects directly to the null device, without a copy
	// goroutine that descendants could keep blocked after supervisor death.
	cmd.Dir, cmd.Env = r.workspace, config.Environment(r.environment, home)
	utils.ConfigureProcessGroup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to open DSH stdin: %w", err), os.RemoveAll(home))
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to open DSH stdout: %w", err), stdin.Close(), os.RemoveAll(home))
	}
	control, err := supervisor.Start(cmd)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to start DSH: %w", err), stdin.Close(), stdout.Close(), os.RemoveAll(home))
	}
	p := &process{stdin: stdin, control: control, wire: newWire(stdin, stdout), done: make(chan struct{}), home: home, runner: r, events: events{tools: make(map[string]tool)}}
	go func() { p.waitErr = control.Wait(); close(p.done) }()
	proxy, err := startMCP(ctx, r.config, r.servers)
	if err != nil {
		return nil, errors.Join(err, p.stop())
	}
	p.mcp = proxy
	return p, nil
}

func (r *Runner) Run(ctx context.Context, turn runtime.Turn, sink runtime.EventSink) (out runtime.Outcome, returnErr error) {
	return r.run(ctx, turn, sink, "", nil)
}

type turnNotStartedError struct{ error }

func (e *turnNotStartedError) Unwrap() error { return e.error }

type processCleanupError struct{ error }

func (e *processCleanupError) Unwrap() error { return e.error }

func (r *Runner) run(ctx context.Context, turn runtime.Turn, sink runtime.EventSink, resolvedApproval string, resolvedResult *runtime.ToolResult) (out runtime.Outcome, returnErr error) {
	started := false
	defer func() {
		var cleanupErr *processCleanupError
		if returnErr != nil && !started && !errors.As(returnErr, &cleanupErr) {
			returnErr = &turnNotStartedError{returnErr}
		}
	}()
	if err := r.admitApprovalTurn(ctx, resolvedApproval); err != nil {
		return out, err
	}
	if strings.TrimSpace(turn.Prompt) == "" {
		return out, fmt.Errorf("DSH prompt is required")
	}
	p, err := r.start(ctx)
	if err != nil {
		return out, err
	}
	defer func() {
		if out.Pending == nil {
			returnErr = errors.Join(returnErr, p.stop())
		}
	}()
	setupCtx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()
	result, err := p.wire.call(setupCtx, "1", "initialize", struct {
		ProtocolVersion    int      `json:"protocolVersion"`
		ClientCapabilities struct{} `json:"clientCapabilities"`
	}{ProtocolVersion: 1})
	if err != nil {
		return out, err
	}
	var init struct {
		ProtocolVersion   int `json:"protocolVersion"`
		AgentCapabilities struct {
			MCP struct {
				HTTP bool `json:"http"`
			} `json:"mcpCapabilities"`
			Sessions struct {
				Resume json.RawMessage `json:"resume"`
			} `json:"sessionCapabilities"`
		} `json:"agentCapabilities"`
	}
	if err := json.Unmarshal(result, &init); err != nil {
		return out, fmt.Errorf("failed to decode ACP initialization: %w", err)
	}
	if init.ProtocolVersion != 1 || (len(r.config.MCP) > 0 && !init.AgentCapabilities.MCP.HTTP) {
		return out, fmt.Errorf("DSH ACP does not advertise required protocol capabilities")
	}
	method := "session/new"
	if turn.ContinuationID != "" {
		if len(init.AgentCapabilities.Sessions.Resume) == 0 {
			return out, fmt.Errorf("DSH ACP does not advertise session resume")
		}
		method = "session/resume"
	}
	servers := p.mcp.servers
	if servers == nil {
		servers = []config.MCPServer{}
	}
	result, err = p.wire.call(setupCtx, "2", method, struct {
		SessionID  string             `json:"sessionId,omitempty"`
		CWD        string             `json:"cwd"`
		MCPServers []config.MCPServer `json:"mcpServers"`
	}{SessionID: turn.ContinuationID, CWD: r.workspace, MCPServers: servers})
	if err != nil {
		return out, err
	}
	id := turn.ContinuationID
	if id == "" {
		var created struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(result, &created); err != nil {
			return out, fmt.Errorf("failed to decode ACP session: %w", err)
		}
		id = created.SessionID
	}
	if err := ValidateSessionID(id); err != nil {
		return out, err
	}
	p.events.sessionID = id
	if err := sink.SessionStarted(runtime.SessionStarted{ContinuationID: id}); err != nil {
		return out, err
	}
	// After publishing the saved result or attempting the prompt write, a retry
	// cannot prove non-execution. Only earlier setup failures can safely repark.
	started = true
	if resolvedResult != nil {
		if err := sink.ToolResult(*resolvedResult); err != nil {
			return out, err
		}
	}
	if err := p.wire.send(ctx, message{JSONRPC: "2.0", ID: json.RawMessage(promptID), Method: "session/prompt"}, struct {
		SessionID string    `json:"sessionId"`
		Prompt    []content `json:"prompt"`
	}{SessionID: id, Prompt: []content{{Type: "text", Text: turn.Prompt}}}); err != nil {
		return out, err
	}
	return p.collect(ctx, sink)
}

func ValidateSessionID(id string) error {
	if id == "" || len(id) > 256 || strings.ContainsAny(id, "/\\\x00\r\n\t ") {
		return fmt.Errorf("invalid DSH session identity")
	}
	return nil
}

func (p *process) collect(ctx context.Context, sink runtime.EventSink) (runtime.Outcome, error) {
	var held *heldTool
	var associate <-chan time.Time
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		if held != nil {
			callID, err := p.events.heldCallID(held)
			if err != nil {
				return runtime.Outcome{}, err
			}
			if callID != "" {
				return p.deferTool(ctx, held, callID)
			}
		}
		var m message
		var err error
		select {
		case held = <-p.mcp.approvals:
			timer = time.NewTimer(setupTimeout)
			associate = timer.C
			continue
		case <-associate:
			return runtime.Outcome{}, fmt.Errorf("protected MCP call has no matching ACP event")
		case <-ctx.Done():
			err = ctx.Err()
		case f, ok := <-p.wire.frames:
			if !ok {
				err = fmt.Errorf("DSH ACP stream closed")
			} else {
				m, err = f.message, f.err
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return runtime.Outcome{}, errors.Join(ctx.Err(), p.cancel())
			}
			return runtime.Outcome{}, err
		}
		switch {
		case m.Method == "session/update" && len(m.ID) == 0:
			if err := p.events.apply(m.Params, sink); err != nil {
				return runtime.Outcome{}, err
			}
		case m.Method == "session/request_permission" && len(m.ID) != 0:
			if held != nil {
				return runtime.Outcome{}, fmt.Errorf("native permission and protected MCP requests must be sequential")
			}
			request, allow, reject, err := p.events.approval(m.ID, m.Params)
			if err != nil {
				return runtime.Outcome{}, err
			}
			return runtime.Outcome{Pending: &pending{process: p, id: m.ID, request: request, allow: allow, reject: reject}}, nil
		case m.Method != "" && len(m.ID) != 0:
			return runtime.Outcome{}, fmt.Errorf("unsupported ACP server request")
		case m.Method != "":
			return runtime.Outcome{}, fmt.Errorf("unsupported ACP notification")
		default:
			if string(m.ID) != promptID {
				return runtime.Outcome{}, fmt.Errorf("unexpected ACP prompt response identity")
			}
			if m.Error != nil {
				return runtime.Outcome{Failure: &runtime.Failure{Message: fmt.Sprintf("DSH ACP prompt failed (code %d)", m.Error.Code)}}, nil
			}
			var result struct {
				StopReason string `json:"stopReason"`
			}
			if err := json.Unmarshal(m.Result, &result); err != nil {
				return runtime.Outcome{}, fmt.Errorf("failed to decode ACP completion: %w", err)
			}
			switch result.StopReason {
			case "end_turn", "max_tokens", "max_turn_requests":
			case "refusal", "cancelled":
				return runtime.Outcome{Failure: &runtime.Failure{Message: "DSH stopped: " + result.StopReason}}, nil
			default:
				return runtime.Outcome{}, fmt.Errorf("unsupported ACP stop reason")
			}
			closeCtx, cancel := context.WithTimeout(context.Background(), stopTimeout)
			defer cancel()
			_, err := p.wire.call(closeCtx, "4", "session/close", sessionParams{SessionID: p.events.sessionID})
			return runtime.Outcome{}, err
		}
	}
}

type sessionParams struct {
	SessionID string `json:"sessionId"`
}
type permissionResponse struct {
	Outcome permissionOutcome `json:"outcome"`
}
type permissionOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}

// pending retains a live ACP request and requires memory-preserving suspension.
// TODO: adapt to the shared durable-input contract when
// https://github.com/kagent-dev/kagent/pull/3165 lands. Migration also requires a
// DSH-supported way to resume the exact pending call after process replacement,
// without replaying a possibly executed tool. A persisted session ID alone cannot
// replace this live request; do not enable DATA-only approval recovery by itself.
type pending struct {
	process       *process
	id            json.RawMessage
	request       *runtime.ApprovalRequest
	allow, reject string
}

var _ runtime.PendingTurn = (*pending)(nil)

func (p *pending) Request() runtime.InputRequest { return p.request }

func (p *pending) Resume(ctx context.Context, response runtime.InputResponse, sink runtime.EventSink) (out runtime.Outcome, returnErr error) {
	decision, ok := response.(*runtime.ApprovalDecision)
	if !ok || decision.ID != p.request.ID {
		return out, errors.Join(fmt.Errorf("approval decision does not match pending DSH request"), p.process.stop())
	}
	select {
	case <-p.process.done:
		// done closes only after the supervisor confirms descendant cleanup.
		// The exit is the failure we report, so preserve cleanup errors without
		// replacing this actionable outcome with the expected process exit error.
		return runtime.Outcome{Failure: &runtime.Failure{Message: "DSH exited while awaiting approval. The approval could not be applied. Send a new message in this conversation to continue."}}, p.process.cleanup()
	default:
	}
	defer func() {
		if out.Pending == nil {
			returnErr = errors.Join(returnErr, p.process.stop())
		}
	}()
	option := p.reject
	if decision.Approved {
		option = p.allow
	} else {
		tool := p.process.events.tools[p.request.CallID]
		tool.declined = true
		p.process.events.tools[p.request.CallID] = tool
	}
	if err := p.process.wire.respond(ctx, p.id, permissionResponse{Outcome: permissionOutcome{Outcome: "selected", OptionID: option}}); err != nil {
		return out, err
	}
	return p.process.collect(ctx, sink)
}

func (p *pending) Cancel(_ context.Context, _ runtime.ModelCallSink) error {
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	err := p.process.wire.respond(ctx, p.id, permissionResponse{Outcome: permissionOutcome{Outcome: "cancelled"}})
	return errors.Join(err, p.process.cancel(), p.process.stop())
}

func (p *process) cancel() error {
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	if err := p.wire.send(ctx, message{JSONRPC: "2.0", Method: "session/cancel"}, sessionParams{SessionID: p.events.sessionID}); err != nil {
		return err
	}
	for {
		m, err := p.wire.next(ctx)
		if err != nil {
			return err
		}
		if string(m.ID) == promptID && m.Method == "" {
			return nil
		}
		if m.Method != "" && len(m.ID) != 0 {
			if m.Method != "session/request_permission" {
				return fmt.Errorf("unsupported ACP request during cancellation")
			}
			if err := p.wire.respond(ctx, m.ID, permissionResponse{Outcome: permissionOutcome{Outcome: "cancelled"}}); err != nil {
				return err
			}
		}
	}
}

// EOF lets DSH flush persistence. After the grace period, the supervisor kills
// DSH and reaps all descendants, including tools in separate process groups.
func (p *process) stop() error {
	err := p.cleanup()
	return errors.Join(err, p.waitErr)
}

// cleanup always waits for confirmed process-tree cleanup. The caller decides
// whether a nonzero exit is an error or an already classified failed outcome.
func (p *process) cleanup() error {
	p.cleanupOnce.Do(func() {
		if err := p.cleanupResources(); err != nil {
			p.cleanupErr = &processCleanupError{err}
		}
	})
	return p.cleanupErr
}

func (p *process) cleanupResources() error {
	close(p.wire.done)
	err := p.stdin.Close()
	if errors.Is(err, os.ErrClosed) || errors.Is(err, io.ErrClosedPipe) {
		err = nil
	}
	select {
	case <-p.done:
	case <-time.After(stopTimeout):
		// Request forced cleanup through the lifetime pipe, then keep waiting
		// for confirmation rather than releasing the Actor on a deadline.
		err = errors.Join(err, p.control.Close())
		<-p.done
	}
	if closeErr := p.control.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
		err = errors.Join(err, closeErr)
	}
	<-p.wire.readDone
	if p.mcp != nil {
		err = errors.Join(err, p.mcp.close())
	}
	return errors.Join(err, os.RemoveAll(p.home))
}
