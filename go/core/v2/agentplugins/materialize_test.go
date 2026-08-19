package agentplugins

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeGitPlugin(t *testing.T) {
	repository := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", repository}, args...)...)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	git("init")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	if err := os.MkdirAll(filepath.Join(repository, "skills", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"plugin.json":            `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"acme.test"}`,
		"mcp.json":               `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"local":{"type":"stdio","command":"server"}}}`,
		"skills/review/SKILL.md": "# Review",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(repository, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("add", ".")
	git("commit", "-m", "plugin")
	commit := git("rev-parse", "HEAD")

	root := t.TempDir()
	result, err := Materialize(context.Background(), Config{Plugins: []Plugin{{
		Source: Source{Git: &Git{URL: repository, Commit: commit}}, Skills: []string{"review"},
	}}}, Paths{Plugins: filepath.Join(root, "plugins"), Skills: filepath.Join(root, "skills"), Data: filepath.Join(root, "data")})
	if err != nil {
		t.Fatal(err)
	}
	if !result.HasSkills || len(result.Stdio) != 1 || result.Stdio[0].Command != "server" {
		t.Fatalf("materialized plugin = %#v", result)
	}
	if content, err := os.ReadFile(filepath.Join(root, "skills", "review", "SKILL.md")); err != nil || string(content) != "# Review" {
		t.Fatalf("materialized skill = %q, %v", content, err)
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
	manifest, err := loadManifest(root)
	if err != nil || manifest.Name != "acme.tools" {
		t.Fatalf("loadManifest() = %#v, %v", manifest, err)
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
	if stdio.Command != command || stdio.Args[0] != "--root="+root || stdio.Env["STATE"] != filepath.Join(data, "state") || stdio.CWD != filepath.Join(data, "work") {
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
