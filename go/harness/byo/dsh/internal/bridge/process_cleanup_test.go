//go:build linux

package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/config"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

func TestStopReapsDetachedTool(t *testing.T) {
	for _, mode := range []string{"forced", "normal", "orphan"} {
		t.Run(mode, func(t *testing.T) { testDetachedCleanup(t, mode) })
	}
}

func testDetachedCleanup(t *testing.T, mode string) {
	t.Helper()
	sentinel := exec.Command("/bin/sleep", "60")
	require.NoError(t, sentinel.Start())
	t.Cleanup(func() { _ = sentinel.Process.Kill(); _ = sentinel.Wait() })
	executable, err := os.Executable()
	require.NoError(t, err)
	dir := t.TempDir()
	script := dir + "/dsh"
	pidFile := dir + "/tool.pid"
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nexec '"+strings.ReplaceAll(executable, "'", "'\\''")+"' -test.run=TestDetachedDSHProcess\n"), 0700))
	cfg, err := config.Parse([]byte(`{"model":{"type":"openai","model":"mock"},"runtime":{"control_plane_url":"http://controller:8083"}}`))
	require.NoError(t, err)
	runner, err := New(cfg, script, t.TempDir(), append(os.Environ(), "DSH_AUDIT_DETACHED="+pidFile, "DSH_AUDIT_EXIT="+mode))
	require.NoError(t, err)
	defer func() {
		if data, err := os.ReadFile(pidFile); err == nil {
			pid, _ := strconv.Atoi(string(data))
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, err = runner.Run(ctx, runtime.Turn{Prompt: "work"}, &recordingSink{})
	if mode == "forced" {
		require.Error(t, err)
	} else {
		require.NoError(t, err)
	}
	require.NoError(t, syscall.Kill(sentinel.Process.Pid, 0), "cleanup must not affect unrelated processes")
	data, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	pid, err := strconv.Atoi(string(data))
	require.NoError(t, err)
	live := syscall.Kill(pid, 0) == nil
	t.Logf("separate process group tool PID=%d still alive after DSH cleanup: %t", pid, live)
	require.False(t, live, "forced cleanup must not release the Actor with a running tool process")
}
func TestDetachedDSHProcess(t *testing.T) {
	pidFile := os.Getenv("DSH_AUDIT_DETACHED")
	if pidFile == "" {
		return
	}
	if os.WriteFile(pidFile+".dsh", []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
		os.Exit(2)
	}
	signal.Ignore(syscall.SIGTERM)
	if os.Getenv("DSH_AUDIT_EXIT") == "orphan" {
		executable, err := os.Executable()
		if err != nil {
			os.Exit(2)
		}
		child := exec.Command(executable, "-test.run=TestDetachedOrphanProcess")
		if child.Run() != nil {
			os.Exit(2)
		}
	} else {
		child := exec.Command("/bin/sleep", "60")
		child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
			os.Exit(2)
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var m message
		if json.Unmarshal(scanner.Bytes(), &m) != nil {
			os.Exit(2)
		}
		switch m.Method {
		case "initialize":
			fmt.Println(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{}}}`)
		case "session/new":
			fmt.Println(`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"audit-detached"}}`)
		case "session/prompt":
			fmt.Println(`{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn"}}`)
		case "session/close":
			fmt.Println(`{"jsonrpc":"2.0","id":4,"result":{}}`)
		}
	}
	if os.Getenv("DSH_AUDIT_EXIT") != "forced" {
		os.Exit(0)
	}
	for {
		time.Sleep(time.Second)
	}
}

func TestDetachedOrphanProcess(t *testing.T) {
	path := os.Getenv("DSH_AUDIT_DETACHED")
	if path == "" {
		return
	}
	child := exec.Command("/bin/sleep", "60")
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if child.Start() != nil {
		os.Exit(2)
	}
	if os.WriteFile(path, []byte(strconv.Itoa(child.Process.Pid)), 0600) != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestAdapterDeathReapsDetachedTools(t *testing.T) {
	executable, err := os.Executable()
	require.NoError(t, err)
	pidFile := t.TempDir() + "/tool.pid"
	owner := exec.Command(executable, "-test.run=TestDSHLifetimeOwner")
	owner.Env = append(os.Environ(), "DSH_AUDIT_OWNER="+pidFile)
	require.NoError(t, owner.Start())
	t.Cleanup(func() { _ = owner.Process.Kill(); _ = owner.Wait() })
	var toolPID, dshPID int
	require.Eventually(t, func() bool {
		toolData, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		dshData, err := os.ReadFile(pidFile + ".dsh")
		if err != nil {
			return false
		}
		toolPID, err = strconv.Atoi(string(toolData))
		if err != nil {
			return false
		}
		dshPID, err = strconv.Atoi(string(dshData))
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, owner.Process.Kill())
	require.Error(t, owner.Wait())
	require.Eventually(t, func() bool {
		return syscall.Kill(toolPID, 0) == syscall.ESRCH && syscall.Kill(dshPID, 0) == syscall.ESRCH
	}, 5*time.Second, 10*time.Millisecond, "supervisor must reap DSH and its detached tool after adapter death")
}

func TestDSHLifetimeOwner(t *testing.T) {
	pidFile := os.Getenv("DSH_AUDIT_OWNER")
	if pidFile == "" {
		return
	}
	executable, err := os.Executable()
	require.NoError(t, err)
	script := t.TempDir() + "/dsh"
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nexec '"+strings.ReplaceAll(executable, "'", "'\\''")+"' -test.run=TestDetachedDSHProcess\n"), 0700))
	cfg, err := config.Parse([]byte(`{"model":{"type":"openai","model":"mock"},"runtime":{"control_plane_url":"http://controller:8083"}}`))
	require.NoError(t, err)
	runner, err := New(cfg, script, t.TempDir(), append(os.Environ(), "DSH_AUDIT_DETACHED="+pidFile, "DSH_AUDIT_EXIT=forced"))
	require.NoError(t, err)
	_, _ = runner.Run(t.Context(), runtime.Turn{Prompt: "work"}, &recordingSink{})
	os.Exit(3)
}
