//go:build linux

package supervisor

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Run must execute in a dedicated process: subreaper status and wait4 belong
// to that process, never to the long-lived A2A server. It returns only after
// the main child and all adopted descendants have been reaped.
func Run(args []string) int {
	completion, err := privatePipe(4, "dsh-cleanup")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer completion.Close()
	err = run(args, completion)
	// run returns only before starting a child or after ECHILD. Either
	// supervisor can confirm cleanup if the other was killed.
	_, _ = completion.Write([]byte{cleanupComplete})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func run(args []string, completion *os.File) error {
	if len(args) < 2 || (args[0] != guardStage && args[0] != payloadStage) {
		return fmt.Errorf("DSH supervisor requires an executable")
	}
	control, err := privatePipe(3, "dsh-lifetime")
	if err != nil {
		return err
	}
	defer control.Close()
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("failed to enable DSH child supervision: %w", err)
	}
	// Check procfs before starting any payload; there is no unsupervised fallback.
	if _, err := childPIDs(); err != nil {
		return err
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	parentGone := make(chan struct{})
	go func() { var b [1]byte; _, _ = control.Read(b[:]); close(parentGone) }()
	cmd := exec.Command(args[1], args[2:]...)
	if args[0] == guardStage {
		self, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to locate DSH supervisor executable: %w", err)
		}
		cmd = exec.Command(self, append([]string{Mode, payloadStage}, args[1:]...)...)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if args[0] == guardStage {
		// If this guard dies, EOF makes the inner supervisor clean up. If the
		// inner supervisor dies, its descendants are adopted by this guard.
		lifetime, startErr := startSupervised(cmd, completion)
		err = startErr
		if lifetime != nil {
			defer lifetime.Close()
		}
	} else {
		err = cmd.Start()
	}
	if err != nil {
		return fmt.Errorf("failed to launch DSH payload: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-parentGone:
		waitErr = killAndWait(cmd, done)
	case <-signals:
		waitErr = killAndWait(cmd, done)
	}
	// The main child is already reaped. No other goroutine may wait on children
	// while cleanup enumerates and signals them, preventing PID-reuse races.
	return errors.Join(waitErr, reapDescendants())
}

func privatePipe(fd int, name string) (*os.File, error) {
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		return nil, fmt.Errorf("DSH supervisor requires %s pipe", name)
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		_ = file.Close()
		return nil, fmt.Errorf("DSH supervisor requires %s pipe", name)
	}
	unix.CloseOnExec(fd)
	return file, nil
}

func killAndWait(cmd *exec.Cmd, done <-chan error) error {
	err := cmd.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		err = nil
	}
	return errors.Join(err, <-done)
}

func reapDescendants() error {
	var firstErr error
	for {
		empty, err := reapPass()
		if err != nil && firstErr == nil {
			firstErr = err
			slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("failed to clean up dsh descendants", "error", err)
		}
		if empty {
			return firstErr
		}
		// Never release the Actor on a cleanup failure. Retry until wait4 proves
		// there are no children; kernel-level uninterruptible tasks can delay this.
		time.Sleep(10 * time.Millisecond)
	}
}

func reapPass() (bool, error) {
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if errors.Is(err, syscall.ECHILD) {
			return true, nil
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("failed to reap DSH descendant: %w", err)
		}
		if pid > 0 {
			continue
		}
		children, err := childPIDs()
		if err != nil {
			return false, err
		}
		for _, child := range children {
			if err := syscall.Kill(child, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				return false, fmt.Errorf("failed to stop DSH descendant: %w", err)
			}
		}
		// Children can exit or reparent their children during enumeration. An empty
		// procfs snapshot is never proof of completion: only wait4 ECHILD is.
		return false, nil
	}
}

func childPIDs() ([]int, error) {
	paths, err := filepath.Glob("/proc/self/task/*/children")
	if err != nil || len(paths) == 0 {
		return nil, fmt.Errorf("DSH supervision requires procfs child lists")
	}
	var children []int
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		} // A Go runtime thread may have exited.
		if err != nil {
			return nil, fmt.Errorf("failed to read DSH child list: %w", err)
		}
		for value := range strings.FieldsSeq(string(data)) {
			pid, err := strconv.Atoi(value)
			if err != nil || pid <= 0 {
				return nil, fmt.Errorf("invalid PID in DSH child list")
			}
			children = append(children, pid)
		}
	}
	return children, nil
}
