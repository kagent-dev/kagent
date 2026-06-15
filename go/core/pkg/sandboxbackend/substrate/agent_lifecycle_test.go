package substrate

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
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

func TestBuildSubstrateGoKagentCommand(t *testing.T) {
	t.Parallel()

	// Substrate's atelet copies Command verbatim into the OCI Process.Args with
	// no image-entrypoint fallback, so the declarative Go command must be explicit.
	require.Equal(t, []string{"/app", "--host", "0.0.0.0", "--port", "80"}, buildSubstrateGoKagentCommand())
}

func TestBuildSubstrateKagentContainerCommand(t *testing.T) {
	t.Parallel()

	sa := &v1alpha2.SandboxAgent{
		Spec: v1alpha2.SandboxAgentSpec{
			AgentSpec: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					Runtime: v1alpha2.DeclarativeRuntime_Go,
				},
			},
		},
	}
	sa.Name = "my-agent"
	sa.Namespace = "kagent"
	cmd, env := buildSubstrateKagentContainerCommand(sa)
	require.Equal(t, []string{"/app", "--host", "0.0.0.0", "--port", "80"}, cmd)
	require.NotEmpty(t, env)

	// KAGENT_NAME / KAGENT_NAMESPACE must be literal values so the Go ADK can
	// derive the correct app name (fieldRef env vars are dropped on Substrate).
	envByName := map[string]string{}
	for _, e := range env {
		envByName[e.Name] = e.Value
	}
	require.Equal(t, "my-agent", envByName["KAGENT_NAME"])
	require.Equal(t, "kagent", envByName["KAGENT_NAMESPACE"])
}
