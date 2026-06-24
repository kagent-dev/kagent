package openclaw_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openclaw"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBuildBootstrapJSON_OpenAIDefaultBaseURLInferenceLocal(t *testing.T) {
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
	sbx := &v1alpha2.AgentHarness{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: ns}}

	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, mc).Build()
	raw, _, err := openclaw.BuildBootstrapJSON(context.Background(), kube, ns, sbx, mc, openclaw.OpenshellGatewayBootstrap(18800), openclaw.DefaultInferenceBaseURL)
	require.NoError(t, err)

	var root map[string]any
	require.NoError(t, json.Unmarshal(raw, &root))
	models := root["models"].(map[string]any)
	provs := models["providers"].(map[string]any)
	openai := provs["openai"].(map[string]any)
	require.Equal(t, openclaw.DefaultInferenceBaseURL, openai["baseUrl"])
	require.Equal(t, "openshell:resolve:env:OPENAI_API_KEY", openai["apiKey"])
	secRoot := root["secrets"].(map[string]any)
	secProvs := secRoot["providers"].(map[string]any)
	kagent := secProvs["kagent"].(map[string]any)
	require.Equal(t, "env", kagent["source"])
	require.Contains(t, kagent["allowlist"], "OPENAI_API_KEY")
}

func TestBuildBootstrapJSON_SubstrateOmitsModelsWhenNoExplicitBaseURL(t *testing.T) {
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
	sbx := &v1alpha2.AgentHarness{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: ns}}

	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, mc).Build()
	raw, _, err := openclaw.BuildBootstrapJSON(context.Background(), kube, ns, sbx, mc, openclaw.SubstrateGatewayBootstrap("tok", 80, "/api/agentharnesses/default/s1/gateway"), openclaw.SubstrateBootstrapDefaultBaseURL)
	require.NoError(t, err)

	var root map[string]any
	require.NoError(t, json.Unmarshal(raw, &root))
	_, hasModels := root["models"]
	require.False(t, hasModels)
	agents := root["agents"].(map[string]any)
	defaults := agents["defaults"].(map[string]any)
	model := defaults["model"].(map[string]any)
	require.Equal(t, "openai/gpt-4o", model["primary"])
}

func TestBuildBootstrapJSON_OpenAIAndTelegram(t *testing.T) {
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
	sbx := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: ns},
		Spec: v1alpha2.AgentHarnessSpec{
			Channels: []v1alpha2.AgentHarnessChannel{
				{
					Name: "tg1",
					Type: v1alpha2.AgentHarnessChannelTypeTelegram,
					Telegram: &v1alpha2.AgentHarnessTelegramChannelSpec{
						BotToken: v1alpha2.AgentHarnessChannelCredential{Value: "telegram-bot-token"},
					},
				},
			},
		},
	}

	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, mc).Build()
	raw, env, err := openclaw.BuildBootstrapJSON(context.Background(), kube, ns, sbx, mc, openclaw.OpenshellGatewayBootstrap(18800), openclaw.DefaultInferenceBaseURL)
	require.NoError(t, err)
	require.Equal(t, "sk-test", env["OPENAI_API_KEY"])
	require.Equal(t, "telegram-bot-token", env["TELEGRAM_BOT_TOKEN_TG1"])

	var root map[string]any
	require.NoError(t, json.Unmarshal(raw, &root))
	require.Contains(t, root, "gateway")
	gw := root["gateway"].(map[string]any)
	require.Equal(t, "loopback", gw["bind"])
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
	require.ElementsMatch(t, []any{"TELEGRAM_BOT_TOKEN_TG1", "OPENAI_API_KEY"}, al)
	ch := root["channels"].(map[string]any)
	require.Contains(t, ch, "telegram")
	tg := ch["telegram"].(map[string]any)
	tgAcc := tg["accounts"].(map[string]any)
	tg1 := tgAcc["tg1"].(map[string]any)
	require.Equal(t, "openshell:resolve:env:TELEGRAM_BOT_TOKEN_TG1", tg1["botToken"])
}
