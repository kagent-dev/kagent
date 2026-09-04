// Package driver translates the Codex App Server protocol into the
// runtime-neutral events consumed by the shared A2A executor.
package driver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/harness/internal/utils"
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
}

// ProcessDriver supervises one Codex App Server process per runtime turn.
type ProcessDriver struct{ config ProcessConfig }

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
	return d.consume(ctx, client, command, wait, stderr, newEventTranslator(threadID, turnID), sink)
}

func (d *ProcessDriver) consume(ctx context.Context, client *rpcClient, command *exec.Cmd, wait <-chan error, stderr *utils.BoundedBuffer, translator *eventTranslator, sink runtime.EventSink) (runtime.Outcome, error) {
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
				return runtime.Outcome{}, fmt.Errorf("unsupported Codex server request %q", message.Method)
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
