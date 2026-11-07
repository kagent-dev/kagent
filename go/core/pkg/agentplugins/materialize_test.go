package agentplugins

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
	"github.com/kagent-dev/kagent/go/internal/testutil"
)

func TestMaterializeGitPlugin(t *testing.T) {
	repository, commit := testutil.CommitFiles(t, map[string]string{
		"plugin.json":            `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"acme.test"}`,
		"mcp.json":               `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"local":{"type":"stdio","command":"server"}}}`,
		"skills/review/SKILL.md": "# Review",
	})

	root := t.TempDir()
	materialization, err := Materialize(t.Context(), agentplugin.Resources{Plugins: []agentplugin.Bundle{{
		Source: agentplugin.Source{Git: &agentplugin.GitSource{URL: repository, Commit: commit}}, Skills: []string{"review"},
	}}}, Paths{
		Packages: filepath.Join(root, "packages"),
		Skills:   filepath.Join(root, "skills"),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := LoadMCP(t.Context(), materialization, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Stdio) != 1 || result.Stdio[0].Command != "server" {
		t.Fatalf("materialized plugin = %#v", result)
	}
	if content, err := os.ReadFile(filepath.Join(root, "skills", "review", "SKILL.md")); err != nil || string(content) != "# Review" {
		t.Fatalf("materialized skill = %q, %v", content, err)
	}
}

func TestFetchSourceReusesExistingMaterialization(t *testing.T) {
	destination := t.TempDir()
	if err := os.Mkdir(filepath.Join(destination, "selected"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "selected/SKILL.md"), []byte("# Existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(destination, "unrelated-link")); err != nil {
		t.Fatal(err)
	}

	source := agentplugin.Source{Git: &agentplugin.GitSource{URL: "does-not-exist", Commit: strings.Repeat("a", 40)}, Path: "selected"}
	root, err := fetchSource(t.Context(), source, destination, "SKILL.md")
	if err != nil {
		t.Fatalf("fetchSource() redownloaded existing materialization: %v", err)
	}
	if want := canonicalPath(filepath.Join(destination, "selected")); root != want {
		t.Fatalf("fetchSource() root = %q, want %q", root, want)
	}
}

func TestMaterializeReplacesLegacyCache(t *testing.T) {
	repository, commit := testutil.CommitFiles(t, map[string]string{"SKILL.md": "# Current"})
	source := agentplugin.Source{Git: &agentplugin.GitSource{URL: repository, Commit: commit}}
	for _, scenario := range []struct {
		name   string
		record string
	}{
		{name: "Given an unrecorded cache, when materializing a skill, assert that the pinned content replaces it"},
		{name: "Given a recorded cache, when materializing a skill, assert that the pinned content replaces it", record: `{"git":{"url":"previous","commit":"previous"}}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			paths := Paths{Packages: filepath.Join(t.TempDir(), "packages"), Skills: t.TempDir()}
			legacy := filepath.Join(paths.Packages, "standalone-0")
			require.NoError(t, os.MkdirAll(legacy, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(legacy, "SKILL.md"), []byte("# Previous"), 0o644))
			if scenario.record != "" {
				require.NoError(t, os.WriteFile(legacy+".source", []byte(scenario.record), 0o644))
			}

			_, err := Materialize(t.Context(), agentplugin.Resources{Skills: []agentplugin.Skill{{Name: "review", Source: source}}}, paths)

			require.NoError(t, err)
			content, err := os.ReadFile(filepath.Join(paths.Skills, "review", "SKILL.md"))
			require.NoError(t, err)
			require.Equal(t, "# Current", string(content))
			require.NoDirExists(t, legacy)
			require.NoFileExists(t, legacy+".source")
		})
	}
}

func TestMaterializeRefetchesAPackageOfAnotherSource(t *testing.T) {
	repository, first := testutil.CommitFiles(t, map[string]string{
		"plugin.json":            `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"acme.test"}`,
		"skills/review/SKILL.md": "# First",
	})
	if err := os.WriteFile(filepath.Join(repository, "skills", "review", "SKILL.md"), []byte("# Second"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.CommandContext(t.Context(), "git", "-C", repository, "commit", "-qam", "second").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	head, err := exec.CommandContext(t.Context(), "git", "-C", repository, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	pinned := func(commit string) agentplugin.Resources {
		return agentplugin.Resources{Plugins: []agentplugin.Bundle{{
			Source: agentplugin.Source{Git: &agentplugin.GitSource{URL: repository, Commit: commit}}, Skills: []string{"review"},
		}}}
	}
	root := t.TempDir()
	paths := Paths{Packages: filepath.Join(root, "packages"), Skills: filepath.Join(root, "skills")}
	if _, err := Materialize(t.Context(), pinned(first), paths); err != nil {
		t.Fatal(err)
	}

	if _, err := Materialize(t.Context(), pinned(strings.TrimSpace(string(head))), paths); err != nil {
		t.Fatal(err)
	}

	if content, err := os.ReadFile(filepath.Join(paths.Skills, "review", "SKILL.md")); err != nil || string(content) != "# Second" {
		t.Fatalf("skill after the pin moved = %q, %v; want the new commit's", content, err)
	}
	entries, err := os.ReadDir(paths.Packages)
	require.NoError(t, err)
	require.Len(t, entries, 1, "unused pins must not accumulate in the durable cache")
}

func TestFetchSourceDoesNotReuseIncompleteMaterialization(t *testing.T) {
	destination := t.TempDir()

	_, err := fetchSource(t.Context(), agentplugin.Source{Git: &agentplugin.GitSource{
		URL: "does-not-exist", Commit: strings.Repeat("a", 40),
	}}, destination, "SKILL.md")
	if err == nil {
		t.Fatal("fetchSource() reused incomplete materialization")
	}
}

func TestMaterializeCopiesSelectionsWithoutLoadingPluginMCP(t *testing.T) {
	root := t.TempDir()
	repository, commit := testutil.CommitFiles(t, map[string]string{
		"plugin.json":            `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"acme.test"}`,
		"mcp.json":               `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"local":{"type":"stdio","command":"server"}}}`,
		"skills/review/SKILL.md": "# Review",
	})
	paths := Paths{
		Packages: filepath.Join(root, "plugins"),
		Skills:   filepath.Join(root, "skills"),
	}
	resources := agentplugin.Resources{Plugins: []agentplugin.Bundle{{
		Source: agentplugin.Source{Git: &agentplugin.GitSource{URL: repository, Commit: commit}},
		Skills: []string{"review"},
	}}}
	if _, err := Materialize(t.Context(), resources, paths); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(paths.Skills, "review", "SKILL.md"))
	if err != nil || string(content) != "# Review" {
		t.Fatalf("materialized skill = %q, %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(root, "data")); !os.IsNotExist(err) {
		t.Fatalf("plugin MCP data directory was created: %v", err)
	}
}

func TestLoadManifestUsesAgentPluginsV1Schema(t *testing.T) {
	root := t.TempDir()
	raw := `{
		"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
		"name":"acme.tools",
		"unknown":"ignored",
		"extensions":"ignored"
	}`
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, claudeFormat, err := loadManifest(root)
	if err != nil || claudeFormat || manifest.Name != "acme.tools" {
		t.Fatalf("loadManifest() = %#v, %v, %v", manifest, claudeFormat, err)
	}
}

func TestLoadManifestFormats(t *testing.T) {
	const agentPlugins = `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"acme.tools"}`
	const claude = `{"name":"acme-claude"}`
	tests := []struct {
		name             string
		files            map[string]string
		wantName         string
		wantClaudeFormat bool
		wantErr          bool
	}{
		{name: "agent plugins manifest", files: map[string]string{"plugin.json": agentPlugins}, wantName: "acme.tools"},
		{name: "claude manifest", files: map[string]string{".claude-plugin/plugin.json": claude}, wantName: "acme-claude", wantClaudeFormat: true},
		{name: "both manifests use the root one", files: map[string]string{"plugin.json": agentPlugins, ".claude-plugin/plugin.json": claude}, wantName: "acme.tools"},
		{name: "no manifest", files: map[string]string{}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			for name, content := range tt.files {
				path := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			manifest, claudeFormat, err := loadManifest(root)
			if (err != nil) != tt.wantErr || manifest.Name != tt.wantName || claudeFormat != tt.wantClaudeFormat {
				t.Fatalf("loadManifest() = %#v, %v, %v", manifest, claudeFormat, err)
			}
		})
	}
}

func TestMaterializeClaudeFormatPlugin(t *testing.T) {
	repository, commit := testutil.CommitFiles(t, map[string]string{
		".claude-plugin/plugin.json": `{"name":"acme-native","version":"1.0.0"}`,
		"skills/review/SKILL.md":     "# Review",
	})
	resources := agentplugin.Resources{Plugins: []agentplugin.Bundle{{
		Source: agentplugin.Source{Git: &agentplugin.GitSource{URL: repository, Commit: commit}}, Skills: []string{"review"},
	}}}

	root := t.TempDir()
	paths := Paths{Packages: filepath.Join(root, "packages"), Skills: filepath.Join(root, "skills")}
	materialization, err := Materialize(t.Context(), resources, paths)
	if err != nil {
		t.Fatal(err)
	}
	roots := materialization.ClaudeFormatPluginRoots()
	if len(roots) != 1 {
		t.Fatalf("ClaudeFormatPluginRoots() = %v, want 1", roots)
	}
	if _, err := os.Stat(filepath.Join(roots[0], ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("plugin root %s: %v", roots[0], err)
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "review")); !os.IsNotExist(err) {
		t.Fatalf("skill copied from a Claude-format plugin: %v", err)
	}
	if mcp, err := LoadMCP(t.Context(), materialization, filepath.Join(root, "data")); err != nil || len(mcp.Stdio)+len(mcp.SSE)+len(mcp.StreamableHTTP) != 0 {
		t.Fatalf("LoadMCP() = %#v, %v", mcp, err)
	}
}

func TestParseMCPServerSupportsLocalAndRemoteTransports(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	command := filepath.Join(root, "bin", "server")
	if err := os.MkdirAll(filepath.Dir(command), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(command, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	stdio, err := parseMCPServer(json.RawMessage(`{
		"type":"stdio","command":"./bin/server",
		"args":["--root=${PLUGIN_ROOT}"],
		"env":{"STATE":"${PLUGIN_DATA}/state"},
		"cwd":"${PLUGIN_DATA}/work"
	}`), root, data)
	if err != nil {
		t.Fatal(err)
	}
	if stdio.Command != canonicalPath(command) || stdio.Args[0] != "--root="+root || stdio.Env["STATE"] != filepath.Join(data, "state") || stdio.CWD != filepath.Join(data, "work") {
		t.Fatalf("stdio server = %#v", stdio)
	}

	for _, transport := range []string{"streamable-http", "sse"} {
		server, err := parseMCPServer(json.RawMessage(`{"type":"`+transport+`","url":"https://mcp.example.com","headers":{"X-Tenant":"public"}}`), root, data)
		if err != nil || server.Type != transport {
			t.Fatalf("parseMCPServer(%s) = %#v, %v", transport, server, err)
		}
	}
}

func TestParseMCPServerRejectsEscapingCommand(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	_, err := parseMCPServer(json.RawMessage(`{"type":"stdio","command":"../server"}`), root, data)
	if err == nil {
		t.Fatal("parseMCPServer() accepted an escaping command")
	}
}

func TestValidatePackageRejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := validatePackage(root); err == nil {
		t.Fatal("validatePackage() accepted an escaping symlink")
	}
}

func TestCopySkillPreservesContainedSymlink(t *testing.T) {
	source, destination := t.TempDir(), filepath.Join(t.TempDir(), "skill")
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("# skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("SKILL.md", filepath.Join(source, "README.md")); err != nil {
		t.Fatal(err)
	}
	if err := validatePackage(source); err != nil {
		t.Fatal(err)
	}
	if err := copySkill(source, destination); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(destination, "README.md"))
	if err != nil || string(content) != "# skill" {
		t.Fatalf("copied symlink content = %q, %v", content, err)
	}
	link, err := os.Readlink(filepath.Join(destination, "README.md"))
	if err != nil || link != "SKILL.md" {
		t.Fatalf("copied symlink = %q, %v", link, err)
	}
}

func TestCopySkillRejectsSymlinkOutsideSkill(t *testing.T) {
	root, destination := t.TempDir(), filepath.Join(t.TempDir(), "skill")
	source := filepath.Join(root, "skill")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("# skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "shared.md"), []byte("shared"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../shared.md", filepath.Join(source, "shared.md")); err != nil {
		t.Fatal(err)
	}
	if err := copySkill(source, destination); err == nil {
		t.Fatal("copySkill() accepted a symlink outside the skill root")
	}
}

func TestValidateSkillNameRejectsPathTraversal(t *testing.T) {
	for _, name := range []string{"../escape", "nested/skill", `nested\skill`, ".", ".."} {
		if err := validateSkillName(name); err == nil {
			t.Fatalf("validateSkillName(%q) accepted a path-like name", name)
		}
	}
}

func TestValidateSkillSelectionsRejectsDuplicateNames(t *testing.T) {
	err := validateSkillSelections([]string{"review", "lint", "review"})
	if err == nil || !strings.Contains(err.Error(), `duplicate skill name "review"`) {
		t.Fatalf("validateSkillSelections() error = %v, want duplicate skill error", err)
	}
}

func TestPathWithinCanonicalizesRootAliases(t *testing.T) {
	actualRoot := t.TempDir()
	child := filepath.Join(actualRoot, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "root-alias")
	if err := os.Symlink(actualRoot, alias); err != nil {
		t.Fatal(err)
	}

	if !pathWithin(alias, child) {
		t.Fatalf("pathWithin(%q, %q) rejected a path under the aliased root", alias, child)
	}
}
