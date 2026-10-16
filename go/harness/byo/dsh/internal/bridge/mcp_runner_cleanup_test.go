//go:build linux

package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/config"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

type auditLaunch struct {
	PID  int    `json:"pid"`
	Home string `json:"home"`
}

func TestRunnerCleansUpAfterInvalidMCPToolSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		var request message
		_ = json.NewDecoder(r.Body).Decode(&request)
		if len(request.ID) == 0 {
			w.WriteHeader(202)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		result := json.RawMessage(`{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"serverInfo":{"name":"audit","version":"1"}}`)
		if request.Method == "tools/list" {
			result = json.RawMessage(`{"tools":[{"name":"broken","inputSchema":{"type":"array"}}]}`)
		}
		_ = json.NewEncoder(w).Encode(message{JSONRPC: "2.0", ID: request.ID, Result: result})
	}))
	defer server.Close()
	cfg, err := config.Parse([]byte(fmt.Sprintf(`{"model":{"type":"openai","model":"mock"},"runtime":{"control_plane_url":"http://controller:8083"},"http_tools":[{"params":{"url":%q}}]}`, server.URL)))
	require.NoError(t, err)
	executable, err := os.Executable()
	require.NoError(t, err)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "launch.json")
	script := filepath.Join(dir, "dsh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nexec '"+strings.ReplaceAll(executable, "'", "'\\''")+"' -test.run=TestMCPRunnerCleanupHelperProcess -- \"$@\"\n"), 0700))
	runner, err := New(cfg, script, filepath.Join(dir, "data"), append(os.Environ(), "DSH_AUDIT_LAUNCH="+pidFile))
	require.NoError(t, err)
	var launch auditLaunch
	t.Cleanup(func() {
		if launch.PID > 0 {
			_ = syscall.Kill(-launch.PID, syscall.SIGTERM)
			deadline := time.Now().Add(time.Second)
			for syscall.Kill(launch.PID, 0) == nil && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
		}
		if strings.HasPrefix(launch.Home, filepath.Join(os.TempDir(), "kagent-dsh-")) {
			_ = os.RemoveAll(launch.Home)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var runErr error
	require.NotPanics(t, func() {
		_, runErr = runner.Run(ctx, runtime.Turn{Prompt: "work"}, &recordingSink{})
	})
	require.Error(t, runErr, "malformed remote MCP schemas must return a startup error")
	deadline := time.Now().Add(time.Second)
	for {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			require.NoError(t, json.Unmarshal(data, &launch))
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("DSH test process did not record its PID")
		}
		time.Sleep(10 * time.Millisecond)
	}
	live := syscall.Kill(launch.PID, 0) == nil
	t.Logf("DSH process PID=%d still running after MCP schema failure: %t", launch.PID, live)
	require.False(t, live, "invalid remote tool schema must not leak the DSH subprocess")
}
func TestMCPRunnerCleanupHelperProcess(t *testing.T) {
	path := os.Getenv("DSH_AUDIT_LAUNCH")
	if path == "" {
		return
	}
	home := ""
	for i, arg := range os.Args {
		if arg == "--patch" && i+1 < len(os.Args) {
			home = filepath.Dir(os.Args[i+1])
		}
	}
	data, _ := json.Marshal(auditLaunch{PID: os.Getpid(), Home: home})
	if os.WriteFile(path, data, 0600) != nil {
		os.Exit(2)
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}
