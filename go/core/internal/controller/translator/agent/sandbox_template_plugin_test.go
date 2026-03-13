package agent

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	extv1alpha1 "sigs.k8s.io/agent-sandbox/extensions/api/v1alpha1"
)

func TestSandboxTemplatePlugin_SkipsNonDeclarative(t *testing.T) {
	plugin := NewSandboxTemplatePlugin()
	agent := &v1alpha2.Agent{
		Spec: v1alpha2.AgentSpec{Type: v1alpha2.AgentType_BYO},
	}
	outputs := &AgentOutputs{}

	if err := plugin.ProcessAgent(context.Background(), agent, outputs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs.Manifest) != 0 {
		t.Errorf("expected no manifest objects for BYO agent, got %d", len(outputs.Manifest))
	}
}

func TestSandboxTemplatePlugin_SkipsDisabled(t *testing.T) {
	plugin := NewSandboxTemplatePlugin()
	agent := &v1alpha2.Agent{
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				Workspace: &v1alpha2.WorkspaceSpec{Enabled: false},
			},
		},
	}
	outputs := &AgentOutputs{}

	if err := plugin.ProcessAgent(context.Background(), agent, outputs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs.Manifest) != 0 {
		t.Errorf("expected no manifest for disabled workspace, got %d", len(outputs.Manifest))
	}
}

func TestSandboxTemplatePlugin_SkipsUserTemplateRef(t *testing.T) {
	plugin := NewSandboxTemplatePlugin()
	agent := &v1alpha2.Agent{
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				Workspace: &v1alpha2.WorkspaceSpec{
					Enabled:     true,
					TemplateRef: "my-custom-template",
				},
			},
		},
	}
	outputs := &AgentOutputs{}

	if err := plugin.ProcessAgent(context.Background(), agent, outputs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs.Manifest) != 0 {
		t.Errorf("expected no manifest when templateRef is set, got %d", len(outputs.Manifest))
	}
}

func TestSandboxTemplatePlugin_GeneratesStandaloneWorkspace(t *testing.T) {
	plugin := NewSandboxTemplatePlugin()
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-agent",
			Namespace: "test-ns",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			// No skills configured — workspace works standalone.
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				Workspace: &v1alpha2.WorkspaceSpec{Enabled: true},
			},
		},
	}
	outputs := &AgentOutputs{
		Config: &adk.AgentConfig{
			Workspace: &adk.WorkspaceConfig{Enabled: true},
		},
	}

	if err := plugin.ProcessAgent(context.Background(), agent, outputs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(outputs.Manifest) != 1 {
		t.Fatalf("expected 1 manifest object, got %d", len(outputs.Manifest))
	}

	st, ok := outputs.Manifest[0].(*extv1alpha1.SandboxTemplate)
	if !ok {
		t.Fatalf("expected *SandboxTemplate, got %T", outputs.Manifest[0])
	}

	if st.Name != "my-agent-sandbox" {
		t.Errorf("expected name=my-agent-sandbox, got %s", st.Name)
	}

	// No skills → no init containers, no volumes.
	if len(st.Spec.PodTemplate.Spec.InitContainers) != 0 {
		t.Errorf("expected 0 init containers for standalone workspace, got %d", len(st.Spec.PodTemplate.Spec.InitContainers))
	}
	if len(st.Spec.PodTemplate.Spec.Volumes) != 0 {
		t.Errorf("expected 0 volumes for standalone workspace, got %d", len(st.Spec.PodTemplate.Spec.Volumes))
	}
}

func TestSandboxTemplatePlugin_GeneratesWithSkills(t *testing.T) {
	plugin := NewSandboxTemplatePlugin()
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-agent",
			Namespace: "test-ns",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Skills: &v1alpha2.SkillForAgent{
				Refs: []string{"ghcr.io/org/my-skill:v1"},
			},
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				Workspace: &v1alpha2.WorkspaceSpec{Enabled: true},
			},
		},
	}
	outputs := &AgentOutputs{
		Config: &adk.AgentConfig{
			Workspace: &adk.WorkspaceConfig{Enabled: true},
		},
	}

	if err := plugin.ProcessAgent(context.Background(), agent, outputs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(outputs.Manifest) != 1 {
		t.Fatalf("expected 1 manifest object, got %d", len(outputs.Manifest))
	}

	st := outputs.Manifest[0].(*extv1alpha1.SandboxTemplate)

	// With skills → init container present.
	if len(st.Spec.PodTemplate.Spec.InitContainers) != 1 {
		t.Fatalf("expected 1 init container, got %d", len(st.Spec.PodTemplate.Spec.InitContainers))
	}
	if st.Spec.PodTemplate.Spec.InitContainers[0].Name != "skills-init" {
		t.Errorf("expected init container named skills-init, got %s", st.Spec.PodTemplate.Spec.InitContainers[0].Name)
	}

	// Sandbox container should have skills volume mount.
	container := st.Spec.PodTemplate.Spec.Containers[0]
	if len(container.VolumeMounts) != 1 || container.VolumeMounts[0].Name != "kagent-skills" {
		t.Errorf("expected kagent-skills volume mount, got %v", container.VolumeMounts)
	}
}

func TestSandboxTemplatePlugin_GetOwnedResourceTypes(t *testing.T) {
	plugin := NewSandboxTemplatePlugin()
	types := plugin.GetOwnedResourceTypes()
	if len(types) != 1 {
		t.Fatalf("expected 1 owned type, got %d", len(types))
	}
	if _, ok := types[0].(*extv1alpha1.SandboxTemplate); !ok {
		t.Errorf("expected *SandboxTemplate, got %T", types[0])
	}
}

func TestSandboxTemplateName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "normal", input: "my-agent", expected: "my-agent-sandbox"},
		{name: "truncation", input: "very-long-agent-name-that-exceeds-kubernetes-name-length-limits-oh-no", expected: "very-long-agent-name-that-exceeds-kubernetes-name-length-limits"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sandboxTemplateName(tt.input)
			if got != tt.expected {
				t.Errorf("sandboxTemplateName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
			if len(got) > 63 {
				t.Errorf("name exceeds 63 chars: %d", len(got))
			}
		})
	}
}
