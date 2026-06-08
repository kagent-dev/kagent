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
	AllowedUserIDs     []string
	HomeChannel        string
	HomeChannelName    string
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

// Resolve reads AgentHarness channels, populates per-channel credential env keys in Secrets,
// and returns structured account metadata for Hermes/OpenClaw bootstrap.
func Resolve(ctx context.Context, kube client.Client, namespace string, backend v1alpha2.AgentHarnessBackendType, channels []v1alpha2.AgentHarnessChannel) (*Resolved, error) {
	r := &Resolved{Secrets: map[string]string{}}
	seenNames := make(map[string]struct{}, len(channels))
	for _, ch := range channels {
		name := strings.TrimSpace(ch.Name)
		if name == "" {
			continue
		}
		if _, dup := seenNames[name]; dup {
			return nil, fmt.Errorf("channel %q: duplicate binding name", name)
		}
		seenNames[name] = struct{}{}
		switch ch.Type {
		case v1alpha2.AgentHarnessChannelTypeTelegram:
			if err := r.addTelegram(ctx, kube, namespace, ch); err != nil {
				return nil, err
			}
		case v1alpha2.AgentHarnessChannelTypeSlack:
			if err := r.addSlackChannel(ctx, kube, namespace, backend, ch); err != nil {
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
	if err := PutChannelCredential(ctx, kube, namespace, spec.BotToken, TelegramBotTokenEnvKey(ch.Name), r.Secrets); err != nil {
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

func (r *Resolved) addSlackChannel(ctx context.Context, kube client.Client, namespace string, backend v1alpha2.AgentHarnessBackendType, ch v1alpha2.AgentHarnessChannel) error {
	spec := ch.Slack
	if spec == nil {
		return fmt.Errorf("channel %q: slack spec is required", ch.Name)
	}
	switch backend {
	case v1alpha2.AgentHarnessBackendHermes:
		opts := spec.Hermes
		if opts == nil {
			opts = &v1alpha2.AgentHarnessHermesSlackOptions{}
		}
		return r.addHermesSlack(ctx, kube, namespace, ch.Name, spec.BotToken, spec.AppToken, opts)
	case v1alpha2.AgentHarnessBackendOpenClaw, v1alpha2.AgentHarnessBackendNemoClaw:
		opts := spec.OpenClaw
		if opts == nil {
			opts = &v1alpha2.AgentHarnessOpenClawSlackOptions{}
		}
		return r.addOpenClawSlack(ctx, kube, namespace, ch.Name, spec.BotToken, spec.AppToken, opts)
	default:
		return fmt.Errorf("channel %q: slack channels are not supported for backend %q", ch.Name, backend)
	}
}

func (r *Resolved) putSlackCredentials(ctx context.Context, kube client.Client, namespace, channelName string, botToken, appToken v1alpha2.AgentHarnessChannelCredential) error {
	if err := PutChannelCredential(ctx, kube, namespace, botToken, SlackBotTokenEnvKey(channelName), r.Secrets); err != nil {
		return fmt.Errorf("channel %q slack bot token: %w", channelName, err)
	}
	if err := PutChannelCredential(ctx, kube, namespace, appToken, SlackAppTokenEnvKey(channelName), r.Secrets); err != nil {
		return fmt.Errorf("channel %q slack app token: %w", channelName, err)
	}
	return nil
}

func (r *Resolved) addOpenClawSlack(ctx context.Context, kube client.Client, namespace, channelName string, botToken, appToken v1alpha2.AgentHarnessChannelCredential, opts *v1alpha2.AgentHarnessOpenClawSlackOptions) error {
	if err := r.putSlackCredentials(ctx, kube, namespace, channelName, botToken, appToken); err != nil {
		return err
	}
	interactive := true
	if opts.InteractiveReplies != nil {
		interactive = *opts.InteractiveReplies
	}
	access := opts.ChannelAccess
	if access == "" {
		access = v1alpha2.AgentHarnessChannelAccessOpen
	}
	r.HasSlack = true
	r.Slack = append(r.Slack, SlackAccount{
		Name:               channelName,
		ChannelAccess:      access,
		AllowlistChannels:  TrimNonEmptyStrings(opts.AllowlistChannels),
		InteractiveReplies: interactive,
	})
	if !r.slackSeen {
		r.slackRootPolicy = access
		r.slackSeen = true
	}
	return nil
}

func (r *Resolved) addHermesSlack(ctx context.Context, kube client.Client, namespace, channelName string, botToken, appToken v1alpha2.AgentHarnessChannelCredential, opts *v1alpha2.AgentHarnessHermesSlackOptions) error {
	if err := r.putSlackCredentials(ctx, kube, namespace, channelName, botToken, appToken); err != nil {
		return err
	}
	allow, err := HermesSlackAllowedUsers(ctx, kube, namespace, opts)
	if err != nil {
		return fmt.Errorf("channel %q slack allowed users: %w", channelName, err)
	}
	homeChannel := strings.TrimSpace(opts.HomeChannel)
	homeChannelName := strings.TrimSpace(opts.HomeChannelName)
	r.HasSlack = true
	if len(allow) > 0 {
		r.SlackAllow = append(r.SlackAllow, allow...)
	}
	r.Slack = append(r.Slack, SlackAccount{
		Name:               channelName,
		ChannelAccess:      v1alpha2.AgentHarnessChannelAccessOpen,
		AllowedUserIDs:     allow,
		HomeChannel:        homeChannel,
		HomeChannelName:    homeChannelName,
		InteractiveReplies: true,
	})
	if r.SlackHomeChannel == "" && homeChannel != "" {
		r.SlackHomeChannel = homeChannel
		r.SlackHomeChannelName = homeChannelName
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
