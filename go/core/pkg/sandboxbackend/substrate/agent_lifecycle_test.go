package substrate

import (
	"fmt"
	"k8s.io/apimachinery/pkg/util/intstr"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestActorTemplateEnvFromPodEnv(t *testing.T) {
	t.Parallel()

	env := []corev1.EnvVar{
		{Name: "LITERAL", Value: "ok"},
		{
			Name: "KAGENT_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			},
		},
		{
			Name: "API_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "secret"},
					Key:                  "key",
				},
			},
		},
		{
			Name: "UNSUPPORTED",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
			},
		},
	}

	got := actorTemplateEnvFromPodEnv(env)
	require.Len(t, got, 2)
	require.NotNil(t, got[0].Value)
	require.Equal(t, "ok", *got[0].Value)
	require.NotNil(t, got[1].ValueFrom.SecretKeyRef)
}

func TestBuildSubstrateDeclarativeCommand(t *testing.T) {
	t.Parallel()

	// Substrate's atelet copies Command verbatim into the OCI Process.Args with
	// no image-entrypoint fallback, so the declarative command must be explicit.
	require.Equal(t,
		[]string{"/app", "--host", "0.0.0.0", "--port", "80"},
		buildSubstrateDeclarativeCommand(v1alpha2.DeclarativeRuntime_Go),
	)
	require.Equal(t,
		[]string{"/.kagent/.venv/bin/kagent-adk", "static", "--host", "0.0.0.0", "--port", "80"},
		buildSubstrateDeclarativeCommand(v1alpha2.DeclarativeRuntime_Python),
	)
}

func declarativeSandboxAgent(runtime v1alpha2.DeclarativeRuntime) *v1alpha2.SandboxAgent {
	sa := &v1alpha2.SandboxAgent{
		Spec: v1alpha2.SandboxAgentSpec{
			AgentSpec: v1alpha2.AgentSpec{
				Type:        v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{Runtime: runtime},
			},
		},
	}
	sa.Name = "my-agent"
	sa.Namespace = "kagent"
	return sa
}

func TestBuildSubstrateKagentContainerCommandDeclarative(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		runtime v1alpha2.DeclarativeRuntime
		wantCmd []string
	}{
		{"go", v1alpha2.DeclarativeRuntime_Go, []string{"/app", "--host", "0.0.0.0", "--port", "80"}},
		{"python", v1alpha2.DeclarativeRuntime_Python, []string{"/.kagent/.venv/bin/kagent-adk", "static", "--host", "0.0.0.0", "--port", "80"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sa := declarativeSandboxAgent(tc.runtime)
			cmd, env, err := buildSubstrateKagentContainerCommand(sa, &corev1.Container{}, "my-agent-abc123")
			require.NoError(t, err)
			require.Equal(t, tc.wantCmd, cmd)

			// KAGENT_NAME / KAGENT_NAMESPACE must be literal values so the ADK can
			// derive the correct app name (fieldRef env vars are dropped on Substrate).
			envByName := map[string]string{}
			for _, e := range env {
				envByName[e.Name] = e.Value
			}
			require.Equal(t, "my-agent", envByName["KAGENT_NAME"])
			require.Equal(t, "kagent", envByName["KAGENT_NAMESPACE"])

			// Config env must reference the per-config-hash Secret (so a golden materializes its
			// own config), not the shared per-agent Secret.
			for _, e := range env {
				if e.Name == "KAGENT_CONFIG_JSON" {
					require.NotNil(t, e.ValueFrom)
					require.NotNil(t, e.ValueFrom.SecretKeyRef)
					require.Equal(t, "my-agent-abc123", e.ValueFrom.SecretKeyRef.Name)
				}
			}

			// Substrate v0.0.9's atelet applies the image's ENV directives, so kagent no
			// longer re-supplies LD_LIBRARY_PATH/PATH; neither runtime carries it.
			_, ok := envByName["LD_LIBRARY_PATH"]
			require.False(t, ok, "kagent must not re-supply the image runtime ENV")
		})
	}
}

