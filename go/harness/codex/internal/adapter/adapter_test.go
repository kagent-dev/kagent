package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/harness/codex/config"
	"github.com/pelletier/go-toml/v2"
)

func TestNewMaterializesCompilerOwnedConfiguration(t *testing.T) {
	durable := filepath.Join(t.TempDir(), "data")
	cfg := config.Production("gpt-5.2-codex", "line one\nline two")
	cfg.Provider = config.Provider{Name: "openai", BaseURL: "https://gateway.example.com/v1"}
	cfg.Agents = map[string]config.Agent{"reviewer": {Description: "Review carefully", Instruction: "Inspect \"all\" changes", Model: "gpt-5.2-codex"}}
	cfg.MCPServers = map[string]config.MCPServer{"tools": {
		URL: "https://mcp.example.com/mcp", Headers: map[string]string{"X-Tenant": "test", "Authorization": "${KAGENT_CODEX_MCP_CREDENTIAL_ABC}"}, EnabledTools: []string{"read"},
	}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := New(context.Background(), Input{ConfigJSON: raw, Workspace: filepath.Join(durable, "workspace"), DurableDir: durable, Environment: []string{"PATH=/bin", "CODEX_HOME=/wrong"}})
	if err != nil {
		t.Fatal(err)
	}
	if runner == nil {
		t.Fatal("New() returned nil")
	}
	codexHome := filepath.Join(durable, "codex")
	configPath := filepath.Join(codexHome, "config.toml")
	agentPath := filepath.Join(codexHome, "agents", "reviewer.toml")
	for _, path := range []string{configPath, agentPath} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s permissions = %v, %v", path, info, err)
		}
	}
	configContents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var native nativeConfig
	if err := toml.Unmarshal(configContents, &native); err != nil {
		t.Fatalf("decode generated Codex configuration: %v", err)
	}
	provider := native.ModelProviders["kagent-openai"]
	server := native.MCPServers["tools"]
	agent := native.Agents["reviewer"]
	if native.ModelProvider != "kagent-openai" || provider.WireAPI != "responses" || provider.EnvKey != "OPENAI_API_KEY" || provider.BaseURL != cfg.Provider.BaseURL {
		t.Fatalf("generated provider configuration = %#v, provider name = %q", provider, native.ModelProvider)
	}
	if server.EnvHTTPHeaders["Authorization"] != "KAGENT_CODEX_MCP_CREDENTIAL_ABC" || server.HTTPHeaders["X-Tenant"] != "test" || len(server.EnabledTools) != 1 || server.EnabledTools[0] != "read" {
		t.Fatalf("generated MCP server configuration = %#v", server)
	}
	if agent.Description != "Review carefully" || agent.ConfigFile != agentPath {
		t.Fatalf("generated agent registration = %#v", agent)
	}
	agentContents, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	var agentConfig nativeAgentConfig
	if err := toml.Unmarshal(agentContents, &agentConfig); err != nil {
		t.Fatalf("decode generated Codex agent configuration: %v", err)
	}
	if agentConfig.Model != "gpt-5.2-codex" || agentConfig.DeveloperInstructions != `Inspect "all" changes` {
		t.Fatalf("generated Codex agent configuration = %#v", agentConfig)
	}
}

func TestNewRejectsSymlinkedCodexHome(t *testing.T) {
	durable := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(durable, "codex")); err != nil {
		t.Fatal(err)
	}
	cfg := config.Production("model", "instruction")
	cfg.Provider = config.Provider{Name: "openai"}
	raw, _ := json.Marshal(cfg)
	if _, err := New(context.Background(), Input{ConfigJSON: raw, Workspace: filepath.Join(durable, "workspace"), DurableDir: durable}); err == nil {
		t.Fatal("New() accepted symlinked Codex home")
	}
}

func TestPinnedCodexAcceptsGeneratedConfiguration(t *testing.T) {
	executable, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("pinned Codex CLI is not installed")
	}
	tests := []struct {
		name     string
		provider config.Provider
	}{
		{name: "OpenAI", provider: config.Provider{Name: "openai"}},
		{name: "OpenAI gateway", provider: config.Provider{Name: "openai", BaseURL: "https://gateway.example.com/v1"}},
		{name: "Bedrock", provider: config.Provider{Name: "amazon-bedrock"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertPinnedCodexAcceptsConfig(t, executable, test.provider)
		})
	}
}

func assertPinnedCodexAcceptsConfig(t *testing.T, executable string, provider config.Provider) {
	t.Helper()
	durable := filepath.Join(t.TempDir(), "data")
	cfg := config.Production("gpt-5.2-codex", "work carefully")
	cfg.Provider = provider
	cfg.Agents = map[string]config.Agent{"reviewer": {Description: "Reviews", Instruction: "Review", Model: "gpt-5.2-codex"}}
	cfg.MCPServers = map[string]config.MCPServer{"tools": {URL: "https://mcp.example.com/mcp", EnabledTools: []string{"read"}}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(context.Background(), Input{ConfigJSON: raw, Workspace: filepath.Join(durable, "workspace"), DurableDir: durable, Environment: os.Environ()}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "app-server", "--strict-config", "--stdio")
	command.Env = append(os.Environ(), "CODEX_HOME="+filepath.Join(durable, "codex"), "OPENAI_API_KEY=test", "AWS_REGION=us-east-1", "AWS_BEARER_TOKEN_BEDROCK=test")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"kagent-test","version":"1"}}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("initialize generated Codex config: %v: %s", err, stderr.String())
	}
	var response struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != 1 || len(response.Error) != 0 || len(response.Result) == 0 {
		t.Fatalf("initialize response = %s, stderr = %s", line, stderr.String())
	}
	_ = stdin.Close()
	_ = command.Process.Kill()
	_ = command.Wait()
}
