package openshell

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const openclawBootstrapEnvProvider = "kagent"

// openclawOpenshellResolveEnv matches OpenClaw onboard --custom-api-key:
// credentials resolve via OpenShell’s env path inside the sandbox (same as
// literal OPENAI_API_KEY etc. still injected by kagent on ExecSandbox).
func openclawOpenshellResolveEnv(envVar string) string {
	return "openshell:resolve:env:" + envVar
}

func sandboxChannelEnvSuffix(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(name)) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	s := strings.Trim(b.String(), "_")
	if s == "" {
		return "CH"
	}
	return s
}

func channelSecretEnvVar(channelName, tokenRole string) string {
	return fmt.Sprintf("KAGENT_SB_CH_%s_%s", sandboxChannelEnvSuffix(channelName), tokenRole)
}

func putChannelCredential(ctx context.Context, kube client.Client, namespace string, cred v1alpha2.SandboxChannelCredential, envKey string, env map[string]string) error {
	if strings.TrimSpace(cred.Value) != "" {
		env[envKey] = strings.TrimSpace(cred.Value)
		return nil
	}
	if cred.ValueFrom == nil {
		return fmt.Errorf("channel credential requires value or valueFrom")
	}
	v, err := cred.ValueFrom.Resolve(ctx, kube, namespace)
	if err != nil {
		return fmt.Errorf("resolve credential %s: %w", envKey, err)
	}
	env[envKey] = v
	return nil
}

// resolvedChannelSecret returns the plaintext value putChannelCredential stored in env.
// Channel configs (Telegram botToken, Discord token, Slack tokens) must use literals in
// openclaw.json: OpenClaw's Bot API clients build URLs from botToken before OpenShell-style
// openshell:resolve:env: placeholders are expanded, which yields 404 from Telegram.
func resolvedChannelSecret(env map[string]string, envKey string) (string, error) {
	v := strings.TrimSpace(env[envKey])
	if v == "" {
		return "", fmt.Errorf("credential %s is missing or empty after resolve", envKey)
	}
	return v, nil
}

