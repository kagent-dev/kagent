package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

const (
	defaultCommandTimeout   = 60 * time.Second
	maxCommandOutput        = 64 * 1024
	commandTruncationMarker = "\n[output truncated at 65536 bytes; redirect output to a file for the full result]"
)

type commandConfig struct {
	directory       string
	skillsDirectory string
	bashVenvPath    string
	timeout         time.Duration
}

// commandOutput keeps draining the pipe after the capture limit. Stopping reads
// would block a verbose child and turn output truncation into a command timeout.
// os/exec uses one copying goroutine when stdout and stderr share this writer.
var _ io.Writer = (*commandOutput)(nil)

type commandOutput struct {
	buffer    bytes.Buffer
	truncated bool
}

func (o *commandOutput) Write(p []byte) (int, error) {
	n := len(p)
	keep := min(n, maxCommandOutput-o.buffer.Len())
	_, err := o.buffer.Write(p[:keep])
	o.truncated = o.truncated || keep < n
	return n, err
}

func (o *commandOutput) text() string {
	output := strings.TrimSpace(strings.ToValidUTF8(o.buffer.String(), "�"))
	if o.truncated {
		output += commandTruncationMarker
	}
	return output
}

// commandEnvironment preserves the Actor environment, including Substrate's
// egress SDK placeholders. The sandbox owns isolation; this is not a secret filter.
func commandEnvironment(base []string, cfg commandConfig) []string {
	value := func(key string) string {
		for _, variable := range slices.Backward(base) {
			k, v, _ := strings.Cut(variable, "=")
			if k == key {
				return v
			}
		}
		return ""
	}
	env := append([]string(nil), base...)
	pythonPath := []string{cfg.directory}
	if cfg.skillsDirectory != "" {
		pythonPath = append(pythonPath, cfg.skillsDirectory)
	}
	if existing := value("PYTHONPATH"); existing != "" {
		pythonPath = append(pythonPath, existing)
	}
	env = append(env, "PYTHONPATH="+strings.Join(pythonPath, string(os.PathListSeparator)))
	if cfg.bashVenvPath != "" {
		env = append(env, "PATH="+filepath.Join(cfg.bashVenvPath, "bin")+string(os.PathListSeparator)+value("PATH"), "VIRTUAL_ENV="+cfg.bashVenvPath)
	}
	return env
}

func killCommandGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

// executeCommand owns a command and its descendants for the duration of one tool
// call. Background jobs are stopped on return; durable files live in the workspace.
func executeCommand(ctx context.Context, command string, cfg commandConfig) (output string, resultErr error) {
	timeout := cfg.timeout
	if timeout == 0 {
		timeout = defaultCommandTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = cfg.directory
	cmd.Env = commandEnvironment(os.Environ(), cfg)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killCommandGroup(cmd) }
	// Descendants can keep output pipes open after bash exits. Bound that wait,
	// then stop the remaining process group in the deferred cleanup.
	cmd.WaitDelay = time.Second
	defer func() {
		if err := killCommandGroup(cmd); err != nil && !errors.Is(err, os.ErrProcessDone) {
			resultErr = errors.Join(resultErr, fmt.Errorf("failed to stop command processes: %w", err))
		}
	}()
	var captured commandOutput
	cmd.Stdout, cmd.Stderr = &captured, &captured
	err := cmd.Run()
	output = captured.text()
	if ctx.Err() != nil {
		return output, fmt.Errorf("command interrupted: %w", ctx.Err())
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return output, fmt.Errorf("command failed with exit code %d: %w", exitErr.ExitCode(), err)
	}
	if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		return output, fmt.Errorf("failed to execute command: %w", err)
	}
	if output == "" {
		output = "Command completed successfully."
	}
	return output, nil
}
