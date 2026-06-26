package substrate

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBuildOpenClawActorStartup_WithModelConfig(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha2.AddToScheme(scheme))

	ns := "kagent"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "openai-key", Namespace: ns},
		Data:       map[string][]byte{"OPENAI_API_KEY": []byte("sk-test")},
	}
	mc := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "default-model-config", Namespace: ns},
		Spec: v1alpha2.ModelConfigSpec{
			Model:           "gpt-4o",
			Provider:        v1alpha2.ModelProviderOpenAI,
			APIKeySecret:    "openai-key",
			APIKeySecretKey: "OPENAI_API_KEY",
			OpenAI:          &v1alpha2.OpenAIConfig{},
		},
	}
	ah := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "peterj-claw", Namespace: ns},
		Spec: v1alpha2.AgentHarnessSpec{
			ModelConfigRef: "default-model-config",
			Substrate: &v1alpha2.AgentHarnessSubstrateSpec{
				SnapshotsConfig: &v1alpha2.AgentHarnessSubstrateSnapshotsConfig{
					Location: "gs://bucket/prefix/",
				},
			},
		},
	}

	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, mc).Build()
	p := &Lifecycle{
		Client: kube,
	}

	script, env, err := p.buildOpenClawActorStartup(context.Background(), ah)
	require.NoError(t, err)
	require.Contains(t, script, "base64 -d")
	require.Contains(t, script, "openclaw-gateway-ensure.sh")
	require.Contains(t, script, "exec /usr/local/bin/acp-shim")
	require.Contains(t, script, "--listen :80")
	require.NotContains(t, script, "--passthrough")

	var foundKey bool
	for _, e := range env {
		if e.Name != "OPENAI_API_KEY" {
			continue
		}
		require.NotNil(t, e.ValueFrom)
		require.NotNil(t, e.ValueFrom.SecretKeyRef)
		require.Equal(t, "openai-key", e.ValueFrom.SecretKeyRef.Name)
		require.Equal(t, "OPENAI_API_KEY", e.ValueFrom.SecretKeyRef.Key)
		require.Empty(t, e.Value, "API key must not be inlined in ActorTemplate env")
		foundKey = true
	}
	require.True(t, foundKey, "expected OPENAI_API_KEY secretKeyRef in container env")

	var foundGatewayPort bool
	for _, e := range env {
		switch e.Name {
		case "OPENCLAW_GATEWAY_PORT":
			require.NotNil(t, e.Value)
			require.Equal(t, "18789", *e.Value)
			foundGatewayPort = true
		}
	}
	require.True(t, foundGatewayPort, "expected OPENCLAW_GATEWAY_PORT in container env")

	// Decode embedded JSON from the base64 line in the startup script.
	var payload string
	for line := range strings.SplitSeq(script, "\n") {
		if !strings.Contains(line, "base64 -d") {
			continue
		}
		start := strings.Index(line, `'`) + 1
		end := strings.LastIndex(line, `'`)
		require.Greater(t, end, start)
		payload = line[start:end]
		break
	}
	require.NotEmpty(t, payload)
	raw, decErr := base64.StdEncoding.DecodeString(payload)
	require.NoError(t, decErr)
	var root map[string]any
	require.NoError(t, json.Unmarshal(raw, &root))
	gw := root["gateway"].(map[string]any)
	require.Equal(t, "loopback", gw["bind"])
	require.Equal(t, float64(18789), gw["port"])
	auth := gw["auth"].(map[string]any)
	require.Equal(t, "none", auth["mode"])
	_, hasControlUI := gw["controlUi"]
	require.False(t, hasControlUI, "controlUi should not be emitted")
	_, hasModels := root["models"]
	require.False(t, hasModels, "substrate bootstrap should omit models unless ModelConfig sets an explicit baseUrl")
	require.Contains(t, root, "agents")
}

func TestBuildOpenClawActorStartup_WithExplicitBaseURL(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha2.AddToScheme(scheme))

	ns := "kagent"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "openai-key", Namespace: ns},
		Data:       map[string][]byte{"OPENAI_API_KEY": []byte("sk-test")},
	}
	mc := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "mc", Namespace: ns},
		Spec: v1alpha2.ModelConfigSpec{
			Model:           "gpt-4o",
			Provider:        v1alpha2.ModelProviderOpenAI,
			APIKeySecret:    "openai-key",
			APIKeySecretKey: "OPENAI_API_KEY",
			OpenAI:          &v1alpha2.OpenAIConfig{BaseURL: "https://api.example/v1"},
		},
	}
	ah := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "claw", Namespace: ns},
		Spec: v1alpha2.AgentHarnessSpec{
			ModelConfigRef: "mc",
			Substrate: &v1alpha2.AgentHarnessSubstrateSpec{
				SnapshotsConfig: &v1alpha2.AgentHarnessSubstrateSnapshotsConfig{
					Location: "gs://bucket/prefix/",
				},
			},
		},
	}

	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, mc).Build()
	p := &Lifecycle{Client: kube, Defaults: LifecycleDefaults{}}
	script, _, err := p.buildOpenClawActorStartup(context.Background(), ah)
	require.NoError(t, err)

	var payload string
	for line := range strings.SplitSeq(script, "\n") {
		if strings.Contains(line, "base64 -d") {
			start := strings.Index(line, `'`) + 1
			end := strings.LastIndex(line, `'`)
			payload = line[start:end]
			break
		}
	}
	raw, decErr := base64.StdEncoding.DecodeString(payload)
	require.NoError(t, decErr)
	var root map[string]any
	require.NoError(t, json.Unmarshal(raw, &root))
	openai := root["models"].(map[string]any)["providers"].(map[string]any)["openai"].(map[string]any)
	require.Equal(t, "https://api.example/v1", openai["baseUrl"])
}
