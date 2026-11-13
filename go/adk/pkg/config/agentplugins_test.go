package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/pkg/agentplugins"
)

func TestMaterializeAgentPluginsIsolatesSubagentSkills(t *testing.T) {
	root := t.TempDir()
	paths := AgentPluginPaths{
		Packages: filepath.Join(root, "packages"),
		Skills:   filepath.Join(root, "skills"),
		Data:     filepath.Join(root, "data"),
	}
	source := agentplugin.Source{Git: &agentplugin.GitSource{URL: "unused", Commit: strings.Repeat("a", 40)}}
	agentConfig := &configv1.ADKAgentConfig{
		AgentPlugins: PluginConfig(agentplugin.Resources{Skills: []agentplugin.Skill{{Name: "root", Source: source}}}),
		SubAgents:    []*configv1.ADKAgentConfig{{Name: "child", AgentPlugins: PluginConfig(agentplugin.Resources{Skills: []agentplugin.Skill{{Name: "child", Source: source}}})}},
	}
	for _, path := range []string{
		filepath.Join(paths.Packages, "standalone-0"),
		filepath.Join(paths.Packages, "subagents", "0", "standalone-0"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("# Skill"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := MaterializeAgentPlugins(context.Background(), agentConfig, paths); err != nil {
		t.Fatal(err)
	}
	if agentConfig.SkillsDirectory == agentConfig.SubAgents[0].SkillsDirectory {
		t.Fatalf("root and child share skills directory %q", agentConfig.SkillsDirectory)
	}
	for _, path := range []string{
		filepath.Join(agentConfig.SkillsDirectory, "root", "SKILL.md"),
		filepath.Join(agentConfig.SubAgents[0].SkillsDirectory, "child", "SKILL.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("materialized skill %q: %v", path, err)
		}
	}
}

func TestMaterializeAgentPluginsRejectsClaudeFormatPlugin(t *testing.T) {
	root := t.TempDir()
	paths := AgentPluginPaths{Packages: filepath.Join(root, "packages"), Skills: filepath.Join(root, "skills"), Data: filepath.Join(root, "data")}
	// Pre-seeded package cache: Materialize reuses it instead of fetching.
	manifest := filepath.Join(paths.Packages, "plugin-0", ".claude-plugin", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(`{"name":"acme-claude"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	source := agentplugin.Source{Git: &agentplugin.GitSource{URL: "unused", Commit: strings.Repeat("a", 40)}}
	agentConfig := &configv1.ADKAgentConfig{AgentPlugins: PluginConfig(agentplugin.Resources{Plugins: []agentplugin.Bundle{{Source: source}}})}
	if err := MaterializeAgentPlugins(context.Background(), agentConfig, paths); err == nil {
		t.Fatal("MaterializeAgentPlugins() accepted a Claude-format plugin")
	}
}

func TestMaterializeAgentPluginsReadsRootManifestOfDualManifestPlugin(t *testing.T) {
	t.Run("Given a plugin with an Agent Plugins and a Claude manifest, when the Go ADK materializes it, assert that its selected skill is installed", func(t *testing.T) {
		root := t.TempDir()
		paths := AgentPluginPaths{Packages: filepath.Join(root, "packages"), Skills: filepath.Join(root, "skills"), Data: filepath.Join(root, "data")}
		pluginDir := filepath.Join(root, "plugin")
		for name, content := range map[string]string{
			"plugin.json":                `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"acme.tools"}`,
			".claude-plugin/plugin.json": `{"name":"acme-tools"}`,
			"skills/review/SKILL.md":     "# Review",
		} {
			path := filepath.Join(pluginDir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		agentConfig := &configv1.ADKAgentConfig{AgentPlugins: PluginConfig(agentplugin.Resources{Plugins: []agentplugin.Bundle{{
			Source: agentplugin.Source{Directory: pluginDir}, Skills: []string{"review"},
		}}})}

		if err := MaterializeAgentPlugins(context.Background(), agentConfig, paths); err != nil {
			t.Fatalf("MaterializeAgentPlugins() refused a plugin with both manifests: %v", err)
		}
		if content, err := os.ReadFile(filepath.Join(agentConfig.SkillsDirectory, "review", "SKILL.md")); err != nil || string(content) != "# Review" {
			t.Fatalf("materialized skill = %q, %v", content, err)
		}
	})
}

func TestAddMCPConfigConvertsRuntimeNeutralServers(t *testing.T) {
	agentConfig := &configv1.ADKAgentConfig{}
	addMCPConfig(agentConfig, agentplugins.MCPConfig{
		StreamableHTTP: []agentplugins.RemoteMCPServer{{URL: "https://http.example.com", Headers: map[string]string{"X-Test": "http"}}},
		SSE:            []agentplugins.RemoteMCPServer{{URL: "https://sse.example.com", Headers: map[string]string{"X-Test": "sse"}}},
		Stdio:          []agentplugins.StdioMCPServer{{Command: "server", Args: []string{"--serve"}, Env: map[string]string{"KEY": "value"}, Dir: "/plugin"}},
	})

	if len(agentConfig.HttpTools) != 1 || agentConfig.HttpTools[0].Params.Url != "https://http.example.com" {
		t.Fatalf("HTTP tools = %#v", agentConfig.HttpTools)
	}
	if len(agentConfig.SseTools) != 1 || agentConfig.SseTools[0].Params.Url != "https://sse.example.com" {
		t.Fatalf("SSE tools = %#v", agentConfig.SseTools)
	}
	if len(agentConfig.StdioTools) != 1 || agentConfig.StdioTools[0].Command != "server" {
		t.Fatalf("stdio tools = %#v", agentConfig.StdioTools)
	}
}
