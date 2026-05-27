package channels

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvKeySuffix(t *testing.T) {
	require.Equal(t, "MY_BOT", EnvKeySuffix("my-bot"))
	require.Equal(t, "CHANNEL", EnvKeySuffix("  "))
	require.Equal(t, "TELEGRAM_BOT_TOKEN_MY_BOT", TelegramBotTokenEnvKey("my-bot"))
}
