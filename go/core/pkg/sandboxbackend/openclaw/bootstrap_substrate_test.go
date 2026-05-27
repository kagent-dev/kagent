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

func TestSubstrateGatewayBootstrap(t *testing.T) {
	t.Parallel()
	raw, err := openclaw.BuildGatewayOnlyBootstrapJSON(openclaw.SubstrateGatewayBootstrap("tok", 80, "/api/agentharnesses/kagent/claw/gateway/"))
	require.NoError(t, err)
	var root map[string]any
	require.NoError(t, json.Unmarshal(raw, &root))
	gw := root["gateway"].(map[string]any)
	require.Equal(t, "lan", gw["bind"])
	cui := gw["controlUi"].(map[string]any)
	require.Equal(t, "/api/agentharnesses/kagent/claw/gateway", cui["basePath"])
	require.Equal(t, true, cui["dangerouslyDisableDeviceAuth"])
}

func TestBuildSubstrateBootstrapJSON_ModelConfigAPIKeyUsesSecretRef(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha2.AddToScheme(scheme))

	ns := "default"
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

	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mc).Build()
	raw, env, err := openclaw.BuildSubstrateBootstrapJSON(context.Background(), kube, ns, sbx, mc, openclaw.SubstrateGatewayBootstrap("tok", 80, "/gw/"))
	require.NoError(t, err)

	var root map[string]any
	require.NoError(t, json.Unmarshal(raw, &root))
	secRoot := root["secrets"].(map[string]any)
	secProvs := secRoot["providers"].(map[string]any)
	defaultProv := secProvs["default"].(map[string]any)
	require.Contains(t, defaultProv["allowlist"], "OPENAI_API_KEY")
	defaults := secRoot["defaults"].(map[string]any)
	require.Equal(t, "default", defaults["env"])

	var apiKeyEnv *corev1.EnvVar
	for i := range env {
		if env[i].Name == "OPENAI_API_KEY" {
			apiKeyEnv = &env[i]
			break
		}
	}
	require.NotNil(t, apiKeyEnv)
	require.NotNil(t, apiKeyEnv.ValueFrom)
	require.NotNil(t, apiKeyEnv.ValueFrom.SecretKeyRef)
	require.Equal(t, "openai-key", apiKeyEnv.ValueFrom.SecretKeyRef.Name)
	require.Equal(t, "OPENAI_API_KEY", apiKeyEnv.ValueFrom.SecretKeyRef.Key)
	require.Empty(t, apiKeyEnv.Value)
}

func TestBuildSubstrateBootstrapJSON_TelegramUsesEnvSecretRef(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha2.AddToScheme(scheme))

	ns := "default"
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
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tg-token", Namespace: ns},
		Data:       map[string][]byte{"token": []byte("telegram-bot-token")},
	}
	sbx := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: ns},
		Spec: v1alpha2.AgentHarnessSpec{
			Channels: []v1alpha2.AgentHarnessChannel{
				{
					Name: "tg1",
					Type: v1alpha2.AgentHarnessChannelTypeTelegram,
					Telegram: &v1alpha2.AgentHarnessTelegramChannelSpec{
						BotToken: v1alpha2.AgentHarnessChannelCredential{
							ValueFrom: &v1alpha2.ValueSource{
								Type: v1alpha2.SecretValueSource,
								Name: "tg-token",
								Key:  "token",
							},
						},
					},
				},
			},
		},
	}

	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mc, secret).Build()
	raw, env, err := openclaw.BuildSubstrateBootstrapJSON(context.Background(), kube, ns, sbx, mc, openclaw.SubstrateGatewayBootstrap("tok", 80, "/gw/"))
	require.NoError(t, err)

	var root map[string]any
	require.NoError(t, json.Unmarshal(raw, &root))
	tg := root["channels"].(map[string]any)["telegram"].(map[string]any)
	tg1 := tg["accounts"].(map[string]any)["tg1"].(map[string]any)
	botToken := tg1["botToken"].(map[string]any)
	require.Equal(t, "env", botToken["source"])
	require.Equal(t, "default", botToken["provider"])
	require.Equal(t, "KAGENT_SB_CH_TG1_TELEGRAM_BOT", botToken["id"])
	require.NotEqual(t, "telegram-bot-token", tg1["botToken"])

	var tgEnv *corev1.EnvVar
	for i := range env {
		if env[i].Name == "KAGENT_SB_CH_TG1_TELEGRAM_BOT" {
			tgEnv = &env[i]
			break
		}
	}
	require.NotNil(t, tgEnv)
	require.NotNil(t, tgEnv.ValueFrom)
	require.NotNil(t, tgEnv.ValueFrom.SecretKeyRef)
	require.Equal(t, "tg-token", tgEnv.ValueFrom.SecretKeyRef.Name)
	require.Equal(t, "token", tgEnv.ValueFrom.SecretKeyRef.Key)
}
