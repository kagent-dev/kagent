package substrate

import (
	"crypto/sha256"
	"encoding/json"
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

	// sandboxAgentTemplateNameMaxBase reserves room in the 63-char DNS-1123 budget for
	// the "-<hash>" suffix (hash is up to 16 hex chars). A golden snapshot is an immutable
	// memory image, so a SHAPE change must produce a NEW ActorTemplate (substrate snapshots
	// once and no-ops in PhaseReady); folding the shape hash into the template name (and
	// mirroring it as an annotation) is what achieves that.
	sandboxAgentTemplateNameMaxBase = 46

	// shapeHashAnnotation carries the hash of the rendered ActorTemplateSpec ("shape"). The
	// session actor id is keyed on it: soft config changes ride Secret contents and never touch
	// the spec, so sessions keep their actor (and durable dir) across config rollouts; anything
	// that does change the spec (image digest, command, scopes, literal env) fans out blue-green
	// to a new template + new actors, resetting session state (accepted, plan §4.2).
	actorTemplateHashAnnotation = "kagent.dev/actor-template-hash"

	durableDataVolume = "data"
	durableDataMount  = "/data"
	// The session-store URL reaches the runtime as AgentConfig.session_db_url inside the
	// rendered config Secret (baked in by the translator via AgentsBackend.SessionDBURL).
	// The value is runtime-specific.
	// Python: google-adk's DatabaseSessionService uses SQLAlchemy's async engine, so the URL
	// must name an async driver (aiosqlite is a core google-adk dependency, present in every
	// runtime image). Go: the Go ADK's local store parses either form.
	sessionDBURLPython = "sqlite+aiosqlite:///" + durableDataMount + "/sessions.db"
	sessionDBURLGo     = "sqlite:///" + durableDataMount + "/sessions.db"
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
	secretName := sandboxAgentConfigSecretName(sa)
	command, containerEnv, err := buildSubstrateKagentContainerCommand(sa, kagentContainer, secretName)
	if err != nil {
		return nil, err
	}

	spec := atev1alpha1.ActorTemplateSpec{
		PauseImage:   p.Defaults.PauseImage,
		SandboxClass: atev1alpha1.SandboxClassGvisor,
		Containers: []atev1alpha1.Container{{
			Name:    defaultKagentContainer,
			Image:   image,
			Command: command,
			Env:     actorTemplateEnvFromPodEnv(append(containerEnv, kagentContainer.Env...)),
			// Gate actor readiness (the golden snapshot, and resume RPCs) on the app
			// actually serving A2A traffic, so a startup crash or wrong listen port
			// surfaces as Ready=False instead of a broken-chat "ready" agent. The
			// agent-card path mirrors the k8s Deployment readiness probe contract and
			// is served by both ADKs and any A2A-conformant BYO image; substrate's
			// default Readyz path (/readyz) is served by neither.
			Readyz: &atev1alpha1.ContainerReadyz{
				HTTPGet: &atev1alpha1.HTTPGetAction{
					Path: "/.well-known/agent-card.json",
					Port: substrateKagentListenPort,
				},
			},
		}},
		WorkerSelector: workerSelectorForPool(wpKey),
		SnapshotsConfig: atev1alpha1.SnapshotsConfig{
			Location: sandboxAgentSnapshotsLocation(sa),
			OnPause:  atev1alpha1.SnapshotScopeFull,
			OnCommit: atev1alpha1.SnapshotScopeFull,
		},
	}
	applyDurableDirSessionStore(&spec)

	actorTemplateHash, err := actorTemplateShapeHash(spec)
	if err != nil {
		return nil, err
	}

	annotations := map[string]string{
		// The agent generation at the time this template was (re)applied. It bumps only on a spec
		// change, so the template matching the agent's CURRENT desired config always carries the
		// highest generation — including on a flip-back to a retained older shape, where the
		// reused template is re-applied with the new generation. ResolveCurrentActorTemplate uses
		// this (not creationTimestamp) to pick the desired template, so chat/readiness follow the
		// current config rather than whichever golden was built most recently.
		desiredGenerationAnnotation: strconv.FormatInt(sa.GetGeneration(), 10),
		actorTemplateHashAnnotation: actorTemplateHash,
	}
	// The translator's config hash is kept as an informational annotation (it changes on soft
	// config rollouts while the template stays put; nothing keys on it anymore).
	if configHash := shortConfigHash(podTemplate.Annotations[consts.ConfigHashAnnotation]); configHash != "" {
		annotations[consts.ConfigHashAnnotation] = configHash
	}

	desired := &atev1alpha1.ActorTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:        sandboxAgentActorTemplateName(sa, actorTemplateHash),
			Namespace:   sa.Namespace,
			Labels:      sandboxAgentLifecycleLabels(sa),
			Annotations: annotations,
		},
		Spec: spec,
	}
	if err := controllerutil.SetControllerReference(sa, desired, p.Client.Scheme()); err != nil {
		return nil, fmt.Errorf("set ActorTemplate owner ref: %w", err)
	}
	return desired, nil
}

