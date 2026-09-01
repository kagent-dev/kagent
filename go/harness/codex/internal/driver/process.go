// Package driver translates the Codex App Server protocol into the
// runtime-neutral events consumed by the shared A2A executor.
package driver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/harness/runtime/utils"
)

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
}

type ProcessDriver struct{ config ProcessConfig }

func NewProcessDriver(config ProcessConfig) *ProcessDriver { return &ProcessDriver{config: config} }

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

func (d *ProcessDriver) Run(ctx context.Context, turn runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
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
	defer func() {
		_ = stdin.Close()
		_ = utils.TerminateProcessGroup(command.Process)
		select {
		case <-wait:
		case <-time.After(d.config.InterruptGrace):
			_ = utils.KillProcessGroup(command.Process)
			<-wait
		}
	}()

	if _, err := client.call(ctx, 1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "kagent-codex", "version": "1"}}); err != nil {
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
	return d.consume(ctx, client, command, wait, stderr, threadID, turnID, sink)
}

func (d *ProcessDriver) consume(ctx context.Context, client *rpcClient, command *exec.Cmd, wait <-chan error, stderr *utils.BoundedBuffer, threadID, turnID string, sink runtime.EventSink) (runtime.Outcome, error) {
	tools := make(map[string]string)
	for {
		select {
		case <-ctx.Done():
			interruptCtx, cancel := context.WithTimeout(context.Background(), d.config.InterruptGrace)
			_, err := client.call(interruptCtx, 4, "turn/interrupt", map[string]string{"threadId": threadID, "turnId": turnID})
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
				return runtime.Outcome{}, fmt.Errorf("unsupported Codex server request %q", message.Method)
			}
			outcome, done, err := translateNotification(message, threadID, turnID, sink, tools)
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

func rejectBufferedPostTerminalActivity(frames <-chan rpcFrame) error {
	for {
		select {
		case frame, ok := <-frames:
			if !ok {
				return nil
			}
			if frame.err != nil {
				return frame.err
			}
			if frame.message.Method == "turn/completed" {
				return fmt.Errorf("codex emitted duplicate terminal event")
			}
			if strings.HasPrefix(frame.message.Method, "item/") || strings.HasPrefix(frame.message.Method, "turn/") {
				return fmt.Errorf("codex emitted activity after its terminal event")
			}
		default:
			return nil
		}
	}
}

func translateNotification(message rpcMessage, threadID, turnID string, sink runtime.EventSink, tools map[string]string) (runtime.Outcome, bool, error) {
	switch message.Method {
	case "item/agentMessage/delta":
		var params struct{ ThreadID, TurnID, ItemID, Delta string }
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return runtime.Outcome{}, false, fmt.Errorf("decode Codex text delta: %w", err)
		}
		if params.ThreadID != threadID || params.TurnID != turnID || params.ItemID == "" {
			return runtime.Outcome{}, false, fmt.Errorf("codex text delta has mismatched identity")
		}
		return runtime.Outcome{}, false, sink.TextDelta(runtime.TextDelta{Text: params.Delta})
	case "item/started", "item/completed":
		var params struct {
			ThreadID, TurnID string
			Item             json.RawMessage
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return runtime.Outcome{}, false, fmt.Errorf("decode Codex item event: %w", err)
		}
		if params.ThreadID != threadID || params.TurnID != turnID {
			return runtime.Outcome{}, false, fmt.Errorf("codex item event has mismatched identity")
		}
		if err := translateItem(message.Method == "item/completed", params.Item, sink, tools); err != nil {
			return runtime.Outcome{}, false, err
		}
		return runtime.Outcome{}, false, nil
	case "turn/completed":
		var params struct {
			ThreadID string
			Turn     struct {
				ID, Status string
				Error      *struct{ Message string }
			}
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return runtime.Outcome{}, false, fmt.Errorf("decode Codex terminal event: %w", err)
		}
		if params.ThreadID != threadID || params.Turn.ID != turnID {
			return runtime.Outcome{}, false, fmt.Errorf("codex terminal event has mismatched identity")
		}
		if err := closeActiveTools(sink, tools); err != nil {
			return runtime.Outcome{}, false, err
		}
		switch params.Turn.Status {
		case "completed":
			return runtime.Outcome{}, true, nil
		case "interrupted":
			return runtime.Outcome{Failure: &runtime.Failure{Message: "Codex execution was interrupted"}}, true, nil
		case "failed":
			return runtime.Outcome{Failure: &runtime.Failure{Message: "Codex execution failed"}}, true, nil
		default:
			return runtime.Outcome{}, false, fmt.Errorf("unsupported Codex terminal status %q", params.Turn.Status)
		}
	default:
		// The pinned protocol is explicitly additive. Notifications unrelated to
		// the public text/tool/terminal contract are safe to ignore.
		return runtime.Outcome{}, false, nil
	}
}

func translateItem(completed bool, raw json.RawMessage, sink runtime.EventSink, tools map[string]string) error {
	var item struct {
		Type             string `json:"type"`
		ID               string `json:"id"`
		Command          string `json:"command"`
		CWD              string `json:"cwd"`
		AggregatedOutput string `json:"aggregatedOutput"`
		ExitCode         *int   `json:"exitCode"`
		Changes          any    `json:"changes"`
		Server           string `json:"server"`
		Tool             string `json:"tool"`
		Arguments        any    `json:"arguments"`
		Result           any    `json:"result"`
		Error            any    `json:"error"`
		Prompt           string `json:"prompt"`
		Status           string `json:"status"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return fmt.Errorf("decode Codex thread item: %w", err)
	}
	if item.ID == "" {
		return nil
	}
	name := ""
	var arguments map[string]any
	result := map[string]any{"status": item.Status}
	switch item.Type {
	case "commandExecution":
		name, arguments = "command_execution", map[string]any{"command": bounded(item.Command), "cwd": bounded(item.CWD)}
		result["output"] = bounded(item.AggregatedOutput)
		if item.ExitCode != nil {
			result["exitCode"] = *item.ExitCode
		}
	case "fileChange":
		name, arguments = "file_change", map[string]any{"changes": boundValue(item.Changes)}
		result["changes"] = boundValue(item.Changes)
	case "mcpToolCall":
		name, arguments = item.Server+"."+item.Tool, map[string]any{"arguments": boundValue(item.Arguments)}
		result["result"], result["error"] = boundValue(item.Result), boundValue(item.Error)
	case "collabAgentToolCall":
		name, arguments = "Agent", map[string]any{"prompt": bounded(item.Prompt), "tool": item.Tool}
	default:
		return nil
	}
	if !completed {
		if _, exists := tools[item.ID]; exists {
			return fmt.Errorf("codex tool item %q started more than once", item.ID)
		}
		tools[item.ID] = name
		return sink.ToolCall(runtime.ToolCall{ID: item.ID, Name: name, Arguments: arguments})
	}
	startedName, exists := tools[item.ID]
	if !exists {
		return fmt.Errorf("codex tool item %q completed without starting", item.ID)
	}
	if startedName != name {
		return fmt.Errorf("codex tool item %q changed name from %q to %q", item.ID, startedName, name)
	}
	delete(tools, item.ID)
	return sink.ToolResult(runtime.ToolResult{ID: item.ID, Name: name, Result: result, IsError: item.Status == "failed"})
}

func closeActiveTools(sink runtime.EventSink, tools map[string]string) error {
	ids := make([]string, 0, len(tools))
	for id := range tools {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if err := sink.ToolResult(runtime.ToolResult{
			ID: id, Name: tools[id], Result: map[string]any{"error": "Codex turn ended before tool completion"}, IsError: true,
		}); err != nil {
			return err
		}
		delete(tools, id)
	}
	return nil
}

func boundValue(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{"omitted": "unencodable payload"}
	}
	if len(raw) <= 16<<10 {
		return value
	}
	return map[string]any{"omitted": "payload exceeded 16384 bytes"}
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

func bounded(value string) string {
	const limit = 16 << 10
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func responseThreadID(raw json.RawMessage) (string, error) {
	var response struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", fmt.Errorf("decode Codex thread response: %w", err)
	}
	if response.Thread.ID == "" {
		return "", fmt.Errorf("codex thread response omitted thread ID")
	}
	return response.Thread.ID, nil
}

func responseTurnID(raw json.RawMessage) (string, error) {
	var response struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", fmt.Errorf("decode Codex turn response: %w", err)
	}
	if response.Turn.ID == "" {
		return "", fmt.Errorf("codex turn response omitted turn ID")
	}
	return response.Turn.ID, nil
}

func (d *ProcessDriver) protocolError(err error, stderr *utils.BoundedBuffer) error {
	message := strings.TrimSpace(stderr.String())
	if message == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, message)
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type rpcFrame struct {
	message rpcMessage
	err     error
}

type rpcClient struct {
	writer  io.WriteCloser
	writeMu sync.Mutex
	frames  chan rpcFrame
}

func newRPCClient(writer io.WriteCloser, reader io.Reader, maxFrameBytes int) *rpcClient {
	c := &rpcClient{writer: writer, frames: make(chan rpcFrame, 16)}
	go c.read(reader, maxFrameBytes)
	return c
}

func (c *rpcClient) read(reader io.Reader, max int) {
	defer close(c.frames)
	buffered := bufio.NewReaderSize(reader, max+1)
	for {
		line, err := buffered.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			c.frames <- rpcFrame{err: fmt.Errorf("codex frame exceeds %d bytes", max)}
			return
		}
		if len(line) > max {
			c.frames <- rpcFrame{err: fmt.Errorf("codex frame exceeds %d bytes", max)}
			return
		}
		if len(bytes.TrimSpace(line)) != 0 {
			var message rpcMessage
			if decodeErr := json.Unmarshal(line, &message); decodeErr != nil {
				c.frames <- rpcFrame{err: fmt.Errorf("decode Codex JSON-RPC frame: %w", decodeErr)}
				return
			}
			if message.JSONRPC != "" && message.JSONRPC != "2.0" {
				c.frames <- rpcFrame{err: fmt.Errorf("unsupported Codex JSON-RPC version %q", message.JSONRPC)}
				return
			}
			c.frames <- rpcFrame{message: message}
		}
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			c.frames <- rpcFrame{err: fmt.Errorf("read Codex JSON-RPC frame: %w", err)}
			return
		}
	}
}

func (c *rpcClient) call(ctx context.Context, id int, method string, params any) (json.RawMessage, error) {
	if err := c.write(rpcMessage{JSONRPC: "2.0", ID: json.RawMessage(fmt.Sprintf("%d", id)), Method: method}, params); err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case frame, ok := <-c.frames:
			if !ok {
				return nil, fmt.Errorf("codex protocol stream closed while awaiting %s", method)
			}
			if frame.err != nil {
				return nil, frame.err
			}
			message := frame.message
			if message.Method != "" {
				if len(message.ID) != 0 {
					return nil, fmt.Errorf("unsupported Codex server request %q", message.Method)
				}
				continue
			}
			if string(message.ID) != fmt.Sprintf("%d", id) {
				return nil, fmt.Errorf("unexpected Codex response ID %s", message.ID)
			}
			if message.Error != nil {
				return nil, fmt.Errorf("codex %s failed: %s", method, bounded(message.Error.Message))
			}
			return message.Result, nil
		}
	}
}

func (c *rpcClient) notify(method string, params any) error {
	return c.write(rpcMessage{JSONRPC: "2.0", Method: method}, params)
}

func (c *rpcClient) write(message rpcMessage, params any) error {
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return err
		}
		message.Params = raw
	}
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode Codex JSON-RPC frame: %w", err)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.writer.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write Codex JSON-RPC frame: %w", err)
	}
	return nil
}
