package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestE2EInvokeAgentThroughMCPServeAgents(t *testing.T) {
	kagentURL := os.Getenv("KAGENT_URL")
	if kagentURL == "" {
		kagentURL = "http://localhost:8083"
	}

	_, testFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	goModuleRoot := filepath.Clean(filepath.Join(filepath.Dir(testFile), "../.."))

	kagentBin := filepath.Join(t.TempDir(), "kagent")
	build := exec.Command("go", "build", "-o", kagentBin, "./cli/cmd/kagent")
	build.Dir = goModuleRoot
	buildOutput, err := build.CombinedOutput()
	require.NoError(t, err, string(buildOutput))

	homeDir := t.TempDir()
	cfgDir := filepath.Join(homeDir, ".kagent")
	require.NoError(t, os.MkdirAll(cfgDir, 0755))
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(fmt.Sprintf("kagent_url: %s\nnamespace: kagent\ntimeout: 300s\n", kagentURL)), 0644))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, kagentBin, "mcp", "serve-agents")
	cmd.Env = append(os.Environ(), "HOME="+homeDir)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	lines := make(chan string, 32)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()

	writeLine := func(line string) {
		_, _ = fmt.Fprintln(stdin, line)
	}

	readResponse := func(wantID int) json.RawMessage {
		deadline := time.NewTimer(15 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case line, ok := <-lines:
				require.True(t, ok, stderr.String())
				var msg struct {
					ID     int             `json:"id"`
					Result json.RawMessage `json:"result,omitempty"`
					Error  json.RawMessage `json:"error,omitempty"`
				}
				require.NoError(t, json.Unmarshal([]byte(line), &msg), line)
				if msg.ID != wantID {
					continue
				}
				require.Nil(t, msg.Error, line)
				return msg.Result
			case <-deadline.C:
				t.Fatalf("timed out waiting for id=%d; stderr=%s", wantID, stderr.String())
			}
		}
	}

	writeLine(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"e2e","version":"0.0.0"}}}`)
	_ = readResponse(1)
	writeLine(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)

	writeLine(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	toolsList := readResponse(2)
	var listResult struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(toolsList, &listResult), string(toolsList))
	require.GreaterOrEqual(t, len(listResult.Tools), 2)
	toolNames := make([]string, 0, len(listResult.Tools))
	for _, tool := range listResult.Tools {
		toolNames = append(toolNames, tool.Name)
	}
	require.Contains(t, toolNames, "list_agents")
	require.Contains(t, toolNames, "invoke_agent")

	writeLine(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_agents"}}`)
	agentsResult := readResponse(3)
	var callResult struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(agentsResult, &callResult), string(agentsResult))
	require.NotEmpty(t, callResult.Content)
	require.Contains(t, callResult.Content[0].Text, "kebab-agent")

	writeLine(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"invoke_agent","arguments":{"agent":"kebab-agent","task":"What can you do?"}}}`)
	invokeResult := readResponse(4)
	require.NoError(t, json.Unmarshal(invokeResult, &callResult), string(invokeResult))
	require.NotEmpty(t, callResult.Content)
	require.Contains(t, callResult.Content[0].Text, "kebab")
}
