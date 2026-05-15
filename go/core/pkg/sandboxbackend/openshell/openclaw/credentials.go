package openclaw

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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

func putChannelCredential(ctx context.Context, kube client.Client, namespace string, cred v1alpha2.AgentHarnessChannelCredential, envKey string, env map[string]string) error {
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
// Channel configs must use literals in openclaw.json: OpenClaw's Bot API clients build URLs from botToken before
// openshell:resolve:env: placeholders are expanded.
func resolvedChannelSecret(env map[string]string, envKey string) (string, error) {
	v := strings.TrimSpace(env[envKey])
	if v == "" {
		return "", fmt.Errorf("credential %s is missing or empty after resolve", envKey)
	}
	return v, nil
}
