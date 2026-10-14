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

	"github.com/google/uuid"

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
	PendingDir           string
	Tools                ToolCaller
}

// ProcessDriver supervises Codex App Server. Pending input is persisted and
// the native process exits before input-required.
type ProcessDriver struct {
	config ProcessConfig
}

type processSession struct {
	command    *exec.Cmd
	stdin      io.WriteCloser
	client     *rpcClient
	wait       <-chan error
	stderr     *utils.BoundedBuffer
	translator *eventTranslator
	stopOnce   sync.Once
	turn       runtime.Turn
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
	return d.run(ctx, turn, nil, sink)
}

type turnToolOutput struct {
	Name      string  `json:"name"`
	Namespace *string `json:"namespace"`
	Output    string  `json:"output"`
}

// toolContinuation pairs native input with the original public result. Publish
// that result only once native startup has passed the safe retry boundary.
type toolContinuation struct {
	output turnToolOutput
	result runtime.ToolResult
}

// turnNotStartedError proves that this attempt never sent turn/start.
type turnNotStartedError struct{ err error }

func (e *turnNotStartedError) Error() string { return e.err.Error() }
func (e *turnNotStartedError) Unwrap() error { return e.err }

func (d *ProcessDriver) run(ctx context.Context, turn runtime.Turn, tool *toolContinuation, sink runtime.EventSink) (outcome runtime.Outcome, returnErr error) {
	dispatched := false
	defer func() {
		if !dispatched && returnErr != nil {
			returnErr = &turnNotStartedError{err: returnErr}
		}
	}()
	if strings.TrimSpace(turn.Prompt) == "" && tool == nil {
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
	client := newRPCClient(stdin, stdout, d.config.MaxFrameBytes)
	go func() {
		// Wait closes StdoutPipe; let the RPC reader consume the final reports first.
		<-client.readDone
		wait <- command.Wait()
		close(wait)
	}()
	session := &processSession{command: command, stdin: stdin, client: client, wait: wait, stderr: stderr, turn: turn}
	defer d.stopSession(session, sink)

	initialize := map[string]any{
		"clientInfo": map[string]string{"name": "kagent-codex", "version": "1"},
		// request_user_input is currently part of App Server's experimental API.
		// Unknown experimental server requests still fail closed below.
		"capabilities": map[string]bool{"experimentalApi": true},
	}
	if _, err := client.call(ctx, 1, "initialize", initialize, nil); err != nil {
		return runtime.Outcome{}, d.protocolError(err, stderr)
	}
	if err := client.notify("initialized", map[string]any{}); err != nil {
		return runtime.Outcome{}, d.protocolError(err, stderr)
	}
	threadID := turn.ContinuationID
	if threadID == "" {
		result, err := client.call(ctx, 2, "thread/start", map[string]any{
			"cwd": d.config.Workspace, "model": d.config.Model, "modelProvider": d.config.Provider,
			"approvalPolicy": mcpOnlyApprovalPolicy(), "sandbox": "danger-full-access", "developerInstructions": d.config.DeveloperInstruction,
		}, nil)
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
			"approvalPolicy": mcpOnlyApprovalPolicy(), "sandbox": "danger-full-access", "developerInstructions": d.config.DeveloperInstruction,
		}, nil)
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
	params := struct {
		ThreadID   string          `json:"threadId"`
		Input      []textInput     `json:"input"`
		ToolOutput *turnToolOutput `json:"toolOutput,omitempty"`
	}{ThreadID: threadID, Input: []textInput{}}
	if tool == nil {
		params.Input = append(params.Input, textInput{Type: "text", Text: turn.Prompt})
	} else {
		params.ToolOutput = &tool.output
	}
	// A lost response after sending this request is uncertain, even if the
	// process has not emitted any model or tool activity yet.
	dispatched = true
	result, err := client.call(ctx, 3, "turn/start", params, nil)
	if err != nil {
		return runtime.Outcome{}, d.protocolError(err, stderr)
	}
	turnID, err := responseTurnID(result)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if tool != nil {
		if err := sink.ToolResult(tool.result); err != nil {
			return runtime.Outcome{}, err
		}
	}
	session.translator = newEventTranslator(threadID, turnID)
	return d.consume(ctx, session, sink)
}

type textInput struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// mcpOnlyApprovalPolicy lets Codex surface MCP approval prompts while rejecting
// its command, sandbox, rule, skill, and request_permissions approval flows.
// Per-server approval modes still decide which MCP tool calls actually prompt.
func mcpOnlyApprovalPolicy() map[string]any {
	return map[string]any{"granular": map[string]bool{
		"sandbox_approval":    false,
		"rules":               false,
		"skill_approval":      false,
		"request_permissions": false,
		"mcp_elicitations":    true,
	}}
}

func (d *ProcessDriver) consume(ctx context.Context, session *processSession, sink runtime.EventSink) (runtime.Outcome, error) {
	client, wait, stderr, translator := session.client, session.wait, session.stderr, session.translator
	for {
		select {
		case <-ctx.Done():
			interrupted := time.Now()
			err := d.interrupt(session, sink)
			if err != nil {
				_ = utils.KillProcessGroup(session.command.Process)
			}
			// An interrupt response can precede the final usage report. Let Codex
			// flush it on stdin EOF before escalating to process-group signals.
			d.stopSession(session, sink)
			translator.flushOpenCall(interrupted, sink)
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
					translator.endOpenCalls()
					return d.deferInput(ctx, session, request, sink)
				}
				continue
			}
			outcome, done, err := translator.translate(message, sink)
			if err != nil {
				return runtime.Outcome{}, err
			}
			if done {
				if err := translator.rejectBufferedPostTerminalActivity(client.frames); err != nil {
					return runtime.Outcome{}, err
				}
				return outcome, nil
			}
		}
	}
}

