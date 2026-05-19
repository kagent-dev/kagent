package channels

// MessagingBridgeNames are OpenShell provider names for a sandbox (NemoClaw onboard).
func TelegramBridgeName(sandboxName string) string {
	return sandboxName + "-telegram-bridge"
}

func SlackBridgeName(sandboxName string) string {
	return sandboxName + "-slack-bridge"
}

func SlackAppBridgeName(sandboxName string) string {
	return sandboxName + "-slack-app"
}

// MessagingProviderDef is an OpenShell gateway provider for one messaging credential.
type MessagingProviderDef struct {
	Name        string
	Credentials map[string]string
}

// MessagingProviderDefs builds provider upsert records from resolved channel secrets.
func MessagingProviderDefs(sandboxName string, secrets map[string]string, resolved *Resolved) []MessagingProviderDef {
	if sandboxName == "" || resolved == nil {
		return nil
	}
	var defs []MessagingProviderDef
	if resolved.HasTelegram {
		if tok := secrets[EnvTelegramBotToken]; tok != "" {
			defs = append(defs, MessagingProviderDef{
				Name: TelegramBridgeName(sandboxName),
				Credentials: map[string]string{
					EnvTelegramBotToken: tok,
				},
			})
		}
	}
	if resolved.HasSlack {
		if tok := secrets[EnvSlackBotToken]; tok != "" {
			defs = append(defs, MessagingProviderDef{
				Name: SlackBridgeName(sandboxName),
				Credentials: map[string]string{
					EnvSlackBotToken: tok,
				},
			})
		}
		if tok := secrets[EnvSlackAppToken]; tok != "" {
			defs = append(defs, MessagingProviderDef{
				Name: SlackAppBridgeName(sandboxName),
				Credentials: map[string]string{
					EnvSlackAppToken: tok,
				},
			})
		}
	}
	return defs
}
