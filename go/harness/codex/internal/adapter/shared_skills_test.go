package adapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
	"github.com/kagent-dev/kagent/go/harness/codex/config"
)

func TestSharedAgentsDisableUnselectedSkills(t *testing.T) {
	durable := t.TempDir()
	source := t.TempDir()
	cfg := config.Production("gpt-test", "root instructions")
	cfg.Provider = config.Provider{Name: "openai"}
	cfg.SkillResources = &agentplugin.Resources{}
	for _, name := range []string{"root", "review", "deploy"} {
		path := filepath.Join(source, name)
		require.NoError(t, os.MkdirAll(path, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: Test skill.\n---\n"+name), 0o644))
		cfg.SkillResources.Skills = append(cfg.SkillResources.Skills, agentplugin.Skill{Name: name, Source: agentplugin.Source{Directory: source, Path: name}})
	}
	cfg.Agents = map[string]config.Agent{
		"reviewer": {Description: "Reviews", Instruction: "Review", Model: "gpt-test", Skills: []string{"review"}},
		"deployer": {Description: "Deploys", Instruction: "Deploy", Model: "gpt-test", Skills: []string{"deploy"}},
		"plain":    {Description: "No skills", Instruction: "Help", Model: "gpt-test"},
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	_, err = New(t.Context(), Input{ConfigJSON: raw, Workspace: filepath.Join(durable, "workspace"), DurableDir: durable})
	require.NoError(t, err)
	for role, want := range map[string][]string{"reviewer": {"deploy", "root"}, "deployer": {"review", "root"}, "plain": {"deploy", "review", "root"}} {
		raw, err := os.ReadFile(filepath.Join(durable, "codex", "agents", role+".toml"))
		require.NoError(t, err)
		var parsed nativeAgentConfig
		require.NoError(t, toml.Unmarshal(raw, &parsed))
		require.NotNil(t, parsed.Skills)
		var disabled []string
		for _, selection := range parsed.Skills.Config {
			require.False(t, selection.Enabled)
			require.Equal(t, "SKILL.md", filepath.Base(selection.Path))
			disabled = append(disabled, filepath.Base(filepath.Dir(selection.Path)))
		}
		require.Equal(t, want, disabled, role)
	}
	for _, skill := range cfg.SkillResources.Skills {
		_, err := os.Stat(filepath.Join(config.SkillsRoot(durable), skill.Name, "SKILL.md"))
		require.NoError(t, err, "root must discover every installed skill")
	}
}
