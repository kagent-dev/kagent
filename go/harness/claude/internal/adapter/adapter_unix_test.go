//go:build unix

package adapter

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
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
if mv "$1" "$1.moved" 2>/dev/null; then echo "renamed the harness state"; exit 1; fi
chmod 000 .`, harnessState)
	if groups := strings.Fields(output); len(groups) != 1 || groups[0] != "65532" {
		t.Fatalf("Claude runs with groups %v, want only its own", groups)
	}

	start()
	asClaude(t, input.Workspace, `set -e
test -r . -a -w . -a -x . || { echo "the workspace Claude locked is not reopened"; exit 1; }
chmod 700 repo
test -O repo/.git/HEAD || { echo "a file Claude wrote is no longer Claude's"; exit 1; }`)
}

// A start touches only the roots of Claude's trees, so nothing Claude makes
// under them, however it links, marks or nests it, changes hands or stops the
// next start.
func TestNewLeavesWhatClaudeMadeOfItsTreesAlone(t *testing.T) {
	input := rootInput(t, []byte(`{"version":5,"claude_executable":"claude","expected_claude_version":"2.1.260","strict_version":true,"max_event_bytes":100,"max_stderr_bytes":100,"interrupt_grace_millis":100}`))
	_, err := New(t.Context(), input)
	require.NoError(t, err)
	claudeDir := filepath.Join(input.DurableDir, "claude")
	for _, tree := range []string{input.Workspace, claudeDir} {
		asClaude(t, tree, `set -e
echo a > linked && chmod 600 linked && ln linked link
cp /bin/true setuid && chmod 4755 setuid && ln setuid pinned
touch setgid && chmod 2644 setgid
mkdir shared && chmod 2775 shared
perl -e 'for (1..600) { mkdir "deep" or die $!; chdir "deep" or die $! }'`)
	}

	_, err = New(t.Context(), input)
	require.NoError(t, err)
	for _, tree := range []string{input.Workspace, claudeDir} {
		asClaude(t, tree, `set -e
for name in linked setuid setgid shared; do test -O "$name" || { echo "$name is no longer Claude's"; exit 1; }; done
cat link > /dev/null && : >> link
test -u setuid -a -g setgid -a -g shared`)
	}
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

	policy := []string{input.ManagedSettingsPath, mcpConfig}
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

// A start trusts nothing Claude could write before it: a link or a package it
// planted in the generated tree, or one an earlier image let it plant there,
// neither redirects root's writes nor feeds the skills Claude gets.
func TestNewRebuildsTheGeneratedTreeClaudeCouldWrite(t *testing.T) {
	repo, commit := skillRepository(t, "# Review")
	cfg := config.Production("claude-test", "help")
	cfg.StrictVersion = false
	cfg.SkillResources = &agentplugin.Resources{Skills: []agentplugin.Skill{{
		Name: "review", Source: agentplugin.Source{Git: &agentplugin.GitSource{URL: "file://" + repo, Commit: commit}},
	}}}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	input := rootInput(t, raw)
	generated := filepath.Join(input.DurableDir, "generated")
	for _, dir := range []string{input.Workspace, filepath.Join(input.DurableDir, "claude", "packages", "standalone-0"), filepath.Join(generated, "claude")} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	for _, tree := range []string{input.Workspace, filepath.Join(input.DurableDir, "claude"), generated} {
		chownTree(t, tree)
	}
	require.NoError(t, os.Chmod(input.DurableDir, 0o755))
	outside := filepath.Join(filepath.Dir(input.DurableDir), "outside")
	require.NoError(t, os.MkdirAll(filepath.Join(outside, "standalone-0"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "standalone-0", "SKILL.md"), []byte("root only"), 0o600))
	asClaude(t, input.Workspace, `set -e
