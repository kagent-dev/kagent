package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildSRTSettingsJSON_DefaultDenyConfig(t *testing.T) {
	got, err := buildSRTSettingsJSON(nil)
	if err != nil {
		t.Fatalf("buildSRTSettingsJSON() error = %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(got, &settings); err != nil {
		t.Fatalf("failed to unmarshal settings: %v", err)
	}

	network, ok := settings["network"].(map[string]any)
	if !ok {
		t.Fatalf("settings.network missing or wrong type: %#v", settings["network"])
	}
	if got := network["allowedDomains"]; len(got.([]any)) != 0 {
		t.Fatalf("allowedDomains = %#v, want empty list", got)
	}
	if got := network["deniedDomains"]; len(got.([]any)) != 0 {
		t.Fatalf("deniedDomains = %#v, want empty list", got)
	}

	filesystem, ok := settings["filesystem"].(map[string]any)
	if !ok {
		t.Fatalf("settings.filesystem missing or wrong type: %#v", settings["filesystem"])
	}
	if got := filesystem["denyRead"]; len(got.([]any)) != 0 {
		t.Fatalf("denyRead = %#v, want empty list", got)
	}
	if got := filesystem["allowWrite"].([]any); len(got) != 2 || got[0] != "." || got[1] != "/tmp" {
		t.Fatalf("allowWrite = %#v, want ['.','/tmp']", got)
	}
	if got := filesystem["denyWrite"]; len(got.([]any)) != 0 {
		t.Fatalf("denyWrite = %#v, want empty list", got)
	}
}

func TestNeedsSRTSettings(t *testing.T) {
	declarativeAgent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "decl", Namespace: "default"},
		Spec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{},
		},
	}
	skillsAgent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "skills", Namespace: "default"},
		Spec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{},
			Skills:      &v1alpha2.SkillForAgent{Refs: []string{"example.com/skill:latest"}},
		},
	}
	executeCode := true
	codeAgent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "code", Namespace: "default"},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				ExecuteCodeBlocks: &executeCode,
			},
		},
	}
	byoAgent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "byo", Namespace: "default"},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_BYO,
			BYO:  &v1alpha2.BYOAgentSpec{},
		},
	}

	if needsSRTSettings(declarativeAgent, nil) {
		t.Fatal("declarative agents without sandboxed execution should not get srt settings")
	}
	if !needsSRTSettings(skillsAgent, nil) {
		t.Fatal("declarative agents with skills should get srt settings")
	}
	if !needsSRTSettings(codeAgent, nil) {
		t.Fatal("declarative agents with executeCodeBlocks should get srt settings")
	}
	if needsSRTSettings(byoAgent, nil) {
		t.Fatal("BYO agents should not get srt settings unless sandbox config is set")
	}
	if !needsSRTSettings(byoAgent, &v1alpha2.SandboxConfig{}) {
		t.Fatal("BYO agents with sandbox config should get srt settings")
	}
}

func TestNeedsAgentConfig(t *testing.T) {
	declarativeAgent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "decl", Namespace: "default"},
		Spec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{},
		},
	}
	byoAgent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "byo", Namespace: "default"},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_BYO,
			BYO:  &v1alpha2.BYOAgentSpec{},
		},
	}
	cfg := &adk.AgentConfig{Description: "a test agent"}

	if needsAgentConfig(declarativeAgent, nil) {
		t.Fatal("a nil config should never be rendered")
	}
	if !needsAgentConfig(declarativeAgent, cfg) {
		t.Fatal("declarative agents should get the rendered agent config")
	}
	if needsAgentConfig(byoAgent, cfg) {
		t.Fatal("BYO agents should not get the rendered agent config")
	}
}

func byoManifestContext() manifestContext {
	return manifestContext{
		agent: &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "byo", Namespace: "default"},
			Spec: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_BYO,
				BYO:  &v1alpha2.BYOAgentSpec{},
			},
		},
		deployment: &resolvedDeployment{},
	}
}

