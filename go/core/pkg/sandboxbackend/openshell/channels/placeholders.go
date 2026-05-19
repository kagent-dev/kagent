package channels

// ResolveEnvPlaceholder is the in-sandbox placeholder OpenShell rewrites at L7 egress.
func ResolveEnvPlaceholder(envKey string) string {
	return "openshell:resolve:env:" + envKey
}

// SlackBotTokenPlaceholder matches NemoClaw Hermes/OpenClaw Slack bot token shape validation.
func SlackBotTokenPlaceholder() string {
	return "xoxb-OPENSHELL-RESOLVE-ENV-SLACK_BOT_TOKEN"
}

// SlackAppTokenPlaceholder matches NemoClaw Hermes/OpenClaw Slack app token shape validation.
func SlackAppTokenPlaceholder() string {
	return "xapp-OPENSHELL-RESOLVE-ENV-SLACK_APP_TOKEN"
}
