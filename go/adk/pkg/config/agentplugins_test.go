package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/pkg/agentplugins"
	"github.com/kagent-dev/kagent/go/internal/testutil"
)

func TestMaterializeAgentPluginsIsolatesSubagentSkills(t *testing.T) {
	root := t.TempDir()
	paths := AgentPluginPaths{
		Packages: filepath.Join(root, "packages"),
		Skills:   filepath.Join(root, "skills"),
		Data:     filepath.Join(root, "data"),
	}
	repository, commit := testutil.CommitFiles(t, map[string]string{"SKILL.md": "# Skill"})
	source := agentplugin.Source{Git: &agentplugin.GitSource{URL: repository, Commit: commit}}
	agentConfig := &configv1.ADKAgentConfig{
		AgentPlugins: PluginConfig(agentplugin.Resources{Skills: []agentplugin.Skill{{Name: "root", Source: source}}}),
		SubAgents:    []*configv1.ADKAgentConfig{{Name: "child", AgentPlugins: PluginConfig(agentplugin.Resources{Skills: []agentplugin.Skill{{Name: "child", Source: source}}})}},
	}
	if err := MaterializeAgentPlugins(t.Context(), agentConfig, paths); err != nil {
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
	repository, commit := testutil.CommitFiles(t, map[string]string{".claude-plugin/plugin.json": `{"name":"acme-claude"}`})
	source := agentplugin.Source{Git: &agentplugin.GitSource{URL: repository, Commit: commit}}
	agentConfig := &configv1.ADKAgentConfig{AgentPlugins: PluginConfig(agentplugin.Resources{Plugins: []agentplugin.Bundle{{Source: source}}})}
	if err := MaterializeAgentPlugins(t.Context(), agentConfig, paths); err == nil {
		t.Fatal("MaterializeAgentPlugins() accepted a Claude-format plugin")
	}
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