func TestBuildSubstrateKagentContainerCommandBYO(t *testing.T) {
	t.Parallel()

	cmd := "/serve"
	sa := &v1alpha2.SandboxAgent{
		Spec: v1alpha2.SandboxAgentSpec{
			AgentSpec: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_BYO,
				BYO:  &v1alpha2.BYOAgentSpec{Deployment: &v1alpha2.ByoDeploymentSpec{Image: "example/agent:latest", Cmd: &cmd}},
			},
		},
	}
	sa.Name = "byo-agent"
	sa.Namespace = "kagent"

	container := &corev1.Container{Command: []string{"/serve"}, Args: []string{"--host", "0.0.0.0", "--port", "80"}}
	got, env, err := buildSubstrateKagentContainerCommand(sa, container, "byo-agent")
	require.NoError(t, err)
	// BYO uses the container command + args verbatim.
	require.Equal(t, []string{"/serve", "--host", "0.0.0.0", "--port", "80"}, got)
	// BYO also receives the rendered (minimal) config through the same secret-backed env as
	// declaratives; whether the image consumes it is its own concern.
	var hasConfigEnv bool
	for _, e := range env {
		if e.Name == "KAGENT_CONFIG_JSON" {
			hasConfigEnv = true
			require.NotNil(t, e.ValueFrom)
			require.Equal(t, "byo-agent", e.ValueFrom.SecretKeyRef.Name)
		}
	}
	require.True(t, hasConfigEnv)

	// A BYO agent missing an explicit command is rejected.
	_, _, err = buildSubstrateKagentContainerCommand(sa, &corev1.Container{}, "byo-agent")
	require.Error(t, err)
}

func newTestLifecycle(t *testing.T) *Lifecycle {
	t.Helper()
	scheme := runtime.NewScheme()
	utilruntime.Must(v1alpha2.AddToScheme(scheme))
	utilruntime.Must(atev1alpha1.AddToScheme(scheme))
	return &Lifecycle{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		Defaults: LifecycleDefaults{
			PauseImage: "gcr.io/test/pause@sha256:deadbeef",
		},
	}
}

// envByName flattens an ActorTemplate env list into name->present for assertions.
func actorEnvNames(env []atev1alpha1.EnvVar) map[string]bool {
	out := map[string]bool{}
	for _, e := range env {
		out[e.Name] = true
	}
	return out
}

// TestBuildSandboxAgentActorTemplate exercises the full ActorTemplate generation for each
// supported runtime/type on substrate (Go declarative, Python declarative, BYO), asserting the
// pinned image, the explicit command, and the env wiring side by side.
func TestBuildSandboxAgentActorTemplate(t *testing.T) {
	t.Parallel()

	const pinnedImage = "registry.example/kagent-dev/kagent/app@sha256:1111111111111111111111111111111111111111111111111111111111111111"
	cmd := "/serve"
	wpKey := types.NamespacedName{Namespace: "kagent", Name: "kagent-default"}

	podTemplateFor := func(container corev1.Container) corev1.PodTemplateSpec {
		container.Name = defaultKagentContainer
		container.Image = pinnedImage
		return corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{container}}}
	}

	for _, tc := range []struct {
		name        string
		sa          *v1alpha2.SandboxAgent
		container   corev1.Container
		wantCommand []string
	}{
		{
			name: "go declarative",
			sa: &v1alpha2.SandboxAgent{
				ObjectMeta: metav1.ObjectMeta{Name: "go-agent", Namespace: "kagent"},
				Spec: v1alpha2.SandboxAgentSpec{
					AgentSpec: v1alpha2.AgentSpec{Type: v1alpha2.AgentType_Declarative, Declarative: &v1alpha2.DeclarativeAgentSpec{Runtime: v1alpha2.DeclarativeRuntime_Go}},
				},
			},
			container:   corev1.Container{Args: []string{"--host", "0.0.0.0", "--port", "8080", "--filepath", "/config"}},
			wantCommand: []string{"/app", "--host", "0.0.0.0", "--port", "80"}},
		{
			name: "python declarative",
			sa: &v1alpha2.SandboxAgent{
				ObjectMeta: metav1.ObjectMeta{Name: "py-agent", Namespace: "kagent"},
				Spec: v1alpha2.SandboxAgentSpec{
					AgentSpec: v1alpha2.AgentSpec{Type: v1alpha2.AgentType_Declarative, Declarative: &v1alpha2.DeclarativeAgentSpec{Runtime: v1alpha2.DeclarativeRuntime_Python}},
				},
			},
			container:   corev1.Container{Args: []string{"--host", "0.0.0.0", "--port", "8080", "--filepath", "/config"}},
			wantCommand: []string{"/.kagent/.venv/bin/kagent-adk", "static", "--host", "0.0.0.0", "--port", "80"}},
		{
			name: "byo",
			sa: &v1alpha2.SandboxAgent{
				ObjectMeta: metav1.ObjectMeta{Name: "byo-agent", Namespace: "kagent"},
				Spec: v1alpha2.SandboxAgentSpec{
					AgentSpec: v1alpha2.AgentSpec{Type: v1alpha2.AgentType_BYO, BYO: &v1alpha2.BYOAgentSpec{Deployment: &v1alpha2.ByoDeploymentSpec{Image: pinnedImage, Cmd: &cmd}}},
				},
			},
			container:   corev1.Container{Command: []string{"/serve"}, Args: []string{"--host", "0.0.0.0", "--port", "80"}},
			wantCommand: []string{"/serve", "--host", "0.0.0.0", "--port", "80"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := newTestLifecycle(t)
			tmpl, err := p.buildSandboxAgentActorTemplate(tc.sa, wpKey, podTemplateFor(tc.container))
			require.NoError(t, err)

			require.Len(t, tmpl.Spec.Containers, 1)
			c := tmpl.Spec.Containers[0]
			require.Equal(t, pinnedImage, c.Image, "ActorTemplate must use the digest-pinned image")
			require.Equal(t, tc.wantCommand, c.Command)
			require.Equal(t, wpKey.Name, tmpl.Spec.WorkerSelector.MatchLabels["kagent.dev/worker-pool"])

			require.NotNil(t, c.Readyz, "actor readiness must be gated on the app serving traffic")
			require.NotNil(t, c.Readyz.HTTPGet)
			require.Equal(t, "/.well-known/agent-card.json", c.Readyz.HTTPGet.Path)
			require.Equal(t, substrateKagentListenPort, c.Readyz.HTTPGet.Port)

			names := actorEnvNames(c.Env)
			require.True(t, names["KAGENT_NAME"], "KAGENT_NAME must be a literal env var")
			require.True(t, names["KAGENT_NAMESPACE"], "KAGENT_NAMESPACE must be a literal env var")
			require.True(t, names["KAGENT_CONFIG_JSON"], "every agent type gets the rendered config via secret env (BYO decides for itself whether to consume it)")

			// Durable-dir session storage is on for every sandbox agent, BYO included (asserted
			// in detail in TestBuildSandboxAgentActorTemplateDurableDirSessions). The DB URL
			// travels only as AgentConfig.session_db_url in the config Secret.
			require.Len(t, tmpl.Spec.Volumes, 1)
			require.False(t, names["KAGENT_SESSION_DB_URL"], "the session DB URL must never be a template env var")
		})
	}
}

