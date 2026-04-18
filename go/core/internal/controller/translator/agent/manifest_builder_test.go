package agent

import (
	"encoding/json"
	"testing"

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
