package driver

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

type ProcessConfig struct {
	Executable         string
	ExpectedVersion    string
	StrictVersion      bool
	Workspace          string
	Model              string
	AppendSystemPrompt string
	AgentsJSON         string
	MCPConfigPath      string
	Environment        []string
	MaxEventBytes      int
	MaxStderrBytes     int
	InterruptGrace     time.Duration
}

type ProcessDriver struct {
	config ProcessConfig
}

func NewProcessDriver(config ProcessConfig) *ProcessDriver {
	return &ProcessDriver{config: config}
}

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
		return fmt.Errorf("Claude version mismatch: got %q, expected %q", version, d.config.ExpectedVersion)
	}
	return nil
}

func (d *ProcessDriver) Args(turn runtime.Turn) []string {
	args := []string{
		"-p", turn.Prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--dangerously-skip-permissions",
		"--strict-mcp-config",
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
	if turn.ContinuationID != "" {
		// Resume the Actor's exact root conversation. --continue selects Claude's
		// latest session and can be redirected by subagents or interrupted attempts.
		args = append(args, "--resume", turn.ContinuationID)
	}
	return args
}

func (d *ProcessDriver) Run(ctx context.Context, turn runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
	cmd := exec.Command(d.config.Executable, d.Args(turn)...)
	cmd.Dir = d.config.Workspace
	cmd.Env = append([]string(nil), d.config.Environment...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return runtime.Outcome{}, fmt.Errorf("open Claude stdout: %w", err)
	}
	stderr := &boundedBuffer{max: d.config.MaxStderrBytes}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return runtime.Outcome{}, fmt.Errorf("start Claude: %w", err)
	}
	type parseItem struct {
		event *Event
		err   error
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
	go func() { waitDone <- cmd.Wait() }()
	var terminal *runtime.Outcome

	for {
		select {
		case item, ok := <-items:
			if !ok {
				return runtime.Outcome{}, fmt.Errorf("Claude parser stopped without a result")
			}
			if item.event != nil {
				outcome, err := emitEvent(*item.event, sink, terminal != nil)
				if err == nil {
					if outcome != nil {
						terminal = outcome
					}
					continue
				}
				close(stopEmit)
				d.terminate(cmd, waitDone)
				for range items {
				}
				return runtime.Outcome{}, err
			}
			if item.err != nil {
				close(stopEmit)
				d.terminate(cmd, waitDone)
				return runtime.Outcome{}, item.err
			}
			if waitErr := <-waitDone; waitErr != nil {
				return runtime.Outcome{}, fmt.Errorf("Claude exited with an error: %w: %s", waitErr, stderr.String())
			}
			if terminal == nil {
				return runtime.Outcome{}, fmt.Errorf("Claude process exited without a terminal result")
			}
			return *terminal, nil
		case <-ctx.Done():
			close(stopEmit)
			d.terminate(cmd, waitDone)
			for range items {
			}
			return runtime.Outcome{}, ctx.Err()
		}
	}
}

func emitEvent(event Event, sink runtime.EventSink, terminal bool) (*runtime.Outcome, error) {
	if terminal {
		return nil, fmt.Errorf("Claude emitted activity after its terminal result")
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
			return nil, fmt.Errorf("Claude tool activity has unsupported phase %q", event.ToolPhase)
		}
	case EventCompleted:
		return &runtime.Outcome{}, nil
	case EventFailed:
		return &runtime.Outcome{Failure: &runtime.Failure{Message: event.SafeMessage}}, nil
	default:
		return nil, fmt.Errorf("unsupported Claude event kind %q", event.Kind)
	}
}

func (d *ProcessDriver) terminate(cmd *exec.Cmd, waitDone <-chan error) {
	_ = cmd.Process.Signal(os.Interrupt)
	timer := time.NewTimer(d.config.InterruptGrace)
	defer timer.Stop()
	select {
	case <-waitDone:
	case <-timer.C:
		_ = cmd.Process.Kill()
		<-waitDone
	}
}

type boundedBuffer struct {
	bytes.Buffer
	max int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	remaining := b.max - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	return original, nil
}