// TestBuildSandboxAgentActorTemplateDurableDirSessions covers the durable-dir session-store
// wiring: always on for every sandbox agent, BYO included (the image contract — state under
// /data — is documented on applyDurableDirSessionStore). The store URL travels
// ONLY as AgentConfig.session_db_url in the rendered config Secret — never as a template env
// var (asserted in TestBuildSandboxAgentConfigSecretSessionDBURL).
func TestBuildSandboxAgentActorTemplateDurableDirSessions(t *testing.T) {
	t.Parallel()

	const pinnedImage = "registry.example/kagent-dev/kagent/app@sha256:1111111111111111111111111111111111111111111111111111111111111111"
	cmd := "/serve"
	wpKey := types.NamespacedName{Namespace: "kagent", Name: "kagent-default"}

	agentFor := func(spec v1alpha2.AgentSpec, annotations map[string]string) *v1alpha2.SandboxAgent {
		return &v1alpha2.SandboxAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "my-agent", Namespace: "kagent", Annotations: annotations},
			Spec:       v1alpha2.SandboxAgentSpec{AgentSpec: spec},
		}
	}
	pythonSpec := v1alpha2.AgentSpec{Type: v1alpha2.AgentType_Declarative, Declarative: &v1alpha2.DeclarativeAgentSpec{Runtime: v1alpha2.DeclarativeRuntime_Python}}
	goSpec := v1alpha2.AgentSpec{Type: v1alpha2.AgentType_Declarative, Declarative: &v1alpha2.DeclarativeAgentSpec{Runtime: v1alpha2.DeclarativeRuntime_Go}}
	byoSpec := v1alpha2.AgentSpec{Type: v1alpha2.AgentType_BYO, BYO: &v1alpha2.BYOAgentSpec{Deployment: &v1alpha2.ByoDeploymentSpec{Image: pinnedImage, Cmd: &cmd}}}
	for _, tc := range []struct {
		name      string
		sa        *v1alpha2.SandboxAgent
		container corev1.Container
	}{
		{name: "python", sa: agentFor(pythonSpec, nil)},
		{name: "python with unrelated annotations", sa: agentFor(pythonSpec, map[string]string{"kagent.dev/other": "x"})},
		{name: "go", sa: agentFor(goSpec, nil)},
		{name: "byo", sa: agentFor(byoSpec, nil), container: corev1.Container{Command: []string{"/serve"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := newTestLifecycle(t)
			container := tc.container
			container.Name = defaultKagentContainer
			container.Image = pinnedImage
			podTemplate := corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{container}}}

			tmpl, err := p.buildSandboxAgentActorTemplate(tc.sa, wpKey, podTemplate)
			require.NoError(t, err)
			require.Len(t, tmpl.Spec.Containers, 1)
			c := tmpl.Spec.Containers[0]

			// The store URL never rides the template: it lives in the config Secret.
			require.False(t, actorEnvNames(c.Env)["KAGENT_SESSION_DB_URL"])

			require.Len(t, tmpl.Spec.Volumes, 1)
			require.Equal(t, durableDataVolume, tmpl.Spec.Volumes[0].Name)
			require.NotNil(t, tmpl.Spec.Volumes[0].DurableDir)
			require.Equal(t, []atev1alpha1.VolumeMount{{Name: durableDataVolume, MountPath: durableDataMount}}, c.VolumeMounts)
			require.NotNil(t, c.Readyz)
			require.Equal(t, "/.well-known/agent-card.json", c.Readyz.HTTPGet.Path)
			require.Equal(t, substrateKagentListenPort, c.Readyz.HTTPGet.Port)
			// Durable-dir sessions suspend with Data scope (cheap per-turn snapshots + config
			// refresh on resume); pause keeps Full for the golden build.
			require.Equal(t, atev1alpha1.SnapshotScopeData, tmpl.Spec.SnapshotsConfig.OnCommit)
			require.Equal(t, atev1alpha1.SnapshotScopeFull, tmpl.Spec.SnapshotsConfig.OnPause)
		})
	}
}

