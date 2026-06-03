package substrate

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"strings"
	"text/template"

	atev1alpha1 "github.com/agent-substrate/substrate/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// buildSandboxAgentActorTemplate is invoked from the translator via AgentsBackend.BuildSandbox.

const (
	sandboxAgentIDPrefix     = "asr"
	defaultKagentContainer   = "kagent"
	SandboxAgentLabelKey     = "kagent.dev/sandbox-agent"
	defaultPythonEntrypoint  = "kagent-adk run --host 0.0.0.0 --port 8080"
	defaultGoEntrypoint      = "/app"
)

//go:embed templates/kagent_agent_startup.sh.tmpl
var kagentAgentStartupTmplContent string

var kagentAgentStartupTmpl = template.Must(template.New("kagent_agent_startup").Parse(kagentAgentStartupTmplContent))

type kagentStartupScriptData struct {
	Exec string
}

func (p *Lifecycle) resolveSandboxAgentWorkerPoolRef(ctx context.Context, sa *v1alpha2.SandboxAgent) (types.NamespacedName, error) {
	if p == nil || p.Client == nil {
		return types.NamespacedName{}, fmt.Errorf("substrate lifecycle kubernetes client is required")
	}
	key := p.Defaults.DefaultWorkerPool
	if sub := sa.Spec.Sandbox; sub != nil && sub.Substrate != nil && sub.Substrate.WorkerPoolRef != nil {
		if name := strings.TrimSpace(sub.Substrate.WorkerPoolRef.Name); name != "" {
			key = types.NamespacedName{Namespace: sa.Namespace, Name: name}
		}
	}
	if key.Name == "" {
		return types.NamespacedName{}, fmt.Errorf("spec.sandbox.substrate.workerPoolRef is required when no default substrate WorkerPool is configured")
	}
	if key.Namespace == "" {
		key.Namespace = sa.Namespace
	}
	var wp atev1alpha1.WorkerPool
	if err := p.Client.Get(ctx, key, &wp); err != nil {
		return types.NamespacedName{}, fmt.Errorf("get WorkerPool %s: %w", key, err)
	}
	return key, nil
}

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
	startupScript, containerEnv, err := buildKagentAgentStartup(sa, *kagentContainer)
	if err != nil {
		return nil, err
	}
	env := append([]corev1.EnvVar{}, containerEnv...)
	env = append(env, kagentContainer.Env...)

	desired := &atev1alpha1.ActorTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sandboxAgentActorTemplateName(sa),
			Namespace: sa.Namespace,
			Labels:    sandboxAgentLifecycleLabels(sa),
		},
		Spec: atev1alpha1.ActorTemplateSpec{
			PauseImage: p.Defaults.PauseImage,
			Runsc:      defaultRunscConfig(p.Defaults),
			Containers: []atev1alpha1.Container{{
				Name:    defaultKagentContainer,
				Image:   image,
				Ports:   kagentContainer.Ports,
				Command: []string{"/bin/sh", "-c", startupScript},
				Env:     env,
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

func buildKagentAgentStartup(sa *v1alpha2.SandboxAgent, c corev1.Container) (string, []corev1.EnvVar, error) {
	secretName := sa.Name
	env := []corev1.EnvVar{
		secretEnv("KAGENT_CONFIG_JSON", secretName, "config.json"),
		secretEnv("KAGENT_AGENT_CARD_JSON", secretName, "agent-card.json"),
		secretEnv("KAGENT_SRT_SETTINGS_JSON", secretName, "srt-settings.json", true),
	}
	execLine := defaultRuntimeExecLine(sa)
	if len(c.Command) > 0 {
		execLine = shellJoin(append([]string{}, c.Command...), c.Args)
	} else if len(c.Args) > 0 {
		execLine = shellJoin(c.Args)
	}
	var buf bytes.Buffer
	if err := kagentAgentStartupTmpl.Execute(&buf, kagentStartupScriptData{Exec: execLine}); err != nil {
		return "", nil, fmt.Errorf("render kagent startup script: %w", err)
	}
	return strings.TrimRight(buf.String(), "\n"), env, nil
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

func defaultRuntimeExecLine(sa *v1alpha2.SandboxAgent) string {
	spec := sa.Spec
	if spec.Type == v1alpha2.AgentType_Declarative && spec.Declarative != nil && spec.Declarative.Runtime == v1alpha2.DeclarativeRuntime_Go {
		return defaultGoEntrypoint
	}
	return defaultPythonEntrypoint
}

func shellJoin(parts ...[]string) string {
	var flat []string
	for _, p := range parts {
		flat = append(flat, p...)
	}
	if len(flat) == 0 {
		return ""
	}
	var b strings.Builder
	for i, s := range flat {
		if i > 0 {
			b.WriteByte(' ')
		}
		if strings.ContainsAny(s, " \t'\"") {
			b.WriteByte('"')
			b.WriteString(strings.ReplaceAll(s, `"`, `\"`))
			b.WriteByte('"')
		} else {
			b.WriteString(s)
		}
	}
	return b.String()
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

func sandboxAgentActorTemplateName(sa *v1alpha2.SandboxAgent) string {
	return SandboxAgentActorTemplateName(sa)
}

func sandboxAgentSnapshotsLocation(sa *v1alpha2.SandboxAgent) string {
	if sa == nil {
		return defaultSubstrateSnapshotsLocation("", "")
	}
	if sub := sa.Spec.Sandbox; sub != nil && sub.Substrate != nil && sub.Substrate.SnapshotsConfig != nil {
		if loc := strings.TrimSpace(sub.Substrate.SnapshotsConfig.Location); loc != "" {
			return loc
		}
	}
	return defaultSubstrateSnapshotsLocation(sa.Namespace, sa.Name)
}

// SandboxAgentNameFromLabels returns the SandboxAgent name from generated lifecycle labels.
func SandboxAgentNameFromLabels(labels map[string]string) string {
	if labels == nil {
		return ""
	}
	return strings.TrimSpace(labels[SandboxAgentLabelKey])
}
