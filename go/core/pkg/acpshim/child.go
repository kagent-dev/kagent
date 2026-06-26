package acpshim

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// maxLineSize bounds a single JSON-RPC line read from the child. ACP
// messages can embed file contents, so this is generous.
const maxLineSize = 16 * 1024 * 1024 // 16 MiB

// child wraps a running stdio ACP agent subprocess. It exposes the child's
// stdout as a channel of newline-delimited lines and serializes writes to
// its stdin. The channel applies natural backpressure: if no client is
// draining it, the OS pipe buffer eventually fills and the child blocks.
type child struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	// out carries one element per stdout line (without trailing newline).
	out chan []byte
	// done is closed when the process has exited and out is drained-closed.
	done chan struct{}

	writeMu sync.Mutex

	exitMu  sync.Mutex
	exitErr error // valid after done is closed
}

// startChild spawns the configured agent process and begins reading its
// stdout. Child stderr is passed through to the shim's stderr so it lands
// in container logs.
func startChild(cfg *Config) (*child, error) {
	cmd := exec.Command(cfg.ChildArgv[0], cfg.ChildArgv[1:]...)
	cmd.Dir = cfg.ChildDir
	cmd.Env = append(os.Environ(), cfg.ChildEnv...)
	cmd.Stderr = os.Stderr
	// Put the child in its own process group so terminate() can signal the
	// whole tree (agents commonly fork helpers).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start child %q: %w", cfg.ChildArgv[0], err)
	}
	log.Printf("acp-shim: started child pid=%d argv=%q", cmd.Process.Pid, cfg.ChildArgv)

	c := &child{
		cmd:   cmd,
		stdin: stdin,
		out:   make(chan []byte, 64),
		done:  make(chan struct{}),
	}

	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), maxLineSize)
		for sc.Scan() {
			line := make([]byte, len(sc.Bytes()))
			copy(line, sc.Bytes())
			c.out <- line
		}
		if err := sc.Err(); err != nil {
			log.Printf("acp-shim: child stdout read error: %v", err)
		}
		err := cmd.Wait()
		c.exitMu.Lock()
		c.exitErr = err
		c.exitMu.Unlock()
		close(c.out)
		close(c.done)
		log.Printf("acp-shim: child pid=%d exited: %v", cmd.Process.Pid, err)
	}()

	return c, nil
}

// writeLine writes one JSON-RPC line (newline appended) to the child stdin.
func (c *child) writeLine(line []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.stdin.Write(line); err != nil {
		return fmt.Errorf("failed to write to child stdin: %w", err)
	}
	if _, err := c.stdin.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("failed to write newline to child stdin: %w", err)
	}
	return nil
}

// exited reports whether the child process has exited.
func (c *child) exited() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// exitError returns the child's exit error (nil for clean exit). Only
// meaningful after done is closed.
func (c *child) exitError() error {
	c.exitMu.Lock()
	defer c.exitMu.Unlock()
	return c.exitErr
}

// terminate sends SIGTERM to the child's process group, waits up to grace,
// then SIGKILLs. It blocks until the process has exited.
func (c *child) terminate(grace time.Duration) {
	if c.exited() {
		return
	}
	pgid := -c.cmd.Process.Pid
	_ = c.stdin.Close()
	_ = syscall.Kill(pgid, syscall.SIGTERM)
	select {
	case <-c.done:
		return
	case <-time.After(grace):
		log.Printf("acp-shim: child pid=%d did not exit within %s, sending SIGKILL", c.cmd.Process.Pid, grace)
		_ = syscall.Kill(pgid, syscall.SIGKILL)
		<-c.done
	}
}
