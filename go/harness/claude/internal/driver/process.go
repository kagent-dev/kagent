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
	Environment        []string
	MaxEventBytes      int
	MaxStderrBytes     int
	InterruptGrace     time.Duration
}

// ProcessDriver supervises one Claude Code process per runtime turn.
type ProcessDriver struct {
	config ProcessConfig
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

// Run supervises one Claude Code process and emits its ordered runtime events.
func (d *ProcessDriver) Run(ctx context.Context, turn runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
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
	var waitOnce sync.Once
	waitForExit := func() <-chan error {
		waitOnce.Do(func() {
			go func() { waitDone <- cmd.Wait() }()
		})
		return waitDone
	}
	var terminal *runtime.Outcome

	for {
		select {
		case item, ok := <-items:
			if !ok {
				return runtime.Outcome{}, fmt.Errorf("claude parser stopped without a result")
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
				d.terminate(cmd, waitForExit())
				for range items {
				}
				return runtime.Outcome{}, err
			}
			if item.err != nil {
				close(stopEmit)
				d.terminate(cmd, waitForExit())
				return runtime.Outcome{}, item.err
			}
			// StdoutPipe requires all reads to complete before Wait closes the pipe.
			// The parser's nil result is the EOF boundary, so start Wait only now.
			if waitErr := <-waitForExit(); waitErr != nil {
				return runtime.Outcome{}, fmt.Errorf("claude exited with an error: %w: %s", waitErr, stderr.String())
			}
			if terminal == nil {
				return runtime.Outcome{}, fmt.Errorf("claude process exited without a terminal result")
			}
			return *terminal, nil
		case <-ctx.Done():
			close(stopEmit)
			d.terminate(cmd, waitForExit())
			for range items {
			}
			return runtime.Outcome{}, ctx.Err()
		}
	}
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

func (d *ProcessDriver) terminate(cmd *exec.Cmd, waitDone <-chan error) {
	_ = utils.InterruptProcessGroup(cmd.Process)
	timer := time.NewTimer(d.config.InterruptGrace)
	defer timer.Stop()
	select {
	case <-waitDone:
		// The group leader can exit on the interrupt while a descendant that
		// ignores it remains alive. Kill any processes still in the group.
		_ = utils.KillProcessGroup(cmd.Process)
	case <-timer.C:
		_ = utils.KillProcessGroup(cmd.Process)
		<-waitDone
	}
}
