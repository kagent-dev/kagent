package substrate

import (
	"fmt"
	"strconv"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/consts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// buildSandboxAgentActorTemplate is invoked from the translator via AgentsBackend.BuildSandbox.

const (
	sandboxAgentIDPrefix   = "asr"
	defaultKagentContainer = "kagent"
	SandboxAgentLabelKey   = "kagent.dev/sandbox-agent"
	// desiredGenerationAnnotation records the SandboxAgent generation that last applied a given
	// ActorTemplate; the template for the current desired config carries the highest value.
	desiredGenerationAnnotation = "kagent.dev/desired-generation"
	defaultGoEntrypoint         = "/app"
	// defaultPythonEntrypoint is the absolute path to the kagent-adk console script in the
	// Python ADK image venv. Substrate copies Command verbatim into the OCI Process.Args with
	// no PATH/entrypoint fallback, so the path must be explicit and kept in sync with the
	// Python Dockerfile's UV_PROJECT_ENVIRONMENT (/.kagent/.venv).
	defaultPythonEntrypoint         = "/.kagent/.venv/bin/kagent-adk"
	substrateKagentListenPort int32 = 80
	// pythonRuntimeLibPath / pythonVenvPath mirror the Python ADK image layout
	// (python/Dockerfile): bundled shared libs live on LD_LIBRARY_PATH and the project
	// venv at UV_PROJECT_ENVIRONMENT. Substrate ignores the image's ENV directives (see
	// pythonRuntimeImageEnv), so these are re-supplied via the ActorTemplate env.
	pythonRuntimeLibPath = "/usr/lib/kagent-libs"
	pythonVenvPath       = "/.kagent/.venv"
	// pythonRuntimePath mirrors the image's `ENV PATH="/.kagent/.venv/bin:$PATH"`. Substrate
	// builds the OCI Process.Env from a hardcoded PATH that does NOT include the venv bin, so any
	// bare-name console-script execution (or locating the venv interpreter without an absolute
	// path) would fail; re-supply it with the venv bin first, then the standard system dirs.
	pythonRuntimePath = "/.kagent/.venv/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

	// sandboxAgentTemplateNameMaxBase reserves room in the 63-char DNS-1123 budget for
	// the "-<hash>" suffix (hash is up to 16 hex chars). A golden snapshot is an immutable
	// memory image, so a config change must produce a NEW ActorTemplate (substrate snapshots
	// once and no-ops in PhaseReady); folding the shared consts.ConfigHashAnnotation hash
	// into the template name (and mirroring it as an annotation) is what achieves that.
	sandboxAgentTemplateNameMaxBase = 46
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
	// The config hash is computed by the translator and stamped on the pod template.
	// Folding it into the ActorTemplate name makes a config change create a new template
	// (and thus a fresh golden snapshot) instead of mutating one substrate will never
	// re-snapshot. The annotation carries the same hash for the chat path and reaper.
	configHash := shortConfigHash(podTemplate.Annotations[consts.ConfigHashAnnotation])

	// The config is read from a per-config-hash Secret (cloned in AgentsBackend.BuildSandbox),
	// not the shared per-agent Secret. A golden snapshot materializes config.json from this
	// Secret's env at build time; if every config revision shared one Secret name, substrate's
	// secret-value cache could hand a stale revision to a new golden, so the golden would freeze
	// the wrong provider's config (e.g. an OpenAI agent in a Gemini actor). The per-hash name is
	// a cache-miss for each distinct config, so each golden captures exactly its own config.
	secretName := sandboxAgentConfigSecretName(sa, configHash)
	command, containerEnv, err := buildSubstrateKagentContainerCommand(sa, kagentContainer, secretName)
	if err != nil {
		return nil, err
	}

	annotations := map[string]string{
		// The agent generation at the time this template was (re)applied. It bumps only on a spec
		// change, so the template matching the agent's CURRENT desired config always carries the
		// highest generation — including on a flip-back to a retained older config, where the
		// reused template is re-applied with the new generation. ResolveCurrentActorTemplate uses
		// this (not creationTimestamp) to pick the desired template, so chat/readiness follow the
		// current config rather than whichever golden was built most recently.
		desiredGenerationAnnotation: strconv.FormatInt(sa.GetGeneration(), 10),
	}
	if configHash != "" {
		annotations[consts.ConfigHashAnnotation] = configHash
	}

	desired := &atev1alpha1.ActorTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:        sandboxAgentActorTemplateName(sa, configHash),
			Namespace:   sa.Namespace,
			Labels:      sandboxAgentLifecycleLabels(sa),
			Annotations: annotations,
		},
		Spec: atev1alpha1.ActorTemplateSpec{
			PauseImage:   p.Defaults.PauseImage,
			SandboxClass: atev1alpha1.SandboxClassGvisor,
			Containers: []atev1alpha1.Container{{
				Name:    defaultKagentContainer,
				Image:   image,
				Command: command,
				Env:     actorTemplateEnvFromPodEnv(append(containerEnv, kagentContainer.Env...)),
			}},
			WorkerSelector: workerSelectorForPool(wpKey),
			SnapshotsConfig: atev1alpha1.SnapshotsConfig{
				Location: sandboxAgentSnapshotsLocation(sa),
				// Mirror substrate's CRD defaults so kagent's spec-drift check
				// (apiequality.Semantic.DeepEqual) treats them as equal to the
				// values the API server fills in on admission — otherwise kagent
				// re-creates the ActorTemplate every reconcile in a hot loop.
				OnPause:  atev1alpha1.SnapshotScopeFull,
				OnCommit: atev1alpha1.SnapshotScopeFull,
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

// buildSubstrateKagentContainerCommand returns the ActorTemplate command and the prepended
// env for a SandboxAgent on Substrate. Substrate runs Command directly (no shell) and copies
// it verbatim into the OCI Process.Args with no PATH/entrypoint fallback, so the command must
// be fully explicit.
//
// For declarative agents the command is the runtime ADK entrypoint and config is materialized
// from secret-backed env vars at startup (Go: MaterializeFromEnv in the Go ADK; Python: the
// `static` command materializes the same env vars before reading /config). For BYO agents the
// user-provided container Command/Args are used verbatim; the BYO image must serve A2A on the
// substrate listen port (80).
func buildSubstrateKagentContainerCommand(sa *v1alpha2.SandboxAgent, container *corev1.Container, configSecretName string) ([]string, []corev1.EnvVar, error) {
	// KAGENT_NAME / KAGENT_NAMESPACE are normally injected by the translator pod
	// template, but KAGENT_NAMESPACE uses a Downward API fieldRef which Substrate
	// ActorTemplates do not support (it gets dropped by sanitizeActorTemplateEnvVar).
	// Without it the ADK derives a wrong app name, and the controller rejects
	// session callbacks with "Session does not belong to this agent". Set both as
	// literals here; they are prepended before the pod env so they win deduplication.
	env := []corev1.EnvVar{
		{Name: "KAGENT_NAME", Value: sa.Name},
		{Name: "KAGENT_NAMESPACE", Value: sa.Namespace},
	}

	spec := sa.GetAgentSpec()
	if spec != nil && spec.Type == v1alpha2.AgentType_BYO {
		// BYO: use the explicit container command + args verbatim. Validation
		// (ValidateSubstrateSandboxAgentSpec) guarantees a command is set.
		if len(container.Command) == 0 {
			return nil, nil, fmt.Errorf("BYO substrate agent %q is missing an explicit container command", sa.Name)
		}
		cmd := append([]string{}, container.Command...)
		cmd = append(cmd, container.Args...)
		return cmd, env, nil
	}

	// Declarative: secret-backed config is materialized at startup from the per-config-hash Secret.
	env = append(env, kagentAgentSecretEnv(configSecretName)...)
	runtime := v1alpha2.EffectiveDeclarativeRuntime(sa.GetAgentSpec())
	if runtime == v1alpha2.DeclarativeRuntime_Python {
		env = append(env, pythonRuntimeImageEnv()...)
	}
	return buildSubstrateDeclarativeCommand(runtime), env, nil
}

// pythonRuntimeImageEnv returns the runtime-critical ENV directives baked into the Python
// ADK image (python/Dockerfile). Substrate builds the OCI Process.Env from a hardcoded PATH
// plus the ActorTemplate env only — it does NOT apply the image's ENV directives (the same
// way it ignores the image entrypoint). Without LD_LIBRARY_PATH the standalone interpreter
// cannot locate its bundled shared libraries (libz, libsqlite3, ...) and crashes on import
// (e.g. numpy: "ImportError: libz.so.1: cannot open shared object file"); the failed startup
// then surfaces as a gVisor "inconsistent private memory files on restore" error because the
// golden snapshot captures only the pause container. The Go static binary needs none of this.
// Keep in sync with the final-stage ENV block of python/Dockerfile.
func pythonRuntimeImageEnv() []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "PATH", Value: pythonRuntimePath},
		{Name: "LD_LIBRARY_PATH", Value: pythonRuntimeLibPath},
		{Name: "VIRTUAL_ENV", Value: pythonVenvPath},
		{Name: "PYTHONUNBUFFERED", Value: "1"},
		{Name: "LANG", Value: "C.UTF-8"},
		{Name: "LC_ALL", Value: "C.UTF-8"},
	}
}