// byoSandboxAgentWithSidecar builds a BYO SandboxAgent whose translated pod template carries a
// credential-brokering sidecar next to the kagent container, the shape byo.deployment
// .extraContainers produces.
func byoSandboxAgentWithSidecar(extra corev1.Container) (*v1alpha2.SandboxAgent, corev1.PodTemplateSpec) {
	const pinnedImage = "registry.example/kagent-dev/kagent/app@sha256:1111111111111111111111111111111111111111111111111111111111111111111"
	cmd := "/serve"
	sa := &v1alpha2.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "byo-sidecar", Namespace: "kagent"},
		Spec: v1alpha2.SandboxAgentSpec{
			AgentSpec: v1alpha2.AgentSpec{Type: v1alpha2.AgentType_BYO, BYO: &v1alpha2.BYOAgentSpec{
				Deployment: &v1alpha2.ByoDeploymentSpec{Image: pinnedImage, Cmd: &cmd},
			}},
		},
	}
	podTemplate := corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{
		{Name: defaultKagentContainer, Image: pinnedImage, Command: []string{"/serve"}, Env: []corev1.EnvVar{{Name: "ADDR", Value: "0.0.0.0:80"}}},
		extra,
	}}}
	return sa, podTemplate
}

func TestBuildSandboxAgentActorTemplateExtraContainers(t *testing.T) {
	t.Parallel()
	const sidecarImage = "registry.example/videoamp/central@sha256:2222222222222222222222222222222222222222222222222222222222222222"
	wpKey := types.NamespacedName{Namespace: "kagent", Name: "kagent-default"}

	sidecar := corev1.Container{
		Name:    "agent-vault",
		Image:   sidecarImage,
		Command: []string{"/sidecar"},
		Env: []corev1.EnvVar{
			{Name: "AGENT_VAULT_LISTEN_ADDR", Value: "127.0.0.1:14322"},
			{Name: "AGENT_VAULT_SECRET_ANTHROPIC_AUTH_TOKEN", ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "vault-secrets"}, Key: "anthropic-token"},
			}},
			{Name: "DROPPED_CONFIGMAP", ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "cm"}, Key: "k"},
			}},
		},
		ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromInt32(8080)}}},
	}

	t.Run("renders alongside the agent", func(t *testing.T) {
		t.Parallel()
		p := newTestLifecycle(t)
		sa, podTemplate := byoSandboxAgentWithSidecar(sidecar)
		tmpl, err := p.buildSandboxAgentActorTemplate(sa, wpKey, podTemplate)
		require.NoError(t, err)
		require.Len(t, tmpl.Spec.Containers, 2)

		agent := tmpl.Spec.Containers[0]
		require.Equal(t, defaultKagentContainer, agent.Name, "the agent stays container 0: /data is mounted there")
		require.Equal(t, durableDataMount, agent.VolumeMounts[0].MountPath)
		require.Equal(t, "/.well-known/agent-card.json", agent.Readyz.HTTPGet.Path)

		sc := tmpl.Spec.Containers[1]
		require.Equal(t, "agent-vault", sc.Name)
		require.Equal(t, sidecarImage, sc.Image, "extra images must stay digest-pinned")
		require.Equal(t, []string{"/sidecar"}, sc.Command)
		require.NotNil(t, sc.Readyz, "an HTTP readiness probe maps to readyz so actor readiness gates on the sidecar")
		require.Equal(t, "/readyz", sc.Readyz.HTTPGet.Path)
		require.Equal(t, int32(8080), sc.Readyz.HTTPGet.Port)
		names := actorEnvNames(sc.Env)
		require.True(t, names["AGENT_VAULT_LISTEN_ADDR"])
		require.True(t, names["AGENT_VAULT_SECRET_ANTHROPIC_AUTH_TOKEN"], "secretKeyRef env survives (substrate resolves it server-side)")
		require.False(t, names["DROPPED_CONFIGMAP"], "configMapKeyRef is not expressible in an ActorTemplate and is dropped")
		require.Empty(t, sc.VolumeMounts, "extra containers get no volume mounts: durableDir belongs to the agent's /data")

		// The sidecar changes the shape hash, so it fans out blue-green like any spec change.
		p2 := newTestLifecycle(t)
		sa2, bare := byoSandboxAgentWithSidecar(corev1.Container{})
		bare.Spec.Containers = bare.Spec.Containers[:1]
		tmpl2, err := p2.buildSandboxAgentActorTemplate(sa2, wpKey, bare)
		require.NoError(t, err)
		require.NotEqual(t, tmpl.Name, tmpl2.Name, "extra containers must change the ActorTemplate name (shape hash)")
	})

	t.Run("named readiness port resolves against the container ports", func(t *testing.T) {
		t.Parallel()
		p := newTestLifecycle(t)
		named := sidecar
		named.Ports = []corev1.ContainerPort{{Name: "health", ContainerPort: 8081}}
		named.ReadinessProbe.HTTPGet.Port = intstr.FromString("health")
		sa, podTemplate := byoSandboxAgentWithSidecar(named)
		tmpl, err := p.buildSandboxAgentActorTemplate(sa, wpKey, podTemplate)
		require.NoError(t, err)
		require.Equal(t, int32(8081), tmpl.Spec.Containers[1].Readyz.HTTPGet.Port)
	})

	for _, tc := range []struct {
		name  string
		extra corev1.Container
	}{
		{
			name:  "unpinned image is rejected",
			extra: corev1.Container{Name: "agent-vault", Image: "registry.example/videoamp/central:latest", Command: []string{"/sidecar"}},
		},
		{
			name:  "missing command is rejected",
			extra: corev1.Container{Name: "agent-vault", Image: sidecarImage},
		},
		{
			name: "volume mounts are rejected",
			extra: corev1.Container{Name: "agent-vault", Image: sidecarImage, Command: []string{"/sidecar"},
				VolumeMounts: []corev1.VolumeMount{{Name: "secrets", MountPath: "/etc/agent-vault/secrets"}}},
		},
		{
			name: "unresolvable named readiness port is rejected",
			extra: corev1.Container{Name: "agent-vault", Image: sidecarImage, Command: []string{"/sidecar"},
				ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromString("nope")}}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := newTestLifecycle(t)
			sa, podTemplate := byoSandboxAgentWithSidecar(tc.extra)
			_, err := p.buildSandboxAgentActorTemplate(sa, wpKey, podTemplate)
			require.Error(t, err)
		})
	}

	t.Run("more than 10 containers is rejected", func(t *testing.T) {
		t.Parallel()
		p := newTestLifecycle(t)
		sa, podTemplate := byoSandboxAgentWithSidecar(sidecar)
		for i := 0; i < maxActorTemplateContainers; i++ {
			podTemplate.Spec.Containers = append(podTemplate.Spec.Containers, corev1.Container{
				Name: fmt.Sprintf("extra-%d", i), Image: sidecarImage, Command: []string{"/bin/true"},
			})
		}
		_, err := p.buildSandboxAgentActorTemplate(sa, wpKey, podTemplate)
		require.Error(t, err)
	})
}