echo planted > "$1/claude/packages/standalone-0/SKILL.md"
ln -s "$2" "$1/generated/claude/.claude"
ln -s "$2" "$1/generated/packages"`, input.DurableDir, outside)

	runner, err := New(t.Context(), input)
	require.NoError(t, err)
	t.Cleanup(func() { _ = runner.Close() })

	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Len(t, entries, 1, "root wrote outside the generated tree")
	skill := filepath.Join(generated, "claude", ".claude", "skills", "review", "SKILL.md")
	contents, err := os.ReadFile(skill)
	require.NoError(t, err)
	require.Equal(t, "# Review", string(contents))
	info, err := os.Lstat(skill)
	require.NoError(t, err)
	stat := info.Sys().(*syscall.Stat_t)
	require.Equal(t, [3]uint32{0, config.UnprivilegedGID, 0o640}, [3]uint32{stat.Uid, stat.Gid, uint32(info.Mode().Perm())})
	asClaude(t, input.Workspace, `set -e
cat "$1" > /dev/null
if (: >> "$1") 2>/dev/null; then echo "wrote a skill"; exit 1; fi
if touch "$(dirname "$1")/planted" 2>/dev/null; then echo "added a skill file"; exit 1; fi`, skill)
}

// A volume an earlier image ran as the unprivileged user leaves the harness's
// own state Claude's; the first root start takes it back and drops the links.
func TestNewSecuresTheHarnessStateOfAnUpgradedVolume(t *testing.T) {
	input := rootInput(t, []byte(`{"version":5,"claude_executable":"claude","expected_claude_version":"2.1.260","strict_version":true,"max_event_bytes":100,"max_stderr_bytes":100,"interrupt_grace_millis":100}`))
	state := filepath.Join(input.DurableDir, "adapter")
	require.NoError(t, os.MkdirAll(state, 0o777))
	require.NoError(t, os.WriteFile(filepath.Join(state, "state.json"), []byte("{}"), 0o666))
	require.NoError(t, os.Symlink("/etc/passwd", filepath.Join(state, "link")))
	require.NoError(t, os.Chmod(state, 0o777))
	chownTree(t, input.DurableDir)

	_, err := New(t.Context(), input)
	require.NoError(t, err)

	require.NoFileExists(t, filepath.Join(state, "link"))
	require.Zero(t, owner(t, state))
	require.Zero(t, owner(t, filepath.Join(state, "state.json")))
	asClaude(t, input.Workspace, `if (: >> "$1/state.json") 2>/dev/null; then echo "wrote the harness state"; exit 1; fi
if touch "$1/planted" 2>/dev/null; then echo "added to the harness state"; exit 1; fi`, state)
}

// skillRepository is a local git repository holding one skill, fetchable by
// commit.
func skillRepository(t *testing.T, skill string) (string, string) {
	t.Helper()
	repo := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=test", "-c", "user.email=test@example.com"}, args...)...)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, output)
		return strings.TrimSpace(string(output))
	}
	run("init", "-q")
	run("config", "uploadpack.allowAnySHA1InWant", "true")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "SKILL.md"), []byte(skill), 0o644))
	run("add", "SKILL.md")
	run("commit", "-q", "-m", "skill")
	return repo, run("rev-parse", "HEAD")
}

// rootInput is the adapter input of a root harness under a fresh directory the
// unprivileged user can traverse. It skips the test unless it runs as root.
func rootInput(t *testing.T, configJSON []byte) Input {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("the harness splits Claude off only when it runs as root")
	}
	base := t.TempDir()
	durableDir := filepath.Join(base, "data")
	// Root cannot read what Claude locked; Claude empties its own trees.
	t.Cleanup(func() {
		claude := exec.Command("/bin/sh", "-c", `chmod -R u+rwx "$@"; find "$@" -mindepth 1 -delete`, "sh",
			filepath.Join(durableDir, "workspace"), filepath.Join(durableDir, "claude"))
		utils.RunAs(claude, config.UnprivilegedUID, config.UnprivilegedGID)
		_ = claude.Run()
	})
	for _, dir := range []string{filepath.Dir(base), base} {
		if err := os.Chmod(dir, 0o711); err != nil {
			t.Fatal(err)
		}
	}
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

// chownTree hands a tree the test built as root to the unprivileged user, as
// an earlier image running as that user would have left it.
func chownTree(t *testing.T, root string) {
	t.Helper()
	require.NoError(t, filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(path, config.UnprivilegedUID, config.UnprivilegedGID)
	}))
}

func owner(t *testing.T, path string) int {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return int(info.Sys().(*syscall.Stat_t).Uid)
}
