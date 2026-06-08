package channels

// MessagingProviderDef is an OpenShell gateway provider for one messaging credential.
type MessagingProviderDef struct {
	Name        string
	Credentials map[string]string
}

// TelegramBridgeName is the OpenShell provider name for a Telegram channel binding.
func TelegramBridgeName(sandboxName, channelName string) string {
	return sandboxName + "-telegram-" + EnvKeySuffix(channelName)
}

// SlackBridgeName is the OpenShell provider name for a Slack bot token binding.
func SlackBridgeName(sandboxName, channelName string) string {
	return sandboxName + "-slack-" + EnvKeySuffix(channelName)
}

// SlackAppBridgeName is the OpenShell provider name for a Slack app token binding.
func SlackAppBridgeName(sandboxName, channelName string) string {
	return sandboxName + "-slack-app-" + EnvKeySuffix(channelName)
}

// MessagingProviderDefs builds per-channel provider upsert records from resolved secrets.
func MessagingProviderDefs(sandboxName string, secrets map[string]string, resolved *Resolved) []MessagingProviderDef {
	if sandboxName == "" || resolved == nil {
		return nil
	}
	var defs []MessagingProviderDef
	for _, tg := range resolved.Telegram {
		envKey := TelegramBotTokenEnvKey(tg.Name)
		if tok := secrets[envKey]; tok != "" {
			defs = append(defs, MessagingProviderDef{
				Name:        TelegramBridgeName(sandboxName, tg.Name),
				Credentials: map[string]string{envKey: tok},
			})
		}
	}
	for _, sl := range resolved.Slack {
		if tok := secrets[SlackBotTokenEnvKey(sl.Name)]; tok != "" {
			defs = append(defs, MessagingProviderDef{
				Name:        SlackBridgeName(sandboxName, sl.Name),
				Credentials: map[string]string{SlackBotTokenEnvKey(sl.Name): tok},
			})
		}
		if tok := secrets[SlackAppTokenEnvKey(sl.Name)]; tok != "" {
			defs = append(defs, MessagingProviderDef{
				Name:        SlackAppBridgeName(sandboxName, sl.Name),
				Credentials: map[string]string{SlackAppTokenEnvKey(sl.Name): tok},
			})
		}
	}
	return defs
}