func splitAllowedList(raw string) []any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []any
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == ';'
	}) {
		s := strings.TrimSpace(part)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func telegramAllowFrom(ctx context.Context, kube client.Client, namespace string, spec *v1alpha2.SandboxTelegramChannelSpec) ([]any, error) {
	if len(spec.AllowedUserIDs) > 0 {
		out := make([]any, 0, len(spec.AllowedUserIDs))
		for _, id := range spec.AllowedUserIDs {
			s := strings.TrimSpace(id)
			if s != "" {
				out = append(out, s)
			}
		}
		return out, nil
	}
	if spec.AllowedUserIDsFrom != nil {
		raw, err := spec.AllowedUserIDsFrom.Resolve(ctx, kube, namespace)
		if err != nil {
			return nil, fmt.Errorf("resolve allowedUserIDsFrom: %w", err)
		}
		return splitAllowedList(raw), nil
	}
	return nil, nil
}

// openclawBootstrapProviderBaseURL is the Model provider baseUrl written into openclaw.json.
// OpenClaw inside the sandbox should call the OpenShell inference gateway on inference.local
// unless ModelConfig sets an explicit upstream base URL for that provider.
func openclawBootstrapProviderBaseURL(mc *v1alpha2.ModelConfig) string {
	switch mc.Spec.Provider {
	case v1alpha2.ModelProviderOpenAI:
		if mc.Spec.OpenAI != nil && strings.TrimSpace(mc.Spec.OpenAI.BaseURL) != "" {
			return strings.TrimSpace(mc.Spec.OpenAI.BaseURL)
		}
	case v1alpha2.ModelProviderAnthropic:
		if mc.Spec.Anthropic != nil && strings.TrimSpace(mc.Spec.Anthropic.BaseURL) != "" {
			return strings.TrimSpace(mc.Spec.Anthropic.BaseURL)
		}
	case v1alpha2.ModelProviderAzureOpenAI:
		if mc.Spec.AzureOpenAI != nil && strings.TrimSpace(mc.Spec.AzureOpenAI.Endpoint) != "" {
			return strings.TrimSpace(mc.Spec.AzureOpenAI.Endpoint)
		}
	case v1alpha2.ModelProviderOllama:
		if mc.Spec.Ollama != nil && strings.TrimSpace(mc.Spec.Ollama.Host) != "" {
			return strings.TrimSpace(mc.Spec.Ollama.Host)
		}
	case v1alpha2.ModelProviderSAPAICore:
		if mc.Spec.SAPAICore != nil && strings.TrimSpace(mc.Spec.SAPAICore.BaseURL) != "" {
			return strings.TrimSpace(mc.Spec.SAPAICore.BaseURL)
		}
	}
	return defaultOpenclawInferenceBaseURL
}

func openclawProviderAuth(mc *v1alpha2.ModelConfig) string {
	if mc.Spec.Provider == v1alpha2.ModelProviderBedrock {
		return "aws-sdk"
	}
	return "api-key"
}

func openclawProviderAPI(mc *v1alpha2.ModelConfig) (string, error) {
	switch mc.Spec.Provider {
	case v1alpha2.ModelProviderOpenAI:
		return "openai-completions", nil
	case v1alpha2.ModelProviderAnthropic:
		return "anthropic-messages", nil
	case v1alpha2.ModelProviderAzureOpenAI:
		return "azure-openai-responses", nil
	case v1alpha2.ModelProviderOllama:
		return "ollama", nil
	case v1alpha2.ModelProviderGemini, v1alpha2.ModelProviderGeminiVertexAI:
		return "google-generative-ai", nil
	case v1alpha2.ModelProviderAnthropicVertexAI:
		return "anthropic-messages", nil
	case v1alpha2.ModelProviderBedrock:
		return "bedrock-converse-stream", nil
	case v1alpha2.ModelProviderSAPAICore:
		return "", fmt.Errorf("model provider SAPAICore is not supported for OpenClaw sandbox JSON bootstrap")
	default:
		return "", fmt.Errorf("model provider %q is not supported for OpenClaw sandbox JSON bootstrap yet", mc.Spec.Provider)
	}
}

func slackInteractiveReplies(spec *v1alpha2.SandboxSlackChannelSpec) bool {
	if spec.InteractiveReplies == nil {
		return true
	}
	return *spec.InteractiveReplies
}

// buildOpenClawBootstrapJSON builds ~/.openclaw/openclaw.json contents plus environment variables
// that must be present when OpenClaw resolves openshell:resolve:env:<VAR> (API key + channel tokens).
func buildOpenClawBootstrapJSON(ctx context.Context, kube client.Client, namespace string, sbx *v1alpha2.Sandbox, mc *v1alpha2.ModelConfig, gwPort int) ([]byte, map[string]string, error) {
	if mc == nil {
		return nil, nil, fmt.Errorf("ModelConfig is required")
	}
	apiKey, err := resolveModelConfigAPIKey(ctx, kube, mc)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve model API key: %w", err)
	}
	apiAdapter, err := openclawProviderAPI(mc)
	if err != nil {
		return nil, nil, err
	}

	apiKeyEnv := defaultOpenclawAPIKeyEnvVar(mc.Spec.Provider)
	env := map[string]string{
		apiKeyEnv: apiKey,
	}

	modelID := strings.TrimSpace(mc.Spec.Model)
	if modelID == "" {
		return nil, nil, fmt.Errorf("ModelConfig.spec.model is required for OpenClaw bootstrap JSON")
	}

	providerRecord := gatewayProviderRecordName(mc.Spec.Provider)
	baseURL := openclawBootstrapProviderBaseURL(mc)

	root := map[string]any{
		"gateway": map[string]any{
			"mode": "local",
			"auth": map[string]any{
				"mode": "none",
			},
			"port": gwPort,
		},
		"models": map[string]any{
			"mode": "merge",
			"providers": map[string]any{
				providerRecord: map[string]any{
					"baseUrl": baseURL,
					"apiKey":  openclawOpenshellResolveEnv(apiKeyEnv),
					"auth":    openclawProviderAuth(mc),
					"api":     apiAdapter,
					// openclaw.schema.json requires models.providers.<id>.models (array of {id,name}).
					"models": []any{
						map[string]any{
							"id":   modelID,
							"name": modelID,
						},
					},
				},
			},
		},
		"agents": map[string]any{
			"defaults": map[string]any{
				"model": map[string]any{
					"primary": fmt.Sprintf("%s/%s", providerRecord, modelID),
				},
			},
		},
	}

	channelsRoot := map[string]any{}

	tgAccounts := map[string]any{}
	var tgDefault string
	dcAccounts := map[string]any{}
	var dcDefault string
	slAccounts := map[string]any{}
	var slDefault string
	var slackRootPolicy v1alpha2.SandboxChannelAccess
	var slackSeen bool

	for _, ch := range sbx.Spec.Channels {
		switch ch.Type {
		case v1alpha2.SandboxChannelTypeTelegram:
			spec := ch.Telegram
			if spec == nil {
				return nil, nil, fmt.Errorf("channel %q: telegram spec is required", ch.Name)
			}
			botEnv := channelSecretEnvVar(ch.Name, "TELEGRAM_BOT")
			if err := putChannelCredential(ctx, kube, namespace, spec.BotToken, botEnv, env); err != nil {
				return nil, nil, fmt.Errorf("channel %q telegram bot token: %w", ch.Name, err)
			}
			botTok, err := resolvedChannelSecret(env, botEnv)
			if err != nil {
				return nil, nil, fmt.Errorf("channel %q telegram %w", ch.Name, err)
			}
			allowFrom, err := telegramAllowFrom(ctx, kube, namespace, spec)
			if err != nil {
				return nil, nil, fmt.Errorf("channel %q telegram allowlist: %w", ch.Name, err)
			}
			acc := map[string]any{
				"name":     ch.Name,
				"enabled":  true,
				"botToken": botTok,
			}
			if len(allowFrom) > 0 {
				acc["dmPolicy"] = "allowlist"
				acc["allowFrom"] = allowFrom
			} else {
				acc["dmPolicy"] = "pairing"
			}
			tgAccounts[ch.Name] = acc
			if tgDefault == "" {
				tgDefault = ch.Name
			}

		case v1alpha2.SandboxChannelTypeDiscord:
			spec := ch.Discord
			if spec == nil {
				return nil, nil, fmt.Errorf("channel %q: discord spec is required", ch.Name)
			}
			botEnv := channelSecretEnvVar(ch.Name, "DISCORD_BOT")
			if err := putChannelCredential(ctx, kube, namespace, spec.BotToken, botEnv, env); err != nil {
				return nil, nil, fmt.Errorf("channel %q discord bot token: %w", ch.Name, err)
			}
			dcTok, err := resolvedChannelSecret(env, botEnv)
			if err != nil {
				return nil, nil, fmt.Errorf("channel %q discord %w", ch.Name, err)
			}
			acc := map[string]any{
				"name":        ch.Name,
				"enabled":     true,
				"token":       dcTok,
				"groupPolicy": string(spec.ChannelAccess),
				"dmPolicy":    "open",
			}
			if len(spec.AllowlistChannels) > 0 {
				acc["dm"] = map[string]any{
					"groupEnabled":  true,
					"groupChannels": stringSliceToAny(spec.AllowlistChannels),
				}
			}
			dcAccounts[ch.Name] = acc
			if dcDefault == "" {
				dcDefault = ch.Name
			}

		case v1alpha2.SandboxChannelTypeSlack:
			spec := ch.Slack
			if spec == nil {
				return nil, nil, fmt.Errorf("channel %q: slack spec is required", ch.Name)
			}
			botEnv := channelSecretEnvVar(ch.Name, "SLACK_BOT")
			appEnv := channelSecretEnvVar(ch.Name, "SLACK_APP")
			if err := putChannelCredential(ctx, kube, namespace, spec.BotToken, botEnv, env); err != nil {
				return nil, nil, fmt.Errorf("channel %q slack bot token: %w", ch.Name, err)
			}
			if err := putChannelCredential(ctx, kube, namespace, spec.AppToken, appEnv, env); err != nil {
				return nil, nil, fmt.Errorf("channel %q slack app token: %w", ch.Name, err)
			}
			slackBotTok, err := resolvedChannelSecret(env, botEnv)
			if err != nil {
				return nil, nil, fmt.Errorf("channel %q slack %w", ch.Name, err)
			}
			slackAppTok, err := resolvedChannelSecret(env, appEnv)
			if err != nil {
				return nil, nil, fmt.Errorf("channel %q slack %w", ch.Name, err)
			}
			acc := map[string]any{
				"name":              ch.Name,
				"enabled":           true,
				"mode":              "socket",
				"botToken":          slackBotTok,
				"appToken":          slackAppTok,
				"userTokenReadOnly": true,
				"groupPolicy":       string(spec.ChannelAccess),
				"capabilities": map[string]any{
					"interactiveReplies": slackInteractiveReplies(spec),
				},
			}
			if len(spec.AllowlistChannels) > 0 {
				acc["dm"] = map[string]any{
					"groupEnabled":  true,
					"groupChannels": stringSliceToAny(spec.AllowlistChannels),
				}
			}
			slAccounts[ch.Name] = acc
			if slDefault == "" {
				slDefault = ch.Name
			}
			if !slackSeen {
				slackRootPolicy = spec.ChannelAccess
				slackSeen = true
			}

		default:
			return nil, nil, fmt.Errorf("channel %q: unsupported type %q", ch.Name, ch.Type)
		}
	}

	if len(tgAccounts) > 0 {
		channelsRoot["telegram"] = map[string]any{
			"enabled":        true,
			"accounts":       tgAccounts,
			"defaultAccount": tgDefault,
		}
	}
	if len(dcAccounts) > 0 {
		channelsRoot["discord"] = map[string]any{
			"enabled":        true,
			"accounts":       dcAccounts,
			"defaultAccount": dcDefault,
		}
	}
	if len(slAccounts) > 0 {
		channelsRoot["slack"] = map[string]any{
			"enabled":           true,
			"mode":              "socket",
			"webhookPath":       "/slack/events",
			"userTokenReadOnly": true,
			"groupPolicy":       string(slackRootPolicy),
			"accounts":          slAccounts,
			"defaultAccount":    slDefault,
		}
	}

	if len(channelsRoot) > 0 {
		root["channels"] = channelsRoot
	}

	// Model apiKey and channel tokens use openshell:resolve:env:<VAR> (OpenClaw onboard pattern).
	// secrets.providers.kagent allowlist still enumerates injected vars for compatibility with
	// OpenClaw secret validation (openclaw.schema.json secrets.providers).
	secretAllow := make([]string, 0, len(env))
	for k := range env {
		secretAllow = append(secretAllow, k)
	}
	slices.Sort(secretAllow)
	allowlist := make([]any, len(secretAllow))
	for i, k := range secretAllow {
		allowlist[i] = k
	}
	root["secrets"] = map[string]any{
		"providers": map[string]any{
			openclawBootstrapEnvProvider: map[string]any{
				"source":    "env",
				"allowlist": allowlist,
			},
		},
	}

	raw, err := json.Marshal(root)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal openclaw json: %w", err)
	}
	return raw, env, nil
}

func stringSliceToAny(ss []string) []any {
	out := make([]any, 0, len(ss))
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
