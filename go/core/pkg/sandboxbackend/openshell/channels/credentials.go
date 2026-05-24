package channels

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PutChannelCredential resolves a channel credential into env[envKey].
func PutChannelCredential(ctx context.Context, kube client.Client, namespace string, cred v1alpha2.AgentHarnessChannelCredential, envKey string, env map[string]string) error {
	var v string
	if strings.TrimSpace(cred.Value) != "" {
		v = strings.TrimSpace(cred.Value)
	} else if cred.ValueFrom == nil {
		return fmt.Errorf("channel credential requires value or valueFrom")
	} else {
		var err error
		v, err = cred.ValueFrom.Resolve(ctx, kube, namespace)
		if err != nil {
			return fmt.Errorf("resolve credential %s: %w", envKey, err)
		}
	}
	if prev, ok := env[envKey]; ok && prev != v {
		return fmt.Errorf("env %s already set to a different value (duplicate channel binding?)", envKey)
	}
	env[envKey] = v
	return nil
}

// TelegramAllowFrom returns allowed Telegram user IDs from the channel spec.
func TelegramAllowFrom(ctx context.Context, kube client.Client, namespace string, spec *v1alpha2.AgentHarnessTelegramChannelSpec) ([]string, error) {
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
		return SplitAllowedList(raw), nil
	}
	return nil, nil
}

// HermesSlackAllowedUsers returns allowed Slack member IDs from the Hermes channel spec (SLACK_ALLOWED_USERS).
func HermesSlackAllowedUsers(ctx context.Context, kube client.Client, namespace string, opts *v1alpha2.AgentHarnessHermesSlackOptions) ([]string, error) {
	if len(opts.AllowedUserIDs) > 0 {
		out := make([]string, 0, len(opts.AllowedUserIDs))
		for _, id := range opts.AllowedUserIDs {
			s := strings.TrimSpace(id)
			if s != "" {
				out = append(out, s)
			}
		}
		return out, nil
	}
	if opts.AllowedUserIDsFrom != nil {
		raw, err := opts.AllowedUserIDsFrom.Resolve(ctx, kube, namespace)
		if err != nil {
			return nil, fmt.Errorf("resolve allowedUserIDsFrom: %w", err)
		}
		return SplitAllowedList(raw), nil
	}
	return nil, nil
}

// SplitAllowedList parses comma/newline/semicolon-separated ID lists.
func SplitAllowedList(raw string) []string {
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

// TrimNonEmptyStrings returns trimmed non-empty strings from ss.
func TrimNonEmptyStrings(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
