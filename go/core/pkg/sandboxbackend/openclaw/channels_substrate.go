package openclaw

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// accumulateSubstrateHarnessChannels configures channels with OpenClaw env SecretRefs in openclaw.json
// and returns container env vars (inline value or Kubernetes valueFrom refs) for the ActorTemplate.
func accumulateSubstrateHarnessChannels(ctx context.Context, kube client.Client, namespace string, channels []v1alpha2.AgentHarnessChannel) (*harnessChannels, []corev1.EnvVar, error) {
	a := newHarnessChannels()
	var containerEnv []corev1.EnvVar
	for _, ch := range channels {
		switch ch.Type {
		case v1alpha2.AgentHarnessChannelTypeTelegram:
			env, err := a.addSubstrateTelegram(ctx, kube, namespace, ch)
			if err != nil {
				return nil, nil, err
			}
			containerEnv = append(containerEnv, env...)
		case v1alpha2.AgentHarnessChannelTypeSlack:
			env, err := a.addSubstrateSlack(ctx, kube, namespace, ch)
			if err != nil {
				return nil, nil, err
			}
			containerEnv = append(containerEnv, env...)
		default:
			return nil, nil, unsupportedChannelType(ch.Name, ch.Type)
		}
	}
	return a, containerEnv, nil
}

func (a *harnessChannels) addSubstrateTelegram(ctx context.Context, kube client.Client, namespace string, ch v1alpha2.AgentHarnessChannel) ([]corev1.EnvVar, error) {
	spec := ch.Telegram
	if spec == nil {
		return nil, fmt.Errorf("channel %q: telegram spec is required", ch.Name)
	}
	botEnv := channelSecretEnvVar(ch.Name, "TELEGRAM_BOT")
	botEnvVar, err := channelCredentialContainerEnv(spec.BotToken, botEnv)
	if err != nil {
		return nil, fmt.Errorf("channel %q telegram bot token: %w", ch.Name, err)
	}
	allowFrom, err := telegramAllowFrom(ctx, kube, namespace, spec)
	if err != nil {
		return nil, fmt.Errorf("channel %q telegram allowlist: %w", ch.Name, err)
	}
	ref := openclawEnvSecretRef(botEnv)
	acc := telegramAccount{
		Name:     ch.Name,
		Enabled:  true,
		BotToken: credentialValue{envSecret: &ref},
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
	return []corev1.EnvVar{botEnvVar}, nil
}

func (a *harnessChannels) addSubstrateSlack(ctx context.Context, kube client.Client, namespace string, ch v1alpha2.AgentHarnessChannel) ([]corev1.EnvVar, error) {
	spec := ch.Slack
	if spec == nil {
		return nil, fmt.Errorf("channel %q: slack spec is required", ch.Name)
	}
	botEnv := channelSecretEnvVar(ch.Name, "SLACK_BOT")
	appEnv := channelSecretEnvVar(ch.Name, "SLACK_APP")
	botEnvVar, err := channelCredentialContainerEnv(spec.BotToken, botEnv)
	if err != nil {
		return nil, fmt.Errorf("channel %q slack bot token: %w", ch.Name, err)
	}
	appEnvVar, err := channelCredentialContainerEnv(spec.AppToken, appEnv)
	if err != nil {
		return nil, fmt.Errorf("channel %q slack app token: %w", ch.Name, err)
	}
	botRef := openclawEnvSecretRef(botEnv)
	appRef := openclawEnvSecretRef(appEnv)
	opts := openClawSlackOptions(spec)
	access := openClawSlackChannelAccess(opts)
	acc := slackAccount{
		Name:              ch.Name,
		Enabled:           true,
		Mode:              "socket",
		BotToken:          credentialValue{envSecret: &botRef},
		AppToken:          credentialValue{envSecret: &appRef},
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
	return []corev1.EnvVar{botEnvVar, appEnvVar}, nil
}
