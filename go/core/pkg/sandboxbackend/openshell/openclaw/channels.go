package openclaw

import (
	"context"
	"maps"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/channels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type harnessChannels struct {
	resolved *channels.Resolved
}

func accumulateHarnessChannels(ctx context.Context, kube client.Client, namespace string, backend v1alpha2.AgentHarnessBackendType, specChannels []v1alpha2.AgentHarnessChannel, env map[string]string) (*harnessChannels, error) {
	resolved, err := channels.Resolve(ctx, kube, namespace, backend, specChannels)
	if err != nil {
		return nil, err
	}
	maps.Copy(env, resolved.Secrets)
	return &harnessChannels{resolved: resolved}, nil
}

func (a *harnessChannels) channelsJSON() *channelsConfig {
	if a == nil || a.resolved == nil {
		return nil
	}
	r := a.resolved
	if len(r.Telegram) == 0 && len(r.Slack) == 0 {
		return nil
	}
	out := &channelsConfig{}
	if len(r.Telegram) > 0 {
		accounts := make(map[string]telegramAccount, len(r.Telegram))
		var def string
		for _, tg := range r.Telegram {
			acc := telegramAccount{
				Name:     tg.Name,
				Enabled:  true,
				BotToken: openshellResolveEnv(channels.TelegramBotTokenEnvKey(tg.Name)),
			}
			if len(tg.AllowFrom) > 0 {
				acc.DMPolicy = "allowlist"
				acc.AllowFrom = tg.AllowFrom
			} else {
				acc.DMPolicy = "pairing"
			}
			accounts[tg.Name] = acc
			if def == "" {
				def = tg.Name
			}
		}
		out.Telegram = &telegramBundle{
			Enabled:        true,
			Accounts:       accounts,
			DefaultAccount: def,
		}
	}
	if len(r.Slack) > 0 {
		accounts := make(map[string]slackAccount, len(r.Slack))
		var def string
		for _, sl := range r.Slack {
			botKey := channels.SlackBotTokenEnvKey(sl.Name)
			appKey := channels.SlackAppTokenEnvKey(sl.Name)
			acc := slackAccount{
				Name:              sl.Name,
				Enabled:           true,
				Mode:              "socket",
				BotToken:          channels.SlackBotTokenPlaceholder(botKey),
				AppToken:          channels.SlackAppTokenPlaceholder(appKey),
				UserTokenReadOnly: true,
				GroupPolicy:       string(sl.ChannelAccess),
				Capabilities: slackCaps{
					InteractiveReplies: sl.InteractiveReplies,
				},
			}
			if chans := sl.AllowlistChannels; len(chans) > 0 {
				acc.DM = &groupDM{GroupEnabled: true, GroupChannels: chans}
			}
			accounts[sl.Name] = acc
			if def == "" {
				def = sl.Name
			}
		}
		out.Slack = &slackBundle{
			Enabled:           true,
			Mode:              "socket",
			WebhookPath:       "/slack/events",
			UserTokenReadOnly: true,
			GroupPolicy:       string(r.SlackRootGroupPolicy()),
			Accounts:          accounts,
			DefaultAccount:    def,
		}
	}
	return out
}
