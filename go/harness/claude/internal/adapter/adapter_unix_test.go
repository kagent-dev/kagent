//go:build unix

package adapter

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/harness/claude/config"
	"github.com/kagent-dev/kagent/go/harness/internal/utils"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

// Run as root with only CHOWN, SETGID and SETUID to prove the capability set
// the claude translator asks for.
func TestNewHandsClaudesTreesToTheUnprivilegedUserOnEveryStart(t *testing.T) {
	input := rootInput(t, []byte(`{"version":5,"claude_executable":"claude","expected_claude_version":"2.1.260","strict_version":true,"max_event_bytes":100,"max_stderr_bytes":100,"interrupt_grace_millis":100}`))
	durableDir := input.DurableDir
	start := func() {
		t.Helper()
		if _, err := New(t.Context(), input); err != nil {
			t.Fatalf("New() = %v", err)
		}
		for _, tree := range []string{input.Workspace, filepath.Join(durableDir, "claude")} {
			if uid := owner(t, tree); uid != config.UnprivilegedUID {
				t.Errorf("%s belongs to %d, want the unprivileged user", tree, uid)
			}
		}
		info, err := os.Lstat(durableDir)
		if err != nil {
			t.Fatal(err)
		}
		if owner(t, durableDir) != 0 || info.Mode().Perm()&0o011 != 0o011 {
			t.Errorf("the durable directory is the harness's and searchable, got uid %d mode %o", owner(t, durableDir), info.Mode().Perm())
		}
	}
	start()
	// Without approvals the managed settings still hold: no user, project or
	// workspace permission rule applies.
	managed, err := os.ReadFile(input.ManagedSettingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(managed) != `{"permissions":{},"allowManagedPermissionRulesOnly":true}` {
		t.Errorf("managed settings = %s", managed)
	}
	harnessState := filepath.Join(durableDir, "adapter")
	if err := utils.EnsurePrivateDir(harnessState); err != nil {
		t.Fatal(err)
	}

	output := asClaude(t, input.Workspace, `set -e
id -G
mkdir -p repo/.git && echo ref > repo/.git/HEAD && chmod 600 repo/.git/HEAD && chmod 000 repo
if mv "$1" "$1.moved" 2>/dev/null; then echo "renamed the harness state"; exit 1; fi`, harnessState)
	if groups := strings.Fields(output); len(groups) != 1 || groups[0] != "65532" {
		t.Fatalf("Claude runs with groups %v, want only its own", groups)
	}

	start()
	asClaude(t, input.Workspace, `set -e
test -O repo/.git/HEAD || { echo "a file Claude wrote is no longer Claude's"; exit 1; }
test -r repo -a -w repo -a -x repo || { echo "a directory Claude locked is not reopened"; exit 1; }`)
}

func TestNewFrontsMCPServersWhenTheCallerTokenPropagates(t *testing.T) {
	cfg := config.Production("claude-test", "help")
	cfg.StrictVersion = false
	cfg.MCPServers = map[string]config.MCPServer{
		"muster":    {Type: "http", URL: "https://muster.example.com/mcp", Headers: map[string]string{"X-Muster-Toolset": "preset:read-only"}, RequireApproval: true},
		"knowledge": {Type: "http", URL: "https://mcp.example.com/read", Headers: map[string]string{"Authorization": "Bearer ${KAGENT_CLAUDE_MCP_CREDENTIAL_ABC}"}},
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	input := rootInput(t, raw)
	input.Environment = []string{"PATH=/bin", "KAGENT_PROPAGATE_TOKEN=true"}
	runner, err := New(t.Context(), input)
	require.NoError(t, err)
	t.Cleanup(func() { _ = runner.Close() })
	contents, err := os.ReadFile(filepath.Join(input.PolicyDir, "mcp.json"))
	require.NoError(t, err)
	var written struct {
		Servers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(contents, &written))
	for _, name := range []string{"muster", "knowledge", "kagent_hitl"} {
		server, ok := written.Servers[name]
		require.True(t, ok, "mcp.json lacks %q: %s", name, contents)
		require.Equal(t, "http", server.Type, name)
		require.True(t, strings.HasPrefix(server.URL, "http://127.0.0.1:"), "%s URL = %q, want a loopback endpoint", name, server.URL)
		require.Len(t, server.Headers, 1, "%s headers = %v, want only the loopback token", name, server.Headers)
		require.True(t, strings.HasPrefix(server.Headers["Authorization"], "Bearer "), name)
	}
	for _, leaked := range []string{"muster.example.com", "mcp.example.com", "X-Muster-Toolset", "KAGENT_CLAUDE_MCP_CREDENTIAL_ABC"} {
		require.NotContains(t, string(contents), leaked, "mcp.json carries upstream detail")
	}
	require.Contains(t, strings.Join(runner.Args(runtime.Turn{Prompt: "test"}), "\n"), "--permission-prompt-tool\nmcp__kagent_hitl__approve\n",
		"the approval bridge is lost behind the forwarder")
}

// The rules and configuration Claude Code obeys are the harness's: Claude can
// read them and can neither edit, replace nor add to them.
func TestNewKeepsClaudesPolicyOutOfItsReach(t *testing.T) {
	cfg := config.Production("claude-test", "help")
	cfg.StrictVersion = false
	cfg.MCPServers = map[string]config.MCPServer{
		"muster": {Type: "http", URL: "https://muster.example.com/mcp", RequireApproval: true},
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	input := rootInput(t, raw)
	input.Environment = append(input.Environment, config.GoogleCredentialsJSONEnvName+`={"type":"service_account"}`)
	runner, err := New(t.Context(), input)
	require.NoError(t, err)
	t.Cleanup(func() { _ = runner.Close() })

	contents, err := os.ReadFile(input.ManagedSettingsPath)
	require.NoError(t, err)
	require.JSONEq(t, `{"permissions":{"ask":["mcp__muster__*"]},"allowManagedPermissionRulesOnly":true}`, string(contents))
	args := runner.Args(runtime.Turn{Prompt: "test"})
	require.NotContains(t, args, "--settings", "the managed settings carry every permission rule")
	mcpConfig := filepath.Join(input.PolicyDir, "mcp.json")
	require.Subset(t, args, []string{"--mcp-config", mcpConfig})

	policy := []string{input.ManagedSettingsPath, mcpConfig, filepath.Join(input.PolicyDir, "google-credentials.json")}
	for _, path := range policy {
		info, err := os.Lstat(path)
		require.NoError(t, err)
		stat := info.Sys().(*syscall.Stat_t)
		require.Equal(t, [3]uint32{0, config.UnprivilegedGID, 0o640}, [3]uint32{stat.Uid, stat.Gid, uint32(info.Mode().Perm())}, path)
	}
	asClaude(t, input.Workspace, `set -e
for f in "$@"; do
	cat "$f" > /dev/null
	if (: >> "$f") 2>/dev/null; then echo "wrote $f"; exit 1; fi
	if mv "$f" "$f.moved" 2>/dev/null; then echo "replaced $f"; exit 1; fi
	if touch "$(dirname "$f")/planted" 2>/dev/null; then echo "added beside $f"; exit 1; fi
done`, policy...)
}

// rootInput is the adapter input of a root harness under a fresh directory the
// unprivileged user can traverse. It skips the test unless it runs as root.
func rootInput(t *testing.T, configJSON []byte) Input {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("the harness splits Claude off only when it runs as root")
	}
	base := t.TempDir()
	t.Cleanup(func() { _ = utils.ReclaimTree(base) })
	for _, dir := range []string{filepath.Dir(base), base} {
		if err := os.Chmod(dir, 0o711); err != nil {
			t.Fatal(err)
		}
	}
	durableDir := filepath.Join(base, "data")
	return Input{
		ConfigJSON: configJSON, Workspace: filepath.Join(durableDir, "workspace"), DurableDir: durableDir,
		EphemeralDir: filepath.Join(base, "ephemeral"), Environment: []string{"PATH=/bin:/usr/bin"},
		PolicyDir:           filepath.Join(base, "policy"),
		ManagedSettingsPath: filepath.Join(base, "etc", "claude-code", "managed-settings.json"),
	}
}

func asClaude(t *testing.T, dir, script string, args ...string) string {
	t.Helper()
	claude := exec.Command("/bin/sh", append([]string{"-c", script, "sh"}, args...)...)
	claude.Dir = dir
	utils.RunAs(claude, config.UnprivilegedUID, config.UnprivilegedGID)
	output, err := claude.CombinedOutput()
	if err != nil {
		t.Fatalf("Claude's turn: %v: %s", err, output)
	}
	return string(output)
}

func owner(t *testing.T, path string) int {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return int(info.Sys().(*syscall.Stat_t).Uid)
}
