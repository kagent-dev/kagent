// Package supervisor owns one DSH process tree, including detached tools.
package supervisor

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
)

// Mode is a private re-exec entry point, handled before application startup.
const Mode = "--dsh-supervise"

// Command re-executes this binary as a dedicated supervisor for one DSH turn.
func Command(executable string, args ...string) (*exec.Cmd, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("DSH BYO process supervision requires Linux")
	}
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to locate DSH supervisor executable: %w", err)
	}
	return exec.Command(self, append([]string{Mode, executable}, args...)...), nil
}

// Start gives the supervisor a private lifetime pipe. Closing it requests forced
// cleanup; adapter death also closes it. DSH and its tools never inherit it.
func Start(cmd *exec.Cmd) (io.Closer, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create DSH lifetime pipe: %w", err)
	}
	cmd.ExtraFiles = []*os.File{reader}
	err = cmd.Start()
	_ = reader.Close()
	if err != nil {
		_ = writer.Close()
		return nil, err
	}
	return writer, nil
}
