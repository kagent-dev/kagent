package substrate

import (
	"encoding/json"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestBuildSandboxAgentConfigSecretSessionDBURL covers the config-struct channel for durable-dir
// sessions: declarative agents get AgentConfig.session_db_url injected into the rendered
// config.json (runtime-specific dialect), other fields survive the round-trip untouched.
func TestBuildSandboxAgentConfigSecretSessionDBURL(t *testing.T) {
	t.Parallel()

	agentFor := func(runtime v1alpha2.DeclarativeRuntime) *v1alpha2.SandboxAgent {
		return &v1alpha2.SandboxAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "my-agent", Namespace: "kagent"},
			Spec: v1alpha2.SandboxAgentSpec{AgentSpec: v1alpha2.AgentSpec{
				Type:        v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{Runtime: runtime},
			}},
		}
	}
	renderedConfig := []byte(`{"model":{"type":"gemini","model":"gemini-2.5-flash"},"description":"d","instruction":"i"}`)
	inputFor := func(config []byte) sandboxbackend.BuildInput {
		return sandboxbackend.BuildInput{ConfigSecret: &corev1.Secret{
			Data: map[string][]byte{"config.json": config, "agent-card.json": []byte(`{}`)},
		}}
	}

	for _, tc := range []struct {
		name    string
		runtime v1alpha2.DeclarativeRuntime
		wantURL string
	}{
		{"python", v1alpha2.DeclarativeRuntime_Python, "sqlite+aiosqlite:////data/sessions.db"},
		{"go", v1alpha2.DeclarativeRuntime_Go, "sqlite:////data/sessions.db"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			secret, err := buildSandboxAgentConfigSecret(agentFor(tc.runtime), inputFor(renderedConfig))
			require.NoError(t, err)
			require.NotNil(t, secret)
			require.Equal(t, "my-agent", secret.Name, "config Secret must use the agent's stable name")

			var cfg map[string]any
			require.NoError(t, json.Unmarshal(secret.Data["config.json"], &cfg))
			require.Equal(t, tc.wantURL, cfg["session_db_url"])
			// The rest of the rendered config must survive the round-trip.
			require.Equal(t, "i", cfg["instruction"])
			require.Equal(t, map[string]any{"type": "gemini", "model": "gemini-2.5-flash"}, cfg["model"])
			require.Equal(t, []byte(`{}`), secret.Data["agent-card.json"])
		})
	}

	t.Run("byo opt-in gets the url in its minimal config", func(t *testing.T) {
		t.Parallel()
		cmd := "/serve"
		sa := &v1alpha2.SandboxAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name: "my-agent", Namespace: "kagent",
				Annotations: map[string]string{"kagent.dev/local-session-storage": "true"},
			},
			Spec: v1alpha2.SandboxAgentSpec{AgentSpec: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_BYO,
				BYO:  &v1alpha2.BYOAgentSpec{Deployment: &v1alpha2.ByoDeploymentSpec{Image: "example/agent:latest", Cmd: &cmd}},
			}},
		}
		secret, err := buildSandboxAgentConfigSecret(sa, inputFor([]byte(`{"model":null,"description":"","instruction":""}`)))
		require.NoError(t, err)
		require.NotNil(t, secret)
		var cfg map[string]any
		require.NoError(t, json.Unmarshal(secret.Data["config.json"], &cfg))
		require.Equal(t, "sqlite+aiosqlite:////data/sessions.db", cfg["session_db_url"])
	})

	t.Run("malformed rendered config fails loud", func(t *testing.T) {
		t.Parallel()
		_, err := buildSandboxAgentConfigSecret(agentFor(v1alpha2.DeclarativeRuntime_Python), inputFor([]byte("{not json")))
		require.Error(t, err)
	})

	t.Run("no rendered config yields no secret", func(t *testing.T) {
		t.Parallel()
		secret, err := buildSandboxAgentConfigSecret(agentFor(v1alpha2.DeclarativeRuntime_Python), sandboxbackend.BuildInput{})
		require.NoError(t, err)
		require.Nil(t, secret)
	})
}
