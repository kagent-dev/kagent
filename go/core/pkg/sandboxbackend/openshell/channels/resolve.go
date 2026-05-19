package channels

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TelegramAccount is one Telegram channel account in the harness.
type TelegramAccount struct {
	Name      string
	AllowFrom []string
}

// SlackAccount is one Slack channel account in the harness.
type SlackAccount struct {
	Name               string
	ChannelAccess      v1alpha2.AgentHarnessChannelAccess
	AllowlistChannels  []string
	InteractiveReplies bool
}

// Resolved holds channel credentials and per-backend configuration derived from AgentHarness.spec.channels.
type Resolved struct {
	Secrets map[string]string

	HasTelegram bool
	HasSlack    bool

	TelegramAllow []string
	SlackAllow    []string

	// Hermes: first Slack channel with homeChannel / homeChannelName wins.
	SlackHomeChannel     string
	SlackHomeChannelName string

	Telegram []TelegramAccount
	Slack    []SlackAccount

	slackRootPolicy v1alpha2.AgentHarnessChannelAccess
	slackSeen       bool
}

// Resolve reads AgentHarness channels, populates standard credential env keys in Secrets,
// and returns structured account metadata for Hermes/OpenClaw bootstrap.
func Resolve(ctx context.Context, kube client.Client, namespace string, channels []v1alpha2.AgentHarnessChannel) (*Resolved, error) {
	r := &Resolved{Secrets: map[string]string{}}
	for _, ch := range channels {
		switch ch.Type {
		case v1alpha2.AgentHarnessChannelTypeTelegram:
			if err := r.addTelegram(ctx, kube, namespace, ch); err != nil {
				return nil, err
			}
		case v1alpha2.AgentHarnessChannelTypeSlack:
			if err := r.addSlack(ctx, kube, namespace, ch); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("channel %q: unsupported type %q", ch.Name, ch.Type)
		}
	}
	return r, nil
}

func (r *Resolved) addTelegram(ctx context.Context, kube client.Client, namespace string, ch v1alpha2.AgentHarnessChannel) error {
	spec := ch.Telegram
	if spec == nil {
		return fmt.Errorf("channel %q: telegram spec is required", ch.Name)
	}
	if err := PutChannelCredential(ctx, kube, namespace, spec.BotToken, EnvTelegramBotToken, r.Secrets); err != nil {
		return fmt.Errorf("channel %q telegram bot token: %w", ch.Name, err)
	}
	allow, err := TelegramAllowFrom(ctx, kube, namespace, spec)
	if err != nil {
		return fmt.Errorf("channel %q telegram allowlist: %w", ch.Name, err)
	}
	r.HasTelegram = true
	if len(allow) > 0 {
		r.TelegramAllow = allow
	}
	r.Telegram = append(r.Telegram, TelegramAccount{Name: ch.Name, AllowFrom: allow})
	return nil
}

func (r *Resolved) addSlack(ctx context.Context, kube client.Client, namespace string, ch v1alpha2.AgentHarnessChannel) error {
	spec := ch.Slack
	if spec == nil {
		return fmt.Errorf("channel %q: slack spec is required", ch.Name)
	}
	if err := PutChannelCredential(ctx, kube, namespace, spec.BotToken, EnvSlackBotToken, r.Secrets); err != nil {
		return fmt.Errorf("channel %q slack bot token: %w", ch.Name, err)
	}
	if err := PutChannelCredential(ctx, kube, namespace, spec.AppToken, EnvSlackAppToken, r.Secrets); err != nil {
		return fmt.Errorf("channel %q slack app token: %w", ch.Name, err)
	}
	allow, err := SlackAllowedUsers(ctx, kube, namespace, spec)
	if err != nil {
		return fmt.Errorf("channel %q slack allowed users: %w", ch.Name, err)
	}
	interactive := true
	if spec.InteractiveReplies != nil {
		interactive = *spec.InteractiveReplies
	}
	access := spec.ChannelAccess
	if access == "" {
		access = v1alpha2.AgentHarnessChannelAccessOpen
	}
	r.HasSlack = true
	if len(allow) > 0 {
		r.SlackAllow = append(r.SlackAllow, allow...)
	}
	r.Slack = append(r.Slack, SlackAccount{
		Name:               ch.Name,
		ChannelAccess:      access,
		AllowlistChannels:  TrimNonEmptyStrings(spec.AllowlistChannels),
		InteractiveReplies: interactive,
	})
	if !r.slackSeen {
		r.slackRootPolicy = access
		r.slackSeen = true
	}
	if r.SlackHomeChannel == "" {
		if home := strings.TrimSpace(spec.HomeChannel); home != "" {
			r.SlackHomeChannel = home
			r.SlackHomeChannelName = strings.TrimSpace(spec.HomeChannelName)
		}
	}
	return nil
}

// SlackRootGroupPolicy returns the group policy for the first Slack channel (OpenClaw bundle).
func (r *Resolved) SlackRootGroupPolicy() v1alpha2.AgentHarnessChannelAccess {
	if r.slackSeen {
		return r.slackRootPolicy
	}
	return v1alpha2.AgentHarnessChannelAccessOpen
}