// buildSubstrateDeclarativeCommand returns the explicit command for a declarative ADK image.
// Substrate's atelet copies Command verbatim into the OCI spec's Process.Args with no fallback
// to the image entrypoint, so an empty command makes `runsc create` fail with
// "Spec.Process.Arg must be defined".
func buildSubstrateDeclarativeCommand(runtime v1alpha2.DeclarativeRuntime) []string {
	if runtime == v1alpha2.DeclarativeRuntime_Python {
		// The Python ADK `static` command reads config.json/agent-card.json from its
		// --filepath (default /config), which the materialization step populates from
		// the secret-backed env vars before the server starts.
		return []string{
			defaultPythonEntrypoint, "static",
			"--host", "0.0.0.0",
			"--port", fmt.Sprintf("%d", substrateKagentListenPort),
		}
	}
	return []string{
		defaultGoEntrypoint,
		"--host", "0.0.0.0",
		"--port", fmt.Sprintf("%d", substrateKagentListenPort),
	}
}

// sandboxAgentConfigSecretName returns the name of the Secret holding a SandboxAgent's rendered
// config for a given config hash. It mirrors the ActorTemplate name so the config Secret and the
// template that consumes it are paired per config. When the hash is empty (no config rendered) it
// falls back to the translator's per-agent Secret name.
func sandboxAgentConfigSecretName(sa *v1alpha2.SandboxAgent, configHash string) string {
	if configHash == "" {
		return sa.Name
	}
	return sandboxAgentActorTemplateName(sa, configHash)
}

