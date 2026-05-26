package hermes_test

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/hermes"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBuildHermesConfigYAML_ModelOnly(t *testing.T) {
	mc := &v1alpha2.ModelConfig{
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "test-model",
			Provider: v1alpha2.ModelProviderOpenAI,
		},
	}
	raw, err := hermes.BuildHermesConfigYAML(mc, nil)
	require.NoError(t, err)
	s := string(raw)
	require.Contains(t, s, "default: test-model")
	require.Contains(t, s, "provider: custom")
	require.Contains(t, s, "base_url: https://inference.local/v1")
	require.Contains(t, s, "port: 18642")
}

func TestBuildBootstrapArtifacts_TelegramSlack(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha2.AddToScheme(scheme))

	ns := "default"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tg", Namespace: ns},
		Data:       map[string][]byte{"token": []byte("tg-token")},
	}
	mc := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "mc1", Namespace: ns},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4o",
			Provider: v1alpha2.ModelProviderOpenAI,
		},
	}
	ah := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "h1", Namespace: ns},
		Spec: v1alpha2.AgentHarnessSpec{
			Backend: v1alpha2.AgentHarnessBackendHermes,
			Channels: []v1alpha2.AgentHarnessChannel{
				{
					Name: "tg",
					Type: v1alpha2.AgentHarnessChannelTypeTelegram,
					Telegram: &v1alpha2.AgentHarnessTelegramChannelSpec{
						BotToken: v1alpha2.AgentHarnessChannelCredential{
							ValueFrom: &v1alpha2.ValueSource{
								Type: v1alpha2.SecretValueSource,
								Name: "tg",
								Key:  "token",
							},
						},
						AllowedUserIDs: []string{"123456789"},
					},
				},
				{
					Name: "sl",
					Type: v1alpha2.AgentHarnessChannelTypeSlack,
					Slack: &v1alpha2.AgentHarnessSlackChannelSpec{
						BotToken: v1alpha2.AgentHarnessChannelCredential{Value: "xoxb-bot"},
						AppToken: v1alpha2.AgentHarnessChannelCredential{Value: "xapp-app"},
						Hermes: &v1alpha2.AgentHarnessHermesSlackOptions{
							AllowedUserIDs:  []string{"U01234567", "U89ABCDEF"},
							HomeChannel:     "C01234567890",
							HomeChannelName: "general",
						},
					},
				},
			},
		},
	}

	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, mc).Build()
	configYAML, envFile, execEnv, err := hermes.BuildBootstrapArtifacts(context.Background(), kube, ns, ah, mc)
	require.NoError(t, err)
	require.Contains(t, string(configYAML), "require_mention: true")
	env := string(envFile)
	require.Contains(t, env, "TELEGRAM_BOT_TOKEN_TG=openshell:resolve:env:TELEGRAM_BOT_TOKEN_TG")
	require.Contains(t, env, "TELEGRAM_ALLOWED_USERS_TG=123456789")
	require.Contains(t, env, "SLACK_BOT_TOKEN_SL=xoxb-OPENSHELL-RESOLVE-ENV-SLACK_BOT_TOKEN_SL")
	require.Contains(t, env, "SLACK_APP_TOKEN_SL=xapp-OPENSHELL-RESOLVE-ENV-SLACK_APP_TOKEN_SL")
	require.Contains(t, env, "SLACK_ALLOWED_USERS_SL=U01234567,U89ABCDEF")
	require.Contains(t, env, "SLACK_HOME_CHANNEL_SL=C01234567890")
	require.Contains(t, env, "SLACK_HOME_CHANNEL_NAME_SL=general")
	require.Equal(t, "tg-token", execEnv["TELEGRAM_BOT_TOKEN_TG"])
	require.Equal(t, "xoxb-bot", execEnv["SLACK_BOT_TOKEN_SL"])
}
