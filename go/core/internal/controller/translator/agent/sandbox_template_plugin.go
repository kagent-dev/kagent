package agent

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "sigs.k8s.io/agent-sandbox/api/v1alpha1"
	extv1alpha1 "sigs.k8s.io/agent-sandbox/extensions/api/v1alpha1"
)

// DefaultSandboxMCPImageConfig is the image config for the sandbox MCP server
// container that runs inside sandbox pods.
var DefaultSandboxMCPImageConfig = ImageConfig{
	Registry:   "ghcr.io",
	Tag:        version.Get().Version,
	PullPolicy: string(corev1.PullIfNotPresent),
	Repository: "kagent-dev/kagent-sandbox-mcp",
}

// SandboxTemplatePlugin is a TranslatorPlugin that generates a per-agent
// SandboxTemplate when workspace is enabled and no user-provided templateRef
// is set.
//
// When skills are also configured, the generated template includes a
// skills-init container that fetches skills into /skills. The sandbox-mcp
// container always runs and exposes exec, filesystem, and (when skills are
// present) get_skill MCP tools.
type SandboxTemplatePlugin struct{}

var _ TranslatorPlugin = (*SandboxTemplatePlugin)(nil)

func NewSandboxTemplatePlugin() *SandboxTemplatePlugin {
	return &SandboxTemplatePlugin{}
}

// GetOwnedResourceTypes returns the types this plugin may create.
func (p *SandboxTemplatePlugin) GetOwnedResourceTypes() []client.Object {
	return []client.Object{
		&extv1alpha1.SandboxTemplate{},
	}
}

// ProcessAgent generates a SandboxTemplate when workspace is enabled.
func (p *SandboxTemplatePlugin) ProcessAgent(ctx context.Context, agent *v1alpha2.Agent, outputs *AgentOutputs) error {
	if agent.Spec.Type != v1alpha2.AgentType_Declarative {
		return nil
	}
	if agent.Spec.Declarative == nil {
		return nil
	}

	ws := agent.Spec.Declarative.Workspace
	if ws == nil || !ws.Enabled {
		return nil
	}

	// If the user provided their own templateRef, don't generate one.
	if ws.TemplateRef != "" {
		return nil
	}

	templateName := sandboxTemplateName(agent.Name)

	// Check if skills are configured.
	hasSkills := agent.Spec.Skills != nil &&
		(len(agent.Spec.Skills.Refs) > 0 || len(agent.Spec.Skills.GitRefs) > 0)

	var initContainers []corev1.Container
	var volumes []corev1.Volume
	var sandboxVolumeMounts []corev1.VolumeMount
	var sandboxEnv []corev1.EnvVar

	if hasSkills {
		// Build the skills-init container.
		var skills []string
		var gitRefs []v1alpha2.GitRepo
		var gitAuthSecretRef *corev1.LocalObjectReference
		if agent.Spec.Skills != nil {
			skills = agent.Spec.Skills.Refs
			gitRefs = agent.Spec.Skills.GitRefs
			gitAuthSecretRef = agent.Spec.Skills.GitAuthSecretRef
		}
		insecure := agent.Spec.Skills != nil && agent.Spec.Skills.InsecureSkipVerify

		initContainer, extraVolumes, err := buildSkillsInitContainer(gitRefs, gitAuthSecretRef, skills, insecure, nil)
		if err != nil {
			return fmt.Errorf("failed to build skills init container for sandbox template: %w", err)
		}
		initContainers = append(initContainers, initContainer)

		volumes = append(volumes, corev1.Volume{
			Name:         "kagent-skills",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
		volumes = append(volumes, extraVolumes...)

		sandboxVolumeMounts = append(sandboxVolumeMounts, corev1.VolumeMount{
			Name: "kagent-skills", MountPath: "/skills", ReadOnly: true,
		})
		sandboxEnv = append(sandboxEnv, corev1.EnvVar{
			Name: "SKILLS_DIR", Value: "/skills",
		})
	}

	template := &extv1alpha1.SandboxTemplate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "extensions.agents.x-k8s.io/v1alpha1",
			Kind:       "SandboxTemplate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      templateName,
			Namespace: agent.Namespace,
			Labels: map[string]string{
				"app":                  "kagent",
				"kagent.dev/agent":     agent.Name,
				"kagent.dev/component": "sandbox-template",
			},
		},
		Spec: extv1alpha1.SandboxTemplateSpec{
			PodTemplate: sandboxv1alpha1.PodTemplate{
				Spec: corev1.PodSpec{
					InitContainers: initContainers,
					Containers: []corev1.Container{
						{
							Name:  "sandbox",
							Image: DefaultSandboxMCPImageConfig.Image(),
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
							},
							Env:          sandboxEnv,
							VolumeMounts: sandboxVolumeMounts,
						},
					},
					Volumes: volumes,
				},
			},
		},
	}

	outputs.Manifest = append(outputs.Manifest, template)

	return nil
}

// sandboxTemplateName returns the deterministic name for an agent's auto-generated
// SandboxTemplate.
func sandboxTemplateName(agentName string) string {
	name := agentName + "-sandbox"
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}
