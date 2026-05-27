package channels

import (
	"strings"
	"unicode"
)

// EnvKeySuffix sanitizes a channel binding name for use in environment variable names.
func EnvKeySuffix(channelName string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(channelName) {
		switch {
		case unicode.IsLetter(r):
			b.WriteRune(unicode.ToUpper(r))
		case unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "CHANNEL"
	}
	return b.String()
}

// TelegramBotTokenEnvKey is the per-channel Telegram bot token env var.
func TelegramBotTokenEnvKey(channelName string) string {
	return "TELEGRAM_BOT_TOKEN_" + EnvKeySuffix(channelName)
}

// TelegramAllowedUsersEnvKey is the per-channel Telegram allowlist env var (Hermes).
func TelegramAllowedUsersEnvKey(channelName string) string {
	return "TELEGRAM_ALLOWED_USERS_" + EnvKeySuffix(channelName)
}

// SlackBotTokenEnvKey is the per-channel Slack bot token env var.
func SlackBotTokenEnvKey(channelName string) string {
	return "SLACK_BOT_TOKEN_" + EnvKeySuffix(channelName)
}

// SlackAppTokenEnvKey is the per-channel Slack app token env var.
func SlackAppTokenEnvKey(channelName string) string {
	return "SLACK_APP_TOKEN_" + EnvKeySuffix(channelName)
}

// SlackAllowedUsersEnvKey is the per-channel Slack allowlist env var (Hermes).
func SlackAllowedUsersEnvKey(channelName string) string {
	return "SLACK_ALLOWED_USERS_" + EnvKeySuffix(channelName)
}

// SlackHomeChannelEnvKey is the per-channel Slack home channel env var (Hermes).
func SlackHomeChannelEnvKey(channelName string) string {
	return "SLACK_HOME_CHANNEL_" + EnvKeySuffix(channelName)
}

// SlackHomeChannelNameEnvKey is the per-channel Slack home channel display name env var (Hermes).
func SlackHomeChannelNameEnvKey(channelName string) string {
	return "SLACK_HOME_CHANNEL_NAME_" + EnvKeySuffix(channelName)
}
