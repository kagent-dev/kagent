package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
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

// commandMarkerVariable marks the environment of every process a command
// starts, with a value unique to the call. A descendant that leaves the
// command's process group (setsid, setpgid) keeps its environment, so the
// marker still finds it; only one that drops its environment (env -i) escapes.
const commandMarkerVariable = "KAGENT_BASH_CALL"

var commandCalls atomic.Uint64

func newCommandMarker() string {
	return fmt.Sprintf("%s=%d-%d", commandMarkerVariable, os.Getpid(), commandCalls.Add(1))
}

// commandReapWait bounds how long stopCommand waits for killed processes that
// are children of the runtime to exit so it can reap them.
const commandReapWait = time.Second

// stopCommand ends what is left of a command after its shell returned: the
// process group, and every process carrying the call's marker, setsid'd jobs
// included. A job outlives the shell and is reparented to the runtime when the
// runtime is the sandbox's PID 1 (or a subreaper); nothing else reaps it there,
// so stopCommand reaps the killed jobs that are its children.
func stopCommand(cmd *exec.Cmd, marker string) error {
	err := killCommandGroup(cmd)
	if cmd.Process == nil {
		return err
	}
	reapProcesses(append([]int{-cmd.Process.Pid}, killMarkedProcesses(marker)...))
	return err
}

// killMarkedProcesses kills every process whose environment carries marker and
// returns the ones that are children of this process. It scans again until a
// scan finds no process left, so a process forked during a scan is caught by
// the next. Without procfs there is nothing to scan and the group kill is all.
func killMarkedProcesses(marker string) []int {
	children := map[int]bool{}
	for range 10 {
		processes := markedProcesses(marker)
		if len(processes) == 0 {
			break
		}
		for pid, parent := range processes {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			if parent == os.Getpid() {
				children[pid] = true
			}
		}
	}
	return slices.Collect(maps.Keys(children))
}

// markedProcesses maps each live process whose environment carries marker to
// its parent's PID. An exited process has no environment left to match.
func markedProcesses(marker string) map[int]int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	processes := map[int]int{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		environ, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "environ"))
		if err != nil || !environmentContains(environ, marker) {
			continue
		}
		stat, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		if parent, ok := parentPID(stat); ok {
			processes[pid] = parent
		}
	}
	return processes
}

// environmentContains reports whether the NUL-separated environ of a process
// holds the entry variable=value exactly.
func environmentContains(environ []byte, entry string) bool {
	return bytes.Contains(append([]byte{0}, environ...), []byte("\x00"+entry+"\x00"))
}

// parentPID reads the parent's PID from a /proc/<pid>/stat line, whose fields
// after the parenthesised command name are the state, then the parent's PID.
func parentPID(stat []byte) (int, bool) {
	_, after, found := bytes.CutLast(stat, []byte(")"))
	if !found {
		return 0, false
	}
	fields := strings.Fields(string(after))
	if len(fields) < 2 {
		return 0, false
	}
	parent, err := strconv.Atoi(fields[1])
	return parent, err == nil
}

// reapProcesses reaps the given children of this process (a negative ID is a
// process group), waiting up to commandReapWait for killed ones to exit. It
// waits on those IDs only, never on a shell another call's os/exec waits for.
// A child's PID and a group's ID stay taken until reaped, so none is reused.
func reapProcesses(ids []int) {
	deadline := time.Now().Add(commandReapWait)
	for {
		ids = slices.DeleteFunc(ids, func(id int) bool {
			for {
				pid, err := syscall.Wait4(id, nil, syscall.WNOHANG, nil)
				switch {
				case errors.Is(err, syscall.EINTR), pid > 0 && id < 0:
					continue
				default:
					// ECHILD: nothing of id is a child of this process any more.
					return err != nil || pid > 0
				}
			}
		})
		if len(ids) == 0 || time.Now().After(deadline) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
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
	marker := newCommandMarker()
	cmd.Env = append(commandEnvironment(os.Environ(), cfg), marker)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killCommandGroup(cmd) }
	// Descendants can keep output pipes open after bash exits. Bound that wait,
	// then stop everything the command started in the deferred cleanup.
	cmd.WaitDelay = time.Second
	defer func() {
		if err := stopCommand(cmd, marker); err != nil && !errors.Is(err, os.ErrProcessDone) {
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
