package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/agentplugin"
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
	agentConfig := &adk.AgentConfig{
		AgentPlugins: &agentplugin.Resources{Skills: []agentplugin.Skill{{Name: "root", Source: source}}},
		SubAgents:    []*adk.AgentConfig{{Name: "child", AgentPlugins: &agentplugin.Resources{Skills: []agentplugin.Skill{{Name: "child", Source: source}}}}},
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

func TestAddMCPConfigConvertsRuntimeNeutralServers(t *testing.T) {
	agentConfig := &adk.AgentConfig{}
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