// TestBuildConfigSecret_BYOOmitsAgentConfig guards against handing BYO agents the
// config rendered for the declarative runtime. That config carries no model, and the
// runtime schema requires one, so a BYO image that loads /config/config.json on
// startup fails validation and crashloops.
func TestBuildConfigSecret_BYOOmitsAgentConfig(t *testing.T) {
	translator := &adkApiTranslator{}
	// The minimal config the compiler builds for BYO agents: a description, no model.
	cfg := &adk.AgentConfig{Description: "A BYO test agent"}

	got, err := translator.buildConfigSecret(context.Background(), byoManifestContext(), cfg, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildConfigSecret() error = %v", err)
	}

	if data := got.secret.StringData["config.json"]; data != "" {
		t.Fatalf("config.json = %q, want empty for BYO agents", data)
	}
	if len(got.volumes) != 0 {
		t.Fatalf("volumes = %#v, want none for BYO agents", got.volumes)
	}
	if len(got.mounts) != 0 {
		t.Fatalf("mounts = %#v, want none for BYO agents", got.mounts)
	}
}

// TestBuildConfigSecret_BYOWithSandboxMountsOnlySRTSettings covers the one case where a
// BYO agent still needs the config volume. The agent config stays empty; only the srt
// settings are populated.
func TestBuildConfigSecret_BYOWithSandboxMountsOnlySRTSettings(t *testing.T) {
	translator := &adkApiTranslator{}
	cfg := &adk.AgentConfig{Description: "A BYO test agent"}

	got, err := translator.buildConfigSecret(context.Background(), byoManifestContext(), cfg, &v1alpha2.SandboxConfig{}, nil, nil)
	if err != nil {
		t.Fatalf("buildConfigSecret() error = %v", err)
	}

	if data := got.secret.StringData["config.json"]; data != "" {
		t.Fatalf("config.json = %q, want empty for BYO agents", data)
	}
	if got.secret.StringData["srt-settings.json"] == "" {
		t.Fatal("srt-settings.json should be populated for sandboxed BYO agents")
	}
	if len(got.mounts) != 1 || got.mounts[0].MountPath != "/config" {
		t.Fatalf("mounts = %#v, want a single /config mount", got.mounts)
	}
}

// TestBuildConfigSecret_DeclarativeKeepsAgentConfig pins the declarative path, which
// must keep receiving the rendered config and its volume.
func TestBuildConfigSecret_DeclarativeKeepsAgentConfig(t *testing.T) {
	translator := &adkApiTranslator{}
	manifestCtx := manifestContext{
		agent: &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "decl", Namespace: "default"},
			Spec: v1alpha2.AgentSpec{
				Type:        v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{},
			},
		},
		deployment: &resolvedDeployment{},
	}
	cfg := &adk.AgentConfig{Description: "a declarative agent"}

	got, err := translator.buildConfigSecret(context.Background(), manifestCtx, cfg, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildConfigSecret() error = %v", err)
	}

	if got.secret.StringData["config.json"] == "" {
		t.Fatal("config.json should be populated for declarative agents")
	}
	if len(got.volumes) != 1 || got.volumes[0].Name != "config" {
		t.Fatalf("volumes = %#v, want a single config volume", got.volumes)
	}
	if len(got.mounts) != 1 || got.mounts[0].MountPath != "/config" {
		t.Fatalf("mounts = %#v, want a single /config mount", got.mounts)
	}
}

func TestBuildConfigSecretData_OmitsEmptySRTSettings(t *testing.T) {
	data := buildConfigSecretData(`{"app":"ok"}`, `{"card":"ok"}`, "")

	if data["config.json"] == "" {
		t.Fatal("config.json should be present")
	}
	if data["agent-card.json"] == "" {
		t.Fatal("agent-card.json should be present")
	}
	if _, ok := data["srt-settings.json"]; ok {
		t.Fatal("srt-settings.json should be omitted when empty")
	}
}

func TestBuildConfigSecretData_IncludesSRTSettingsWhenPresent(t *testing.T) {
	data := buildConfigSecretData(`{"app":"ok"}`, `{"card":"ok"}`, `{"network":{}}`)

	if got := data["srt-settings.json"]; got == "" {
		t.Fatal("srt-settings.json should be present when non-empty")
	}
}
