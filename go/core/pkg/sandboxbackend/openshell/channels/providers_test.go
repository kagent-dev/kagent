package channels

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMessagingProviderDefs(t *testing.T) {
	resolved := &Resolved{
		HasTelegram: true,
		HasSlack:    true,
		Secrets: map[string]string{
			EnvTelegramBotToken: "tg",
			EnvSlackBotToken:    "xoxb",
			EnvSlackAppToken:    "xapp",
		},
	}
	defs := MessagingProviderDefs("ns-h", resolved.Secrets, resolved)
	require.Len(t, defs, 3)
	require.Equal(t, "ns-h-telegram-bridge", defs[0].Name)
	require.Equal(t, "ns-h-slack-bridge", defs[1].Name)
	require.Equal(t, "ns-h-slack-app", defs[2].Name)
}