func kagentAgentSecretEnv(secretName string) []corev1.EnvVar {
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

// sandboxAgentActorTemplateBaseName is the stable name prefix for a SandboxAgent's
// ActorTemplate(s), independent of config. Used as the truncation base for hashed names.
func sandboxAgentActorTemplateBaseName(sa *v1alpha2.SandboxAgent) string {
	return truncateDNS1123(sa.Name)
}

// sandboxAgentActorTemplateName is the generated ActorTemplate name for a SandboxAgent at a
// given config hash. The hash suffix makes each distinct config a distinct template (and
// golden). When the hash is empty (no config materialized) it falls back to the stable base
// name. Consumers must NOT assume this name — they resolve the live template via
// ResolveCurrentActorTemplate, since the hash depends on rendered config they don't have.
func sandboxAgentActorTemplateName(sa *v1alpha2.SandboxAgent, configHash string) string {
	if configHash == "" {
		return sandboxAgentActorTemplateBaseName(sa)
	}
	base := truncateDNS1123To(sa.Name, sandboxAgentTemplateNameMaxBase)
	return fmt.Sprintf("%s-%s", base, configHash)
}

// shortConfigHash converts the translator's decimal config-hash annotation into a short,
// DNS-1123-safe hex token (≤16 chars). Returns "" when the annotation is absent/unparseable.
func shortConfigHash(annotationValue string) string {
	v, err := strconv.ParseUint(strings.TrimSpace(annotationValue), 10, 64)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", v)
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
