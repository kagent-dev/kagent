package openclaw

// Types mirror the JSON shape written to ~/.openclaw/openclaw.json during sandbox bootstrap.
// Field names match OpenClaw config conventions (camelCase).

type bootstrapDocument struct {
	Gateway  gatewaySection  `json:"gateway"`
	Models   modelsSection   `json:"models"`
	Agents   agentsSection   `json:"agents"`
	Channels *channelsConfig `json:"channels,omitempty"`
	Secrets  secretsSection  `json:"secrets"`
}

type gatewaySection struct {
	Mode string      `json:"mode"`
	Bind string      `json:"bind"`
	Auth gatewayAuth `json:"auth"`
	Port int         `json:"port"`
}

type gatewayAuth struct {
	Mode string `json:"mode"`
}

type modelsSection struct {
	Mode      string                      `json:"mode"`
	Providers map[string]providerSettings `json:"providers"`
}

type providerSettings struct {
	BaseURL string      `json:"baseUrl"`
	APIKey  string      `json:"apiKey"`
	Auth    string      `json:"auth"`
	API     string      `json:"api"`
	Models  []modelSlot `json:"models"`
}

type modelSlot struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type agentsSection struct {
	Defaults agentDefaults `json:"defaults"`
}

type agentDefaults struct {
	Model defaultModelPick `json:"model"`
}

type defaultModelPick struct {
	Primary string `json:"primary"`
}

type channelsConfig struct {
	Telegram *telegramBundle `json:"telegram,omitempty"`
	Slack    *slackBundle    `json:"slack,omitempty"`
}

type telegramBundle struct {
	Enabled        bool                       `json:"enabled"`
	Accounts       map[string]telegramAccount `json:"accounts"`
	DefaultAccount string                     `json:"defaultAccount"`
}

type telegramAccount struct {
	Name      string   `json:"name"`
	Enabled   bool     `json:"enabled"`
	BotToken  string   `json:"botToken"`
	DMPolicy  string   `json:"dmPolicy"`
	AllowFrom []string `json:"allowFrom,omitempty"`
}

type slackBundle struct {
	Enabled           bool                    `json:"enabled"`
	Mode              string                  `json:"mode"`
	WebhookPath       string                  `json:"webhookPath"`
	UserTokenReadOnly bool                    `json:"userTokenReadOnly"`
	GroupPolicy       string                  `json:"groupPolicy"`
	Accounts          map[string]slackAccount `json:"accounts"`
	DefaultAccount    string                  `json:"defaultAccount"`
}

type slackAccount struct {
	Name              string    `json:"name"`
	Enabled           bool      `json:"enabled"`
	Mode              string    `json:"mode"`
	BotToken          string    `json:"botToken"`
	AppToken          string    `json:"appToken"`
	UserTokenReadOnly bool      `json:"userTokenReadOnly"`
	GroupPolicy       string    `json:"groupPolicy"`
	Capabilities      slackCaps `json:"capabilities"`
	DM                *groupDM  `json:"dm,omitempty"`
}

type slackCaps struct {
	InteractiveReplies bool `json:"interactiveReplies"`
}

type groupDM struct {
	GroupEnabled  bool     `json:"groupEnabled"`
	GroupChannels []string `json:"groupChannels"`
}

type secretsSection struct {
	Providers map[string]secretProvider `json:"providers"`
}

type secretProvider struct {
	Source    string   `json:"source"`
	Allowlist []string `json:"allowlist"`
}
