package substrate

import (
	"fmt"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// buildSandboxAgentActorTemplate is invoked from the translator via AgentsBackend.BuildSandbox.

const (
	sandboxAgentIDPrefix            = "asr"
	defaultKagentContainer          = "kagent"
	SandboxAgentLabelKey            = "kagent.dev/sandbox-agent"
	defaultGoEntrypoint             = "/app"
	substrateKagentListenPort int32 = 80
)

func (p *Lifecycle) buildSandboxAgentActorTemplate(
	sa *v1alpha2.SandboxAgent,
	wpKey types.NamespacedName,
	podTemplate corev1.PodTemplateSpec,
) (*atev1alpha1.ActorTemplate, error) {
	kagentContainer := findKagentContainer(podTemplate.Spec.Containers)
	if kagentContainer == nil {
		return nil, fmt.Errorf("pod template is missing the kagent container")
	}
	image, err := pinImageRef(kagentContainer.Image)
	if err != nil {
		return nil, err
	}
	command, containerEnv := buildSubstrateKagentContainerCommand(sa)

	desired := &atev1alpha1.ActorTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SandboxAgentActorTemplateName(sa),
			Namespace: sa.Namespace,
			Labels:    sandboxAgentLifecycleLabels(sa),
		},
		Spec: atev1alpha1.ActorTemplateSpec{
			PauseImage: p.Defaults.PauseImage,
			Runsc:      defaultRunscConfig(p.Defaults),
			Containers: []atev1alpha1.Container{{
				Name:    defaultKagentContainer,
				Image:   image,
				Command: command,
				Env:     actorTemplateEnvFromPodEnv(append(containerEnv, kagentContainer.Env...)),
			}},
			WorkerPoolRef: corev1.ObjectReference{Name: wpKey.Name, Namespace: wpKey.Namespace},
			SnapshotsConfig: atev1alpha1.SnapshotsConfig{
				Location: sandboxAgentSnapshotsLocation(sa),
			},
		},
	}
	if err := controllerutil.SetControllerReference(sa, desired, p.Client.Scheme()); err != nil {
		return nil, fmt.Errorf("set ActorTemplate owner ref: %w", err)
	}
	return desired, nil
}

func findKagentContainer(containers []corev1.Container) *corev1.Container {
	for i := range containers {
		if containers[i].Name == defaultKagentContainer {
			return &containers[i]
		}
	}
	if len(containers) > 0 {
		return &containers[0]
	}
	return nil
}

// buildSubstrateKagentContainerCommand returns an ActorTemplate command for Substrate.
// Substrate runs Command directly (no shell). Config is materialized from secret-backed
// env vars at startup via MaterializeFromEnv in the Go ADK entrypoint.
func buildSubstrateKagentContainerCommand(sa *v1alpha2.SandboxAgent) ([]string, []corev1.EnvVar) {
	// KAGENT_NAME / KAGENT_NAMESPACE are normally injected by the translator pod
	// template, but KAGENT_NAMESPACE uses a Downward API fieldRef which Substrate
	// ActorTemplates do not support (it gets dropped by sanitizeActorTemplateEnvVar).
	// Without it the Go ADK derives a wrong app name, and the controller rejects
	// session callbacks with "Session does not belong to this agent". Set both as
	// literals here; they are prepended before the pod env so they win deduplication.
	env := []corev1.EnvVar{
		{Name: "KAGENT_NAME", Value: sa.Name},
		{Name: "KAGENT_NAMESPACE", Value: sa.Namespace},
	}
	env = append(env, kagentAgentSecretEnv(sa)...)
	return buildSubstrateGoKagentCommand(), env
}

// buildSubstrateGoKagentCommand returns the explicit command for the declarative
// Go ADK image. Substrate's atelet copies Command verbatim into the OCI spec's
// Process.Args with no fallback to the image entrypoint, so an empty command
// makes `runsc create` fail with "Spec.Process.Arg must be defined". BYO agents
// are rejected for the substrate platform by validation, so only the declarative
// entrypoint is needed here.
func buildSubstrateGoKagentCommand() []string {
	return []string{
		defaultGoEntrypoint,
		"--host", "0.0.0.0",
		"--port", fmt.Sprintf("%d", substrateKagentListenPort),
	}
}

func kagentAgentSecretEnv(sa *v1alpha2.SandboxAgent) []corev1.EnvVar {
	secretName := sa.Name
	return []corev1.EnvVar{
		secretEnv("KAGENT_CONFIG_JSON", secretName, "config.json"),
		secretEnv("KAGENT_AGENT_CARD_JSON", secretName, "agent-card.json"),
		secretEnv("KAGENT_SRT_SETTINGS_JSON", secretName, "srt-settings.json", true),
	}
}

func secretEnv(name, secret, key string, optional ...bool) corev1.EnvVar {
	ev := corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secret},
				Key:                  key,
			},
		},
	}
	if len(optional) > 0 && optional[0] {
		t := true
		ev.ValueFrom.SecretKeyRef.Optional = &t
	}
	return ev
}

func sandboxAgentLifecycleLabels(sa *v1alpha2.SandboxAgent) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "kagent",
		SandboxAgentLabelKey:           sa.Name,
	}
}

// SandboxAgentActorTemplateName is the generated ActorTemplate name for a SandboxAgent.
func SandboxAgentActorTemplateName(sa *v1alpha2.SandboxAgent) string {
	return truncateDNS1123(sa.Name)
}

func sandboxAgentSnapshotsLocation(sa *v1alpha2.SandboxAgent) string {
	if sa == nil {
		return substrateSnapshotsLocationFor("", "", "")
	}
	loc := ""
	if sa.Spec.Substrate != nil && sa.Spec.Substrate.SnapshotsConfig != nil {
		loc = sa.Spec.Substrate.SnapshotsConfig.Location
	}
	return substrateSnapshotsLocationFor(sa.Namespace, sa.Name, loc)
}

// SandboxAgentNameFromLabels returns the SandboxAgent name from generated lifecycle labels.
func SandboxAgentNameFromLabels(labels map[string]string) string {
	if labels == nil {
		return ""
	}
	return strings.TrimSpace(labels[SandboxAgentLabelKey])
}
