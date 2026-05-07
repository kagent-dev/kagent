package openclaw

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type harnessChannels struct {
	telegram map[string]telegramAccount
	tgDef    string

	slack map[string]slackAccount
	slDef string

	slackRootPolicy v1alpha2.AgentHarnessChannelAccess
	slackSeen       bool
}

func newHarnessChannels() *harnessChannels {
	return &harnessChannels{
		telegram: make(map[string]telegramAccount),
		slack:    make(map[string]slackAccount),
	}
}

func accumulateHarnessChannels(ctx context.Context, kube client.Client, namespace string, channels []v1alpha2.AgentHarnessChannel, env map[string]string) (*harnessChannels, error) {
	a := newHarnessChannels()
	for _, ch := range channels {
		switch ch.Type {
		case v1alpha2.AgentHarnessChannelTypeTelegram:
			if err := a.addTelegram(ctx, kube, namespace, ch, env); err != nil {
				return nil, err
			}
		case v1alpha2.AgentHarnessChannelTypeSlack:
			if err := a.addSlack(ctx, kube, namespace, ch, env); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("channel %q: unsupported type %q", ch.Name, ch.Type)
		}
	}
	return a, nil
}

func (a *harnessChannels) channelsJSON() *channelsConfig {
	if len(a.telegram) == 0 && len(a.slack) == 0 {
		return nil
	}
	out := &channelsConfig{}
	if len(a.telegram) > 0 {
		out.Telegram = &telegramBundle{
			Enabled:        true,
			Accounts:       a.telegram,
			DefaultAccount: a.tgDef,
		}
	}
	if len(a.slack) > 0 {
		out.Slack = &slackBundle{
			Enabled:           true,
			Mode:              "socket",
			WebhookPath:       "/slack/events",
			UserTokenReadOnly: true,
			GroupPolicy:       string(a.slackRootPolicy),
			Accounts:          a.slack,
			DefaultAccount:    a.slDef,
		}
	}
	return out
}

func (a *harnessChannels) addTelegram(ctx context.Context, kube client.Client, namespace string, ch v1alpha2.AgentHarnessChannel, env map[string]string) error {
	spec := ch.Telegram
	if spec == nil {
		return fmt.Errorf("channel %q: telegram spec is required", ch.Name)
	}
	botEnv := channelSecretEnvVar(ch.Name, "TELEGRAM_BOT")
	if err := putChannelCredential(ctx, kube, namespace, spec.BotToken, botEnv, env); err != nil {
		return fmt.Errorf("channel %q telegram bot token: %w", ch.Name, err)
	}
	botTok, err := resolvedChannelSecret(env, botEnv)
	if err != nil {
		return fmt.Errorf("channel %q telegram %w", ch.Name, err)
	}
	allowFrom, err := telegramAllowFrom(ctx, kube, namespace, spec)
	if err != nil {
		return fmt.Errorf("channel %q telegram allowlist: %w", ch.Name, err)
	}
	acc := telegramAccount{
		Name:     ch.Name,
		Enabled:  true,
		BotToken: botTok,
	}
	if len(allowFrom) > 0 {
		acc.DMPolicy = "allowlist"
		acc.AllowFrom = allowFrom
	} else {
		acc.DMPolicy = "pairing"
	}
	a.telegram[ch.Name] = acc
	if a.tgDef == "" {
		a.tgDef = ch.Name
	}
	return nil
}

func (a *harnessChannels) addSlack(ctx context.Context, kube client.Client, namespace string, ch v1alpha2.AgentHarnessChannel, env map[string]string) error {
	spec := ch.Slack
	if spec == nil {
		return fmt.Errorf("channel %q: slack spec is required", ch.Name)
	}
	botEnv := channelSecretEnvVar(ch.Name, "SLACK_BOT")
	appEnv := channelSecretEnvVar(ch.Name, "SLACK_APP")
	if err := putChannelCredential(ctx, kube, namespace, spec.BotToken, botEnv, env); err != nil {
		return fmt.Errorf("channel %q slack bot token: %w", ch.Name, err)
	}
	if err := putChannelCredential(ctx, kube, namespace, spec.AppToken, appEnv, env); err != nil {
		return fmt.Errorf("channel %q slack app token: %w", ch.Name, err)
	}
	slackBotTok, err := resolvedChannelSecret(env, botEnv)
	if err != nil {
		return fmt.Errorf("channel %q slack %w", ch.Name, err)
	}
	slackAppTok, err := resolvedChannelSecret(env, appEnv)
	if err != nil {
		return fmt.Errorf("channel %q slack %w", ch.Name, err)
	}
	acc := slackAccount{
		Name:              ch.Name,
		Enabled:           true,
		Mode:              "socket",
		BotToken:          slackBotTok,
		AppToken:          slackAppTok,
		UserTokenReadOnly: true,
		GroupPolicy:       string(spec.ChannelAccess),
		Capabilities: slackCaps{
			InteractiveReplies: slackInteractiveReplies(spec),
		},
	}
	if chans := trimNonEmptyStrings(spec.AllowlistChannels); len(chans) > 0 {
		acc.DM = &groupDM{GroupEnabled: true, GroupChannels: chans}
	}
	a.slack[ch.Name] = acc
	if a.slDef == "" {
		a.slDef = ch.Name
	}
	if !a.slackSeen {
		a.slackRootPolicy = spec.ChannelAccess
		a.slackSeen = true
	}
	return nil
}

func slackInteractiveReplies(spec *v1alpha2.AgentHarnessSlackChannelSpec) bool {
	if spec.InteractiveReplies == nil {
		return true
	}
	return *spec.InteractiveReplies
}

func splitAllowedList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
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

func telegramAllowFrom(ctx context.Context, kube client.Client, namespace string, spec *v1alpha2.AgentHarnessTelegramChannelSpec) ([]string, error) {
	if len(spec.AllowedUserIDs) > 0 {
		out := make([]string, 0, len(spec.AllowedUserIDs))
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

func trimNonEmptyStrings(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