// handleServerRequest handles a Codex server request.
// It checks if the request is a MCP elicitation request or an ask-user request.
// Codex returns MCP tool approval requests as MCP elicitation requests.
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
		ThreadID string  `json:"threadId"`
		TurnID   *string `json:"turnId"`
		Meta     struct {
			Kind       string         `json:"codex_approval_kind"`
			ToolName   string         `json:"tool_name"`
			ToolParams map[string]any `json:"tool_params"`
		} `json:"_meta"`
		Message    string `json:"message"`
		ServerName string `json:"serverName"`
	}
	if err := json.Unmarshal(message.Params, &params); err != nil {
		return nil, false, fmt.Errorf("decode Codex MCP approval request: %w", err)
	}
	_, child := translator.spawnedBy[params.ThreadID]
	if !child && (params.ThreadID != translator.threadID || params.TurnID != nil && *params.TurnID != translator.turnID) {
		return nil, false, fmt.Errorf("Codex MCP request does not match the active turn")
	}
	if params.Meta.Kind != "mcp_tool_call" {
		// This is genuine MCP elicitation, which the native Harness does not expose.
		return nil, false, client.respond(message.ID, map[string]any{"action": "decline", "content": map[string]any{}})
	}
	if _, protected := d.config.ApprovalServers[params.ServerName]; !protected {
		return nil, false, client.respond(message.ID, map[string]any{"action": "accept", "content": map[string]any{}})
	}
	if child {
		// DATA continuation currently resumes only the root thread. Decline a
		// child callback without releasing a protected call or failing its parent.
		return nil, false, client.respond(message.ID, map[string]any{"action": "decline", "content": map[string]any{}})
	}
	callID, name, err := translator.approvalTool(params.ThreadID, params.ServerName, params.Meta.ToolName, params.Meta.ToolParams)
	if err != nil {
		return nil, false, err
	}
	return &runtime.ApprovalRequest{ID: uuid.NewString(), CallID: callID, Name: name, Args: params.Meta.ToolParams, Hint: params.Message}, true, nil
}

// decodeAskUserRequest decodes a Codex ask-user request into a runtime.AskUserRequest.
func decodeAskUserRequest(message rpcMessage, translator *eventTranslator) (*runtime.AskUserRequest, error) {
	var params struct {
		ThreadID  string `json:"threadId"`
		TurnID    string `json:"turnId"`
		Questions []struct {
			ID       string `json:"id"`
			Question string `json:"question"`
			Options  []struct {
				Label string `json:"label"`
			} `json:"options"`
			IsSecret bool `json:"isSecret"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(message.Params, &params); err != nil {
		return nil, fmt.Errorf("decode Codex ask-user request: %w", err)
	}
	// A spawned subagent asks from its own thread and turn.
	_, subagent := translator.spawnedBy[params.ThreadID]
	if !subagent && (params.ThreadID != translator.threadID || params.TurnID != translator.turnID) {
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
		choices := make([]string, 0, len(question.Options))
		for _, option := range question.Options {
			choices = append(choices, option.Label)
		}
		questions = append(questions, runtime.AskUserQuestion{
			ID: question.ID, Question: question.Question, Choices: choices,
		})
	}
	return &runtime.AskUserRequest{
		ID: uuid.NewString(), Questions: questions,
		Hint: "Codex requires more information before continuing.",
	}, nil
}

// interrupt asks Codex to stop the turn. Usage and final errors Codex reports until it answers,
// including buffered ones, still reach sink; stopSession records what arrives after.
func (d *ProcessDriver) interrupt(session *processSession, sink runtime.ModelCallSink) error {
	ctx, cancel := context.WithTimeout(context.Background(), d.config.InterruptGrace)
	defer cancel()
	params := map[string]string{"threadId": session.translator.threadID, "turnId": session.translator.turnID}
	_, err := session.client.call(ctx, 4, "turn/interrupt", params, func(message rpcMessage) { session.recordUsage(message, sink) })
	return err
}

func (s *processSession) recordUsage(message rpcMessage, sink runtime.ModelCallSink) {
	if s.translator == nil {
		return // Initialization failed before a turn started.
	}
	switch message.Method {
	case "thread/tokenUsage/updated":
		_ = s.translator.usage(message.Params, sink)
	case "error":
		_ = s.translator.codexError(message.Params, sink)
	case "turn/completed":
		var params struct {
			ThreadID string
			Turn     struct{ ID, Status string }
		}
		if json.Unmarshal(message.Params, &params) == nil && params.ThreadID == s.translator.threadID &&
			params.Turn.ID == s.translator.turnID && params.Turn.Status == "completed" {
			s.translator.completed = true
		}
	}
}

func (d *ProcessDriver) stopSession(session *processSession, sink runtime.ModelCallSink) {
	session.stopOnce.Do(func() {
		// Drain on every exit path so a full frame buffer cannot block the reader
		// and prevent Wait from reaping the process.
		drained := make(chan struct{})
		go func() {
			defer close(drained)
			for frame := range session.client.frames {
				session.recordUsage(frame.message, sink)
			}
		}()
		defer func() { <-drained }()

		_ = session.stdin.Close()
		select {
		case <-session.wait:
			return
		case <-time.After(d.config.InterruptGrace):
		}

		_ = utils.TerminateProcessGroup(session.command.Process)
		select {
		case <-session.wait:
		case <-time.After(d.config.InterruptGrace):
			_ = utils.KillProcessGroup(session.command.Process)
			<-session.wait
		}
	})
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
