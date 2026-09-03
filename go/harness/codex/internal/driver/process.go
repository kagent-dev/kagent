// Package driver translates the Codex App Server protocol into the
// runtime-neutral events consumed by the shared A2A executor.
package driver

import (
	"context"
	"encoding/json"
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
)

// ProcessConfig contains validated, compiler-owned inputs for one Codex App
// Server process. Actor-owned paths and environment are supplied by the adapter.
type ProcessConfig struct {
	Executable           string
	ExpectedVersion      string
	StrictVersion        bool
	Workspace            string
	Model                string
	Provider             string
	DeveloperInstruction string
	Environment          []string
	MaxFrameBytes        int
	MaxStderrBytes       int
	InterruptGrace       time.Duration
	ApprovalServers      map[string]struct{}
}

// ProcessDriver supervises Codex App Server and retains it while a protected
// MCP invocation is waiting for a human decision.
type ProcessDriver struct {
	config ProcessConfig
	mu     sync.Mutex
	parked *processSession
}

type processSession struct {
	command    *exec.Cmd
	stdin      io.WriteCloser
	client     *rpcClient
	wait       <-chan error
	stderr     *utils.BoundedBuffer
	translator *eventTranslator
	pendingID  json.RawMessage
	pending    runtime.InputRequest
}

// NewProcessDriver constructs a Codex process driver.
func NewProcessDriver(config ProcessConfig) *ProcessDriver { return &ProcessDriver{config: config} }

// Validate checks that the configured executable is the pinned Codex version.
func (d *ProcessDriver) Validate(ctx context.Context) error {
	executable, err := exec.LookPath(d.config.Executable)
	if err != nil {
		return fmt.Errorf("find Codex executable %q: %w", d.config.Executable, err)
	}
	output, err := exec.CommandContext(ctx, executable, "--version").Output()
	if err != nil {
		return fmt.Errorf("read Codex version: %w", err)
	}
	version := strings.TrimSpace(string(output))
	if d.config.StrictVersion && version != "codex-cli "+d.config.ExpectedVersion {
		return fmt.Errorf("codex version mismatch: got %q, expected %q", version, "codex-cli "+d.config.ExpectedVersion)
	}
	return nil
}

