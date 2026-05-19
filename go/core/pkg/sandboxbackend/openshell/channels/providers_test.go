package channels

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMessagingProviderDefs(t *testing.T) {
	resolved := &Resolved{
		Telegram: []TelegramAccount{{Name: "tg"}},
		Slack:    []SlackAccount{{Name: "sl"}},
		Secrets: map[string]string{
			TelegramBotTokenEnvKey("tg"): "tg-secret",
			SlackBotTokenEnvKey("sl"):    "xoxb",
			SlackAppTokenEnvKey("sl"):    "xapp",
		},
	}
	defs := MessagingProviderDefs("ns-h", resolved.Secrets, resolved)
	require.Len(t, defs, 3)
	require.Equal(t, "ns-h-telegram-TG", defs[0].Name)
	require.Equal(t, "ns-h-slack-SL", defs[1].Name)
	require.Equal(t, "ns-h-slack-app-SL", defs[2].Name)
}
