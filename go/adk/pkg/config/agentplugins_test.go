package config

import (
	"context"
	"os"
	"os/exec"
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

func TestMaterializeAgentPluginsAppendsPluginBinsToPath(t *testing.T) {
	t.Run("Given root and sub-agent plugins with bin directories, when plugins are materialized twice, assert that PATH ends with each bin once and image binaries win", func(t *testing.T) {
		imageBin := t.TempDir()
		writeExecutable(t, filepath.Join(imageBin, "shared"))
		t.Setenv("PATH", imageBin)
		mount := t.TempDir()
		for name, tool := range map[string]string{"root-plugin": "root-tool", "child-plugin": "child-tool"} {
			writeExecutable(t, filepath.Join(mount, name, "bin", tool))
			writeExecutable(t, filepath.Join(mount, name, "bin", "shared"))
			manifest := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"` + name + `"}`
			if err := os.WriteFile(filepath.Join(mount, name, "plugin.json"), []byte(manifest), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		plugin := func(path string) *configv1.ADKAgentPlugins {
			return PluginConfig(agentplugin.Resources{Plugins: []agentplugin.Bundle{{Source: agentplugin.Source{Directory: mount, Path: path}}}})
		}
		root := t.TempDir()
		paths := AgentPluginPaths{Packages: filepath.Join(root, "packages"), Skills: filepath.Join(root, "skills"), Data: filepath.Join(root, "data")}

		for range 2 {
			agentConfig := &configv1.ADKAgentConfig{
				AgentPlugins: plugin("root-plugin"),
				SubAgents:    []*configv1.ADKAgentConfig{{Name: "child", AgentPlugins: plugin("child-plugin")}},
			}
			if err := MaterializeAgentPlugins(context.Background(), agentConfig, paths); err != nil {
				t.Fatal(err)
			}
		}

		resolved, err := filepath.EvalSymlinks(mount)
		if err != nil {
			t.Fatal(err)
		}
		rootBin, childBin := filepath.Join(resolved, "root-plugin", "bin"), filepath.Join(resolved, "child-plugin", "bin")
		if got, want := os.Getenv("PATH"), strings.Join([]string{imageBin, rootBin, childBin}, string(os.PathListSeparator)); got != want {
			t.Fatalf("PATH = %q, want %q", got, want)
		}
		for name, wantDir := range map[string]string{"root-tool": rootBin, "child-tool": childBin, "shared": imageBin} {
			if got, err := exec.LookPath(name); err != nil || filepath.Dir(got) != wantDir {
				t.Fatalf("LookPath(%q) = %q, %v; want it in %q", name, got, err, wantDir)
			}
		}
	})
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
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