// Run initializes App Server, starts or resumes the Actor's native thread, and
// emits the turn's ordered runtime events.
func (d *ProcessDriver) Run(ctx context.Context, turn runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.parked != nil {
		return d.resumeParked(ctx, turn, sink)
	}
	if strings.TrimSpace(turn.Prompt) == "" {
		return runtime.Outcome{}, fmt.Errorf("codex prompt is required")
	}
	if err := rejectWorkspaceConfig(d.config.Workspace); err != nil {
		return runtime.Outcome{}, err
	}
	executable, err := exec.LookPath(d.config.Executable)
	if err != nil {
		return runtime.Outcome{}, fmt.Errorf("find Codex executable %q: %w", d.config.Executable, err)
	}
	command := exec.Command(executable, "app-server", "--strict-config", "--stdio")
	command.Dir, command.Env = d.config.Workspace, append([]string(nil), d.config.Environment...)
	utils.ConfigureProcessGroup(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return runtime.Outcome{}, fmt.Errorf("open Codex stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return runtime.Outcome{}, fmt.Errorf("open Codex stdout: %w", err)
	}
	stderr := utils.NewBoundedBuffer(d.config.MaxStderrBytes)
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return runtime.Outcome{}, fmt.Errorf("start Codex App Server: %w", err)
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait(); close(wait) }()
	client := newRPCClient(stdin, stdout, d.config.MaxFrameBytes)
	session := &processSession{command: command, stdin: stdin, client: client, wait: wait, stderr: stderr}
	parked := false
	defer func() {
		if !parked {
			d.stopSession(session)
		}
	}()

	initialize := map[string]any{
		"clientInfo": map[string]string{"name": "kagent-codex", "version": "1"},
		// request_user_input is currently part of App Server's experimental API.
		// Unknown experimental server requests still fail closed below.
		"capabilities": map[string]bool{"experimentalApi": true},
	}
	if _, err := client.call(ctx, 1, "initialize", initialize); err != nil {
		return runtime.Outcome{}, d.protocolError(err, stderr)
	}
	if err := client.notify("initialized", map[string]any{}); err != nil {
		return runtime.Outcome{}, d.protocolError(err, stderr)
	}
	threadID := turn.ContinuationID
	if threadID == "" {
		result, err := client.call(ctx, 2, "thread/start", map[string]any{
			"cwd": d.config.Workspace, "model": d.config.Model, "modelProvider": d.config.Provider,
			"approvalPolicy": "never", "sandbox": "danger-full-access", "developerInstructions": d.config.DeveloperInstruction,
		})
		if err != nil {
			return runtime.Outcome{}, d.protocolError(err, stderr)
		}
		threadID, err = responseThreadID(result)
		if err != nil {
			return runtime.Outcome{}, err
		}
	} else {
		result, err := client.call(ctx, 2, "thread/resume", map[string]any{
			"threadId": threadID, "cwd": d.config.Workspace, "model": d.config.Model, "modelProvider": d.config.Provider,
			"approvalPolicy": "never", "sandbox": "danger-full-access", "developerInstructions": d.config.DeveloperInstruction,
		})
		if err != nil {
			return runtime.Outcome{}, d.protocolError(err, stderr)
		}
		resumedID, err := responseThreadID(result)
		if err != nil {
			return runtime.Outcome{}, err
		}
		if resumedID != threadID {
			return runtime.Outcome{}, fmt.Errorf("codex resumed unexpected thread %q", resumedID)
		}
	}
	if err := sink.SessionStarted(runtime.SessionStarted{ContinuationID: threadID}); err != nil {
		return runtime.Outcome{}, err
	}
	result, err := client.call(ctx, 3, "turn/start", map[string]any{
		"threadId": threadID, "input": []map[string]any{{"type": "text", "text": turn.Prompt}},
	})
	if err != nil {
		return runtime.Outcome{}, d.protocolError(err, stderr)
	}
	turnID, err := responseTurnID(result)
	if err != nil {
		return runtime.Outcome{}, err
	}
	session.translator = newEventTranslator(threadID, turnID)
	outcome, err := d.consume(ctx, session, sink)
	if err == nil && outcome.InputRequired != nil {
		d.parked, parked = session, true
	}
	return outcome, err
}

