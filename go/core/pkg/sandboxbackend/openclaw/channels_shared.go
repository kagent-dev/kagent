package openclaw

import (
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
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

func openClawSlackOptions(spec *v1alpha2.AgentHarnessSlackChannelSpec) *v1alpha2.AgentHarnessOpenClawSlackOptions {
	if spec == nil || spec.OpenClaw == nil {
		return &v1alpha2.AgentHarnessOpenClawSlackOptions{}
	}
	return spec.OpenClaw
}

func slackInteractiveReplies(opts *v1alpha2.AgentHarnessOpenClawSlackOptions) bool {
	if opts == nil || opts.InteractiveReplies == nil {
		return true
	}
	return *opts.InteractiveReplies
}

func openClawSlackChannelAccess(opts *v1alpha2.AgentHarnessOpenClawSlackOptions) v1alpha2.AgentHarnessChannelAccess {
	if opts == nil || opts.ChannelAccess == "" {
		return v1alpha2.AgentHarnessChannelAccessOpen
	}
	return opts.ChannelAccess
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

func unsupportedChannelType(name string, typ v1alpha2.AgentHarnessChannelType) error {
	return fmt.Errorf("channel %q: unsupported type %q", name, typ)
}
