package openshell

import (
	"context"
	"encoding/json"
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

func TestBuildOpenClawBootstrapJSON_OpenAIDefaultBaseURLInferenceLocal(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha2.AddToScheme(scheme))

	ns := "default"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "openai-key", Namespace: ns},
		Data:       map[string][]byte{"OPENAI_API_KEY": []byte("sk-test")},
	}
	mc := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "mc1", Namespace: ns},
		Spec: v1alpha2.ModelConfigSpec{
			Model:           "gpt-4o",
			Provider:        v1alpha2.ModelProviderOpenAI,
			APIKeySecret:    "openai-key",
			APIKeySecretKey: "OPENAI_API_KEY",
			OpenAI:          &v1alpha2.OpenAIConfig{},
		},
	}
	sbx := &v1alpha2.Sandbox{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: ns}}

	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, mc).Build()
	raw, _, err := buildOpenClawBootstrapJSON(context.Background(), kube, ns, sbx, mc, 18800)
	require.NoError(t, err)

	var root map[string]any
	require.NoError(t, json.Unmarshal(raw, &root))
	models := root["models"].(map[string]any)
	provs := models["providers"].(map[string]any)
	openai := provs["openai"].(map[string]any)
	require.Equal(t, "https://inference.local/v1", openai["baseUrl"])
	require.Equal(t, "openshell:resolve:env:OPENAI_API_KEY", openai["apiKey"])
	secRoot := root["secrets"].(map[string]any)
	secProvs := secRoot["providers"].(map[string]any)
	kagent := secProvs["kagent"].(map[string]any)
	require.Equal(t, "env", kagent["source"])
	require.Contains(t, kagent["allowlist"], "OPENAI_API_KEY")
}

func TestBuildOpenClawBootstrapJSON_OpenAIAndTelegram(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha2.AddToScheme(scheme))

	ns := "default"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "openai-key", Namespace: ns},
		Data:       map[string][]byte{"OPENAI_API_KEY": []byte("sk-test")},
	}
	mc := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "mc1", Namespace: ns},
		Spec: v1alpha2.ModelConfigSpec{
			Model:           "gpt-4o",
			Provider:        v1alpha2.ModelProviderOpenAI,
			APIKeySecret:    "openai-key",
			APIKeySecretKey: "OPENAI_API_KEY",
			OpenAI:          &v1alpha2.OpenAIConfig{BaseURL: "https://api.example/v1"},
		},
	}
	sbx := &v1alpha2.Sandbox{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: ns},
		Spec: v1alpha2.SandboxSpec{
			Channels: []v1alpha2.SandboxChannel{
				{
					Name: "tg1",
					Type: v1alpha2.SandboxChannelTypeTelegram,
					Telegram: &v1alpha2.SandboxTelegramChannelSpec{
						BotToken: v1alpha2.SandboxChannelCredential{Value: "telegram-bot-token"},
					},
				},
			},
		},
	}

	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, mc).Build()
	raw, env, err := buildOpenClawBootstrapJSON(context.Background(), kube, ns, sbx, mc, 18800)
	require.NoError(t, err)
	require.Equal(t, "sk-test", env["OPENAI_API_KEY"])
	require.Equal(t, "telegram-bot-token", env["KAGENT_SB_CH_TG1_TELEGRAM_BOT"])

	var root map[string]any
	require.NoError(t, json.Unmarshal(raw, &root))
	require.Contains(t, root, "gateway")
	require.Contains(t, root, "models")
	require.Contains(t, root, "agents")
	models := root["models"].(map[string]any)
	provs := models["providers"].(map[string]any)
	openai := provs["openai"].(map[string]any)
	modelList := openai["models"].([]any)
	require.Len(t, modelList, 1)
	entry := modelList[0].(map[string]any)
	require.Equal(t, "gpt-4o", entry["id"])
	require.Equal(t, "gpt-4o", entry["name"])
	require.Equal(t, "openshell:resolve:env:OPENAI_API_KEY", openai["apiKey"])
	secRoot := root["secrets"].(map[string]any)
	secProvs := secRoot["providers"].(map[string]any)
	kagent := secProvs["kagent"].(map[string]any)
	require.Equal(t, "env", kagent["source"])
	al := kagent["allowlist"].([]any)
	require.ElementsMatch(t, []any{"KAGENT_SB_CH_TG1_TELEGRAM_BOT", "OPENAI_API_KEY"}, al)
	ch := root["channels"].(map[string]any)
	require.Contains(t, ch, "telegram")
	tg := ch["telegram"].(map[string]any)
	tgAcc := tg["accounts"].(map[string]any)
	tg1 := tgAcc["tg1"].(map[string]any)
	require.Equal(t, "telegram-bot-token", tg1["botToken"])
}

func TestSandboxHasChannelType_Discord(t *testing.T) {
	sbx := &v1alpha2.Sandbox{
		Spec: v1alpha2.SandboxSpec{
			Channels: []v1alpha2.SandboxChannel{
				{Name: "x", Type: v1alpha2.SandboxChannelTypeTelegram, Telegram: &v1alpha2.SandboxTelegramChannelSpec{
					BotToken: v1alpha2.SandboxChannelCredential{Value: "t"},
				}},
			},
		},
	}
	require.False(t, sandboxHasChannelType(sbx, v1alpha2.SandboxChannelTypeDiscord))
	sbx.Spec.Channels = append(sbx.Spec.Channels, v1alpha2.SandboxChannel{
		Name: "d",
		Type: v1alpha2.SandboxChannelTypeDiscord,
		Discord: &v1alpha2.SandboxDiscordChannelSpec{
			BotToken:      v1alpha2.SandboxChannelCredential{Value: "x"},
			ChannelAccess: v1alpha2.SandboxChannelAccessOpen,
		},
	})
	require.True(t, sandboxHasChannelType(sbx, v1alpha2.SandboxChannelTypeDiscord))
}
