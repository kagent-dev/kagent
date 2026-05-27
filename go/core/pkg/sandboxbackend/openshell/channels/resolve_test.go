package channels

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolve_perChannelTelegramSecrets(t *testing.T) {
	ns := "default"
	kube := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	channels := []v1alpha2.AgentHarnessChannel{
		{
			Name: "bot-a",
			Type: v1alpha2.AgentHarnessChannelTypeTelegram,
			Telegram: &v1alpha2.AgentHarnessTelegramChannelSpec{
				BotToken: v1alpha2.AgentHarnessChannelCredential{Value: "token-a"},
			},
		},
		{
			Name: "bot-b",
			Type: v1alpha2.AgentHarnessChannelTypeTelegram,
			Telegram: &v1alpha2.AgentHarnessTelegramChannelSpec{
				BotToken: v1alpha2.AgentHarnessChannelCredential{Value: "token-b"},
			},
		},
	}
	resolved, err := Resolve(context.Background(), kube, ns, v1alpha2.AgentHarnessBackendHermes, channels)
	require.NoError(t, err)
	require.Equal(t, "token-a", resolved.Secrets[TelegramBotTokenEnvKey("bot-a")])
	require.Equal(t, "token-b", resolved.Secrets[TelegramBotTokenEnvKey("bot-b")])
}

func TestResolve_duplicateChannelName(t *testing.T) {
	kube := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	_, err := Resolve(context.Background(), kube, "default", v1alpha2.AgentHarnessBackendHermes, []v1alpha2.AgentHarnessChannel{
		{Name: "dup", Type: v1alpha2.AgentHarnessChannelTypeTelegram, Telegram: &v1alpha2.AgentHarnessTelegramChannelSpec{
			BotToken: v1alpha2.AgentHarnessChannelCredential{Value: "a"},
		}},
		{Name: "dup", Type: v1alpha2.AgentHarnessChannelTypeTelegram, Telegram: &v1alpha2.AgentHarnessTelegramChannelSpec{
			BotToken: v1alpha2.AgentHarnessChannelCredential{Value: "b"},
		}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate binding name")
}

func TestMessagingProviderDefs_perChannel(t *testing.T) {
	resolved := &Resolved{
		Telegram: []TelegramAccount{{Name: "tg1"}, {Name: "tg2"}},
		Slack:    []SlackAccount{{Name: "sl1"}},
		Secrets: map[string]string{
			TelegramBotTokenEnvKey("tg1"): "tok1",
			TelegramBotTokenEnvKey("tg2"): "tok2",
			SlackBotTokenEnvKey("sl1"):    "xoxb",
			SlackAppTokenEnvKey("sl1"):    "xapp",
		},
	}
	defs := MessagingProviderDefs("ns-h", resolved.Secrets, resolved)
	require.Len(t, defs, 4)
	require.Equal(t, "ns-h-telegram-TG1", defs[0].Name)
	require.Equal(t, "tok1", defs[0].Credentials[TelegramBotTokenEnvKey("tg1")])
	require.Equal(t, "ns-h-telegram-TG2", defs[1].Name)
	require.Equal(t, "tok2", defs[1].Credentials[TelegramBotTokenEnvKey("tg2")])
}
