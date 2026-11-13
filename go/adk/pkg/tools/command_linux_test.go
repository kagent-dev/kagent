package tools

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// In a sandbox the runtime is the entrypoint, PID 1, so a background job is
// reparented to it once the shell exits and only the runtime can reap it. The
// test process becomes a child subreaper to stand in for that: a job that is
// not running and not left as a zombie either is gone for kill(pid, 0).
func TestCommandStopsEveryProcessItStarted(t *testing.T) {
	require.NoError(t, unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0))
	t.Cleanup(func() { _ = unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 0, 0, 0, 0) })

	for _, tc := range []struct{ name, job string }{
		{"background job", "sh -c 'echo $$ > pid; exec sleep 600' &"},
		// setsid leaves the command's process group and session, so the group
		// kill alone does not reach it.
		{"setsid job", "setsid sh -c 'echo $$ > pid; exec sleep 600' &"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			command := tc.job + " while [ ! -s pid ]; do sleep 0.01; done; echo started"
			output, err := executeCommand(t.Context(), command, commandConfig{directory: directory})
			require.NoError(t, err)
			require.Equal(t, "started", output)

			content, err := os.ReadFile(filepath.Join(directory, "pid"))
			require.NoError(t, err)
			pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
			require.NoError(t, err)
			t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
			require.ErrorIs(t, syscall.Kill(pid, 0), syscall.ESRCH, "process %d of the command is left after it returned", pid)
		})
	}
}
