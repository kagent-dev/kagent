// Package supervisor owns one DSH process tree, including detached tools.
//
// TODO: adopt shared turn-process ownership after
// https://github.com/kagent-dev/kagent/pull/3131 (built on PR #3006) lands and
// exposes an owner usable by native harnesses. Keep the local supervisor until
// that owner also reaps descendants that leave their process group and confirms
// cleanup after supervisor death; tracking process groups alone is insufficient.
package supervisor

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// Mode is a private re-exec entry point, handled before application startup.
const Mode = "--dsh-supervise"

const (
	guardStage      = "guard"
	payloadStage    = "payload"
	cleanupComplete = byte(1)
)

// Command re-executes this binary as a dedicated supervisor for one DSH turn.
func Command(executable string, args ...string) (*exec.Cmd, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("DSH BYO process supervision requires Linux")
	}
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to locate DSH supervisor executable: %w", err)
	}
	cmd := exec.Command(self, append([]string{Mode, guardStage, executable}, args...)...)
	// Bound inherited output pipes after guard death. Cleanup is independently
	// confirmed by Wait, so closing an output pipe cannot release a live tool.
	cmd.WaitDelay = time.Second
	return cmd, nil
}

// Process owns the lifetime and cleanup-confirmation pipes for a supervised tree.
type Process struct {
	cmd        *exec.Cmd
	lifetime   *os.File
	completion *os.File
}

var _ io.Closer = (*Process)(nil)

// Start gives both supervisors private pipes. Closing the lifetime requests
// forced cleanup; adapter death also closes it. Payloads inherit neither pipe.
func Start(cmd *exec.Cmd) (*Process, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create DSH cleanup pipe: %w", err)
	}
	lifetime, err := startSupervised(cmd, writer)
	_ = writer.Close()
	if err != nil {
		_ = reader.Close()
		return nil, err
	}
	return &Process{cmd: cmd, lifetime: lifetime, completion: reader}, nil
}

func (p *Process) Close() error { return p.lifetime.Close() }

// Wait must be called once. Process exit alone does not prove cleanup: a
// supervisor may have been killed while the surviving supervisor is reaping.
func (p *Process) Wait() error {
	err := p.cmd.Wait()
	var confirmation [1]byte
	_, readErr := io.ReadFull(p.completion, confirmation[:])
	_ = p.completion.Close()
	if readErr != nil || confirmation[0] != cleanupComplete {
		// Both supervisors were lost, or the private protocol broke. Keep the
		// task unsettled: replacing the Actor restores process containment, but
		// does not itself reconcile the controller's task state.
		// TODO: use controller-owned fencing and task recovery once
		// https://github.com/kagent-dev/kagent/issues/3010 is resolved with that
		// contract. Never turn missing cleanup proof into local task completion.
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("dsh cleanup could not be confirmed; actor replacement required", "error", readErr)
		// A bare select would let Go's deadlock detector terminate a check-only
		// process with no server goroutines. Keep this failure closed there too.
		for {
			time.Sleep(time.Hour)
		}
	}
	return err
}

func startSupervised(cmd *exec.Cmd, completion *os.File) (*os.File, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create DSH lifetime pipe: %w", err)
	}
	cmd.ExtraFiles = []*os.File{reader, completion}
	err = cmd.Start()
	_ = reader.Close()
	if err != nil {
		_ = writer.Close()
		return nil, err
	}
	return writer, nil
}
