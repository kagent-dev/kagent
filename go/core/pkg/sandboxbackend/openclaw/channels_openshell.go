package openclaw

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/channels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func accumulateHarnessChannels(ctx context.Context, kube client.Client, namespace string, specChannels []v1alpha2.AgentHarnessChannel, env map[string]string) (*harnessChannels, error) {
	a := newHarnessChannels()
	for _, ch := range specChannels {
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
			return nil, unsupportedChannelType(ch.Name, ch.Type)
		}
	}
	return a, nil
}

func (a *harnessChannels) addTelegram(ctx context.Context, kube client.Client, namespace string, ch v1alpha2.AgentHarnessChannel, env map[string]string) error {
	spec := ch.Telegram
	if spec == nil {
		return fmt.Errorf("channel %q: telegram spec is required", ch.Name)
	}
	botEnv := channels.TelegramBotTokenEnvKey(ch.Name)
	if err := putChannelCredential(ctx, kube, namespace, spec.BotToken, botEnv, env); err != nil {
		return fmt.Errorf("channel %q telegram bot token: %w", ch.Name, err)
	}
	allowFrom, err := telegramAllowFrom(ctx, kube, namespace, spec)
	if err != nil {
		return fmt.Errorf("channel %q telegram allowlist: %w", ch.Name, err)
	}
	acc := telegramAccount{
		Name:     ch.Name,
		Enabled:  true,
		BotToken: credentialValue{literal: openshellResolveEnv(botEnv)},
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
	botEnv := channels.SlackBotTokenEnvKey(ch.Name)
	appEnv := channels.SlackAppTokenEnvKey(ch.Name)
	if err := putChannelCredential(ctx, kube, namespace, spec.BotToken, botEnv, env); err != nil {
		return fmt.Errorf("channel %q slack bot token: %w", ch.Name, err)
	}
	if err := putChannelCredential(ctx, kube, namespace, spec.AppToken, appEnv, env); err != nil {
		return fmt.Errorf("channel %q slack app token: %w", ch.Name, err)
	}
	opts := openClawSlackOptions(spec)
	access := openClawSlackChannelAccess(opts)
	acc := slackAccount{
		Name:              ch.Name,
		Enabled:           true,
		Mode:              "socket",
		BotToken:          credentialValue{literal: channels.SlackBotTokenPlaceholder(botEnv)},
		AppToken:          credentialValue{literal: channels.SlackAppTokenPlaceholder(appEnv)},
		UserTokenReadOnly: true,
		GroupPolicy:       string(access),
		Capabilities: slackCaps{
			InteractiveReplies: slackInteractiveReplies(opts),
		},
	}
	if chans := trimNonEmptyStrings(opts.AllowlistChannels); len(chans) > 0 {
		acc.DM = &groupDM{GroupEnabled: true, GroupChannels: chans}
	}
	a.slack[ch.Name] = acc
	if a.slDef == "" {
		a.slDef = ch.Name
	}
	if !a.slackSeen {
		a.slackRootPolicy = access
		a.slackSeen = true
	}
	return nil
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