// actorTemplateShapeHash returns a short, deterministic hash of a rendered ActorTemplateSpec.
// It covers everything in the spec (image digest, command, env including secretKeyRef names,
// mounts, readyz, scopes, worker selector) and nothing outside it — Secret CONTENTS in
// particular are invisible, which is exactly what makes config rollouts soft.
func actorTemplateShapeHash(spec atev1alpha1.ActorTemplateSpec) (string, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("hash ActorTemplate spec: %w", err)
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum[:8]), nil
}

// applyDurableDirSessionStore mounts a durableDir volume at /data for the session sqlite DB
// and flips the suspend scope to Data: per-turn suspends then capture only the durable dir
// (KBs, not a full memory image), and every resume is a cold boot that re-resolves secretKeyRef
// env from the live Secret — the only config-refresh channel on resume, which is what lets soft
// config rollouts reach an existing session's actor (plan §4.2/§4.3). onPause stays Full so the
// golden build and any future warm-pause tier keep a full snapshot available.
func applyDurableDirSessionStore(spec *atev1alpha1.ActorTemplateSpec) {
	spec.Volumes = append(spec.Volumes, atev1alpha1.Volume{
		Name:         durableDataVolume,
		VolumeSource: atev1alpha1.VolumeSource{DurableDir: &atev1alpha1.DurableDirVolumeSource{}},
	})
	c := &spec.Containers[0]
	c.VolumeMounts = append(c.VolumeMounts, atev1alpha1.VolumeMount{
		Name:      durableDataVolume,
		MountPath: durableDataMount,
	})
	spec.SnapshotsConfig.OnCommit = atev1alpha1.SnapshotScopeData
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
		// Substrate checkpoints the actor as soon as the response body closes,
		// so the ADKs' opt-in pre-response span flush is the only reliable
		// export window; the batch exporter's timer never fires in the ~1s an
		// actor lives past its response. Ordinary Deployments leave this off.
		{Name: "KAGENT_PRE_RESPONSE_TRACE_FLUSH", Value: "true"},
	}

	spec := sa.GetAgentSpec()
	if spec != nil && spec.Type == v1alpha2.AgentType_BYO {
		// BYO: use the explicit container command + args verbatim. Validation
		// (ValidateSubstrateSandboxAgentSpec) guarantees a command is set. The rendered
		// (minimal) config is exposed the same way as for declaratives — secret-backed env —
		// so kagent-owned settings like session_db_url are available to the image; whether
		// the image consumes them is its own concern.
		if len(container.Command) == 0 {
			return nil, nil, fmt.Errorf("BYO substrate agent %q is missing an explicit container command", sa.Name)
		}
		cmd := append([]string{}, container.Command...)
		cmd = append(cmd, container.Args...)
		env = append(env, kagentAgentSecretEnv(configSecretName)...)
		return cmd, env, nil
	}

	// Declarative: secret-backed config is materialized at startup from the per-config-hash Secret.
	env = append(env, kagentAgentSecretEnv(configSecretName)...)
	runtime := v1alpha2.EffectiveDeclarativeRuntime(sa.GetAgentSpec())
	return buildSubstrateDeclarativeCommand(runtime), env, nil
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

// sandboxAgentConfigSecretName returns the agent's STABLE config Secret name — the agent name,
// matching the Secret the translator renders and emits in BuildManifest.
func sandboxAgentConfigSecretName(sa *v1alpha2.SandboxAgent) string {
	return sa.Name
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
// given shape hash. The hash suffix makes each distinct shape a distinct template (and golden).
// When the hash is empty it falls back to the stable base name. Consumers must NOT assume this
// name — they resolve the live template via ResolveCurrentActorTemplate.
func sandboxAgentActorTemplateName(sa *v1alpha2.SandboxAgent, shapeHash string) string {
	if shapeHash == "" {
		return sandboxAgentActorTemplateBaseName(sa)
	}
	base := truncateDNS1123To(sa.Name, sandboxAgentTemplateNameMaxBase)
	return fmt.Sprintf("%s-%s", base, shapeHash)
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
