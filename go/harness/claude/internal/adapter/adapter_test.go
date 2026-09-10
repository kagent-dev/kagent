package adapter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
	"github.com/kagent-dev/kagent/go/harness/claude/config"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

func TestNewMaterializesDurableDirectories(t *testing.T) {
	durableDir := filepath.Join(t.TempDir(), "data")
	ephemeralDir := filepath.Join(t.TempDir(), "credentials")
	workspace := filepath.Join(durableDir, "workspace")
	runner, err := New(context.Background(), Input{
		ConfigJSON: []byte(`{"version":4,"claude_executable":"claude","expected_claude_version":"2.1.260","strict_version":true,"max_event_bytes":100,"max_stderr_bytes":100,"interrupt_grace_millis":100}`),
		Workspace:  workspace,
		DurableDir: durableDir, EphemeralDir: ephemeralDir,
		Environment: []string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/wrong", "DISABLE_UPDATES=0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runner == nil {
		t.Fatal("New() returned a nil runner")
	}
	for _, path := range []string{workspace, filepath.Join(durableDir, "claude")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("%s permissions = %o, want 700", path, info.Mode().Perm())
		}
	}
}

func TestNewMaterializesSkillsAndMCPConfig(t *testing.T) {
	durableDir := filepath.Join(t.TempDir(), "data")
	claudeDir := filepath.Join(durableDir, "claude")
	skillRoot := filepath.Join(durableDir, "generated", "claude")
	packageRoot := filepath.Join(claudeDir, "packages", "standalone-0")
	if err := os.MkdirAll(packageRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "SKILL.md"), []byte("# Review"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Production("claude-test", "help")
	cfg.StrictVersion = false
	cfg.SkillResources = &agentplugin.Resources{Skills: []agentplugin.Skill{{
		Name: "review", Source: agentplugin.Source{Git: &agentplugin.GitSource{URL: "unused", Commit: strings.Repeat("a", 40)}},
	}}}
	cfg.MCPServers = map[string]config.MCPServer{"tools": {Type: "http", URL: "https://mcp.example.com/mcp"}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ephemeralDir := filepath.Join(t.TempDir(), "generated")
	runner, err := New(context.Background(), Input{
		ConfigJSON: raw, Workspace: filepath.Join(durableDir, "workspace"), DurableDir: durableDir,
		EphemeralDir: ephemeralDir, Environment: []string{"PATH=/bin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		filepath.Join(skillRoot, ".claude", "skills", "review", "SKILL.md"): "# Review",
		filepath.Join(ephemeralDir, "mcp.json"):                             `{"mcpServers":{"tools":{"type":"http","url":"https://mcp.example.com/mcp"}}}`,
	} {
		contents, err := os.ReadFile(path)
		if err != nil || string(contents) != want {
			t.Fatalf("%s = %q, %v; want %q", path, contents, err, want)
		}
	}
	if args := strings.Join(runner.Args(runtime.Turn{Prompt: "test"}), "\n"); !strings.Contains(args, "--add-dir\n"+skillRoot) {
		t.Fatalf("arguments do not expose materialized skills to bare mode: %s", args)
	}
}

func TestNewMaterializesApprovalSettings(t *testing.T) {
	durableDir := filepath.Join(t.TempDir(), "data")
	ephemeralDir := filepath.Join(t.TempDir(), "generated")
	cfg := config.Production("claude-test", "help")
	cfg.StrictVersion = false
	cfg.MCPServers = map[string]config.MCPServer{
		"protected": {Type: "http", URL: "https://mcp.example.com/write", RequireApproval: true},
		"readonly":  {Type: "http", URL: "https://mcp.example.com/read"},
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := New(context.Background(), Input{
		ConfigJSON: raw, Workspace: filepath.Join(durableDir, "workspace"), DurableDir: durableDir,
		EphemeralDir: ephemeralDir, Environment: []string{"PATH=/bin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	settings, err := os.ReadFile(filepath.Join(ephemeralDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(settings), `{"permissions":{"ask":["mcp__protected__*"]}}`; got != want {
		t.Fatalf("approval settings = %s, want %s", got, want)
	}
	mcpConfig, err := os.ReadFile(filepath.Join(ephemeralDir, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var materialized struct {
		Servers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(mcpConfig, &materialized); err != nil {
		t.Fatal(err)
	}
	approvalServer := materialized.Servers[approvalMCPServerName]
	if approvalServer.Type != "http" || !strings.HasPrefix(approvalServer.URL, "http://127.0.0.1:") ||
		!strings.HasPrefix(approvalServer.Headers["Authorization"], "Bearer ") {
		t.Fatalf("private approval MCP server = %#v", approvalServer)
	}
	if len(materialized.Servers) != 3 {
		t.Fatalf("materialized MCP servers = %#v", materialized.Servers)
	}
	args := strings.Join(runner.Args(runtime.Turn{Prompt: "test"}), "\n")
	for _, required := range []string{
		"--bare",
		"--setting-sources\n\n",
		"--settings\n" + filepath.Join(ephemeralDir, "settings.json"),
		"--dangerously-skip-permissions",
		"--permission-prompt-tool\nmcp__" + approvalMCPServerName + "__approve",
	} {
		if !strings.Contains(args, required) {
			t.Fatalf("approval arguments do not contain %q: %s", required, args)
		}
	}
	for _, forbidden := range []string{"--permission-mode", "dontAsk"} {
		if strings.Contains(args, forbidden) {
			t.Fatalf("approval arguments contain %q: %s", forbidden, args)
		}
	}
}

func TestNewRejectsInvalidInput(t *testing.T) {
	input := Input{ConfigJSON: []byte(`{}`), Workspace: "relative", DurableDir: "relative", EphemeralDir: "relative"}
	if _, err := New(context.Background(), input); err == nil {
		t.Fatal("New() accepted invalid input")
	}
}

func TestMaterializeGoogleCredentials(t *testing.T) {
	dir := t.TempDir()
	raw := `{"type":"service_account","project_id":"test"}`
	environment, err := materializeGoogleCredentials([]string{"A=1", config.GoogleCredentialsJSONEnvName + "=" + raw}, dir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "google-credentials.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != raw {
		t.Fatalf("credentials = %q", contents)
	}
	if len(environment) != 2 || environment[0] != "A=1" || environment[1] != config.GoogleApplicationCredentialsEnvName+"="+path {
		t.Fatalf("environment = %v", environment)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credential permissions = %v, %v", info, err)
	}
}

func TestSetEnvironmentOverridesExistingValue(t *testing.T) {
	got := setEnvironment([]string{"A=1", "A=2", "B=3"}, "A", "4")
	want := []string{"B=3", "A=4"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("setEnvironment() = %v, want %v", got, want)
	}
}