func (d *ProcessDriver) consume(ctx context.Context, session *processSession, sink runtime.EventSink) (runtime.Outcome, error) {
	client, command, wait, stderr, translator := session.client, session.command, session.wait, session.stderr, session.translator
	for {
		select {
		case <-ctx.Done():
			interruptCtx, cancel := context.WithTimeout(context.Background(), d.config.InterruptGrace)
			_, err := client.call(interruptCtx, 4, "turn/interrupt", map[string]string{"threadId": translator.threadID, "turnId": translator.turnID})
			cancel()
			if err != nil {
				_ = utils.KillProcessGroup(command.Process)
			} else {
				_ = utils.TerminateProcessGroup(command.Process)
			}
			select {
			case <-wait:
			case <-time.After(d.config.InterruptGrace):
				_ = utils.KillProcessGroup(command.Process)
				<-wait
			}
			return runtime.Outcome{}, ctx.Err()
		case waitErr := <-wait:
			if waitErr != nil {
				return runtime.Outcome{}, fmt.Errorf("codex App Server exited: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
			}
			return runtime.Outcome{}, fmt.Errorf("codex App Server exited without a terminal event")
		case frame, ok := <-client.frames:
			if !ok {
				return runtime.Outcome{}, fmt.Errorf("codex protocol stream closed without a terminal event")
			}
			if frame.err != nil {
				return runtime.Outcome{}, frame.err
			}
			message := frame.message
			if message.Method == "" {
				continue
			}
			if len(message.ID) != 0 {
				request, expose, err := d.handleServerRequest(client, translator, message)
				if err != nil {
					return runtime.Outcome{}, err
				}
				if expose {
					session.pendingID = append(json.RawMessage(nil), message.ID...)
					session.pending = request
					return runtime.Outcome{InputRequired: request}, nil
				}
				continue
			}
			outcome, done, err := translator.translate(message, sink)
			if err != nil {
				return runtime.Outcome{}, err
			}
			if done {
				if err := rejectBufferedPostTerminalActivity(client.frames); err != nil {
					return runtime.Outcome{}, err
				}
				return outcome, nil
			}
		}
	}
}

func (d *ProcessDriver) handleServerRequest(client *rpcClient, translator *eventTranslator, message rpcMessage) (runtime.InputRequest, bool, error) {
	if message.Method != "mcpServer/elicitation/request" {
		switch message.Method {
		case "item/tool/requestUserInput":
			request, err := decodeAskUserRequest(message, translator)
			return request, err == nil, err
		default:
			return nil, false, fmt.Errorf("unsupported Codex server request %q", message.Method)
		}
	}
	var params struct {
		Meta struct {
			Kind       string         `json:"codex_approval_kind"`
			ToolParams map[string]any `json:"tool_params"`
		} `json:"_meta"`
		Message    string `json:"message"`
		ServerName string `json:"serverName"`
	}
	if err := json.Unmarshal(message.Params, &params); err != nil {
		return nil, false, fmt.Errorf("decode Codex MCP approval request: %w", err)
	}
	if params.Meta.Kind != "mcp_tool_call" {
		// This is genuine MCP elicitation, which the native Harness does not expose.
		return nil, false, client.respond(message.ID, map[string]any{"action": "decline", "content": map[string]any{}})
	}
	if _, protected := d.config.ApprovalServers[params.ServerName]; !protected {
		return nil, false, client.respond(message.ID, map[string]any{"action": "accept", "content": map[string]any{}})
	}
	callID, name, err := translator.approvalTool(params.ServerName)
	if err != nil {
		return nil, false, err
	}
	key := requestKey(message.ID)
	return &runtime.ApprovalRequest{ID: key, CallID: callID, Name: name, Args: params.Meta.ToolParams, Hint: params.Message}, true, nil
}

func decodeAskUserRequest(message rpcMessage, translator *eventTranslator) (*runtime.AskUserRequest, error) {
	var params struct {
		ThreadID  string `json:"threadId"`
		TurnID    string `json:"turnId"`
		Questions []struct {
			ID       string `json:"id"`
			Header   string `json:"header"`
			Question string `json:"question"`
			Options  []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
			IsOther  bool `json:"isOther"`
			IsSecret bool `json:"isSecret"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(message.Params, &params); err != nil {
		return nil, fmt.Errorf("decode Codex ask-user request: %w", err)
	}
	if params.ThreadID != translator.threadID || params.TurnID != translator.turnID {
		return nil, fmt.Errorf("Codex ask-user request does not match the active turn")
	}
	if len(params.Questions) == 0 {
		return nil, fmt.Errorf("Codex ask-user request contains no questions")
	}
	questions := make([]runtime.AskUserQuestion, 0, len(params.Questions))
	questionIDs := make(map[string]struct{}, len(params.Questions))
	for _, question := range params.Questions {
		if question.ID == "" || question.Question == "" {
			return nil, fmt.Errorf("Codex ask-user request contains an incomplete question")
		}
		if question.IsSecret {
			// A2A task history is durable. Do not present a field as secret until
			// the public response and persistence path can preserve that promise.
			return nil, fmt.Errorf("Codex secret ask-user questions are not supported")
		}
		if _, duplicate := questionIDs[question.ID]; duplicate {
			return nil, fmt.Errorf("Codex ask-user request contains duplicate question ID %q", question.ID)
		}
		questionIDs[question.ID] = struct{}{}
		options := make([]runtime.AskUserOption, 0, len(question.Options))
		for _, option := range question.Options {
			options = append(options, runtime.AskUserOption{Label: option.Label, Description: option.Description})
		}
		questions = append(questions, runtime.AskUserQuestion{
			ID: question.ID, Header: question.Header, Question: question.Question,
			Options: options, IsOther: question.IsOther, IsSecret: question.IsSecret,
		})
	}
	return &runtime.AskUserRequest{
		ID: requestKey(message.ID), Questions: questions,
		Hint: "Codex requires more information before continuing.",
	}, nil
}

func requestKey(id json.RawMessage) string {
	var text string
	if json.Unmarshal(id, &text) == nil {
		return text
	}
	return string(id)
}

func (d *ProcessDriver) resumeParked(ctx context.Context, turn runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
	session := d.parked
	response, err := codexInputResponse(session.pending, turn.InputResponse)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err := session.client.respond(session.pendingID, response); err != nil {
		d.parked = nil
		d.stopSession(session)
		return runtime.Outcome{}, err
	}
	session.pendingID, session.pending = nil, nil
	outcome, err := d.consume(ctx, session, sink)
	if err == nil && outcome.InputRequired != nil {
		return outcome, nil
	}
	d.parked = nil
	d.stopSession(session)
	return outcome, err
}

// CancelParked resolves the outstanding approval as canceled, interrupts its
// native turn, and tears down the retained App Server process. Protocol errors
// fall back to bounded process termination, which still satisfies cancellation.
func (d *ProcessDriver) CancelParked(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.parked == nil {
		return fmt.Errorf("Codex has no parked turn to cancel")
	}
	session := d.parked
	d.parked = nil

	responded := true
	if _, approval := session.pending.(*runtime.ApprovalRequest); approval {
		responded = session.client.respond(session.pendingID, map[string]any{
			"action": "cancel", "content": nil,
		}) == nil
	}
	if responded {
		interruptCtx, cancel := context.WithTimeout(ctx, d.config.InterruptGrace)
		_, _ = session.client.call(interruptCtx, 4, "turn/interrupt", map[string]string{
			"threadId": session.translator.threadID,
			"turnId":   session.translator.turnID,
		})
		cancel()
	}
	d.stopSession(session)
	return nil
}

func codexInputResponse(request runtime.InputRequest, response runtime.InputResponse) (map[string]any, error) {
	switch request := request.(type) {
	case *runtime.ApprovalRequest:
		decision, ok := response.(*runtime.ApprovalDecision)
		if !ok {
			return nil, fmt.Errorf("Codex is waiting for a structured tool approval response")
		}
		if decision.ID != request.ID {
			return nil, fmt.Errorf("tool approval response ID %q does not match pending ID %q", decision.ID, request.ID)
		}
		return approvalResponse(*decision), nil
	case *runtime.AskUserRequest:
		answer, ok := response.(*runtime.AskUserResponse)
		if !ok {
			return nil, fmt.Errorf("Codex is waiting for a structured ask-user response")
		}
		if answer.ID != request.ID {
			return nil, fmt.Errorf("ask-user response ID %q does not match pending ID %q", answer.ID, request.ID)
		}
		if len(answer.Answers) != len(request.Questions) {
			return nil, fmt.Errorf("ask-user response must answer every pending question")
		}
		answers := make(map[string]any, len(request.Questions))
		for index, question := range request.Questions {
			answers[question.ID] = map[string]any{"answers": answer.Answers[index]}
		}
		return map[string]any{"answers": answers}, nil
	default:
		return nil, fmt.Errorf("Codex has an unsupported pending input request %T", request)
	}
}

// approvalResponse maps the runtime-neutral decision onto Codex's pinned MCP
// elicitation response. Codex accepts only the decision here: the public A2A and
// runtime models retain a rejection reason for harnesses that support one, but
// this adapter deliberately drops it rather than sending undocumented metadata.
func approvalResponse(decision runtime.ApprovalDecision) map[string]any {
	if decision.Approved {
		return map[string]any{"action": "accept", "content": map[string]any{}}
	}
	return map[string]any{"action": "decline"}
}

func (d *ProcessDriver) stopSession(session *processSession) {
	_ = session.stdin.Close()
	_ = utils.TerminateProcessGroup(session.command.Process)
	select {
	case <-session.wait:
	case <-time.After(d.config.InterruptGrace):
		_ = utils.KillProcessGroup(session.command.Process)
		<-session.wait
	}
}

func rejectWorkspaceConfig(workspace string) error {
	path := filepath.Join(workspace, ".codex", "config.toml")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect workspace Codex configuration: %w", err)
	}
	if info != nil {
		return fmt.Errorf("workspace Codex configuration %q is not allowed", path)
	}
	return nil
}

func (d *ProcessDriver) protocolError(err error, stderr *utils.BoundedBuffer) error {
	message := strings.TrimSpace(stderr.String())
	if message == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, message)
}
