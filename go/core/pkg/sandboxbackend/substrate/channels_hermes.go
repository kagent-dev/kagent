package substrate

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/channel_helpers"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// buildHermesChannelEnv translates AgentHarness channels into the unsuffixed
// env-var contract that the Hermes messaging gateway auto-detects
// (SLACK_BOT_TOKEN, TELEGRAM_BOT_TOKEN, ...). Hermes supports a single account
// per platform, so a second channel of the same type is rejected.
func buildHermesChannelEnv(ctx context.Context, kube client.Client, namespace string, chs []v1alpha2.AgentHarnessChannel) ([]corev1.EnvVar, error) {
	var env []corev1.EnvVar
	var telegramSeen, slackSeen bool
	for _, ch := range chs {
		switch ch.Type {
		case v1alpha2.AgentHarnessChannelTypeTelegram:
			if telegramSeen {
				return nil, fmt.Errorf("hermes supports at most one telegram channel, found a second %q", ch.Name)
			}
			telegramSeen = true
			e, err := hermesTelegramEnv(ctx, kube, namespace, ch)
			if err != nil {
				return nil, err
			}
			env = append(env, e...)
		case v1alpha2.AgentHarnessChannelTypeSlack:
			if slackSeen {
				return nil, fmt.Errorf("hermes supports at most one slack channel, found a second %q", ch.Name)
			}
			slackSeen = true
			e, err := hermesSlackEnv(ctx, kube, namespace, ch)
			if err != nil {
				return nil, err
			}
			env = append(env, e...)
		default:
			return nil, unsupportedHermesChannelType(ch.Name, ch.Type)
		}
	}
	return env, nil
}

func hermesTelegramEnv(ctx context.Context, kube client.Client, namespace string, ch v1alpha2.AgentHarnessChannel) ([]corev1.EnvVar, error) {
	spec := ch.Telegram
	if spec == nil {
		return nil, fmt.Errorf("channel %q: telegram spec is required", ch.Name)
	}
	botEnv, err := channel_helpers.CredentialContainerEnv(spec.BotToken, "TELEGRAM_BOT_TOKEN")
	if err != nil {
		return nil, fmt.Errorf("channel %q telegram bot token: %w", ch.Name, err)
	}
	out := []corev1.EnvVar{botEnv}
	allow, err := channel_helpers.ResolveAllowedUserIDs(ctx, kube, namespace, spec.AllowedUserIDs, spec.AllowedUserIDsFrom)
	if err != nil {
		return nil, fmt.Errorf("channel %q telegram allowed users: %w", ch.Name, err)
	}
	if len(allow) > 0 {
		out = append(out, corev1.EnvVar{Name: "TELEGRAM_ALLOWED_USERS", Value: strings.Join(allow, ",")})
	}
	return out, nil
}

func hermesSlackEnv(ctx context.Context, kube client.Client, namespace string, ch v1alpha2.AgentHarnessChannel) ([]corev1.EnvVar, error) {
	spec := ch.Slack
	if spec == nil {
		return nil, fmt.Errorf("channel %q: slack spec is required", ch.Name)
	}
	botEnv, err := channel_helpers.CredentialContainerEnv(spec.BotToken, "SLACK_BOT_TOKEN")
	if err != nil {
		return nil, fmt.Errorf("channel %q slack bot token: %w", ch.Name, err)
	}
	appEnv, err := channel_helpers.CredentialContainerEnv(spec.AppToken, "SLACK_APP_TOKEN")
	if err != nil {
		return nil, fmt.Errorf("channel %q slack app token: %w", ch.Name, err)
	}
	out := []corev1.EnvVar{botEnv, appEnv}
	if opts := spec.Hermes; opts != nil {
		allow, err := channel_helpers.ResolveAllowedUserIDs(ctx, kube, namespace, opts.AllowedUserIDs, opts.AllowedUserIDsFrom)
		if err != nil {
			return nil, fmt.Errorf("channel %q slack allowed users: %w", ch.Name, err)
		}
		if len(allow) > 0 {
			out = append(out, corev1.EnvVar{Name: "SLACK_ALLOWED_USERS", Value: strings.Join(allow, ",")})
		}
		if v := strings.TrimSpace(opts.HomeChannel); v != "" {
			out = append(out, corev1.EnvVar{Name: "SLACK_HOME_CHANNEL", Value: v})
		}
		if v := strings.TrimSpace(opts.HomeChannelName); v != "" {
			out = append(out, corev1.EnvVar{Name: "SLACK_HOME_CHANNEL_NAME", Value: v})
		}
	}
	return out, nil
}

func unsupportedHermesChannelType(name string, typ v1alpha2.AgentHarnessChannelType) error {
	return fmt.Errorf("channel %q: unsupported hermes channel type %q", name, typ)
}
