// Package channel_helpers holds backend-agnostic helpers for translating
// AgentHarness channel (Slack/Telegram) credentials and allowlists into
// Substrate ActorTemplate container env vars. Both the OpenClaw and Hermes
// substrate paths reuse these primitives; only the env var naming and config
// shape differ per backend.
package channel_helpers

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CredentialContainerEnv maps a harness channel credential to an ActorTemplate
// env var. Inline values use env.Value; Secret/ConfigMap sources stay as
// valueFrom refs resolved by Substrate ate-api at resume (never inlined).
func CredentialContainerEnv(cred v1alpha2.AgentHarnessChannelCredential, envKey string) (corev1.EnvVar, error) {
	if v := strings.TrimSpace(cred.Value); v != "" {
		return corev1.EnvVar{Name: envKey, Value: v}, nil
	}
	if cred.ValueFrom == nil {
		return corev1.EnvVar{}, fmt.Errorf("channel credential requires value or valueFrom")
	}
	switch cred.ValueFrom.Type {
	case v1alpha2.SecretValueSource:
		return corev1.EnvVar{
			Name: envKey,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: cred.ValueFrom.Name},
					Key:                  cred.ValueFrom.Key,
				},
			},
		}, nil
	case v1alpha2.ConfigMapValueSource:
		return corev1.EnvVar{
			Name: envKey,
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: cred.ValueFrom.Name},
					Key:                  cred.ValueFrom.Key,
				},
			},
		}, nil
	default:
		return corev1.EnvVar{}, fmt.Errorf("unknown value source type %q", cred.ValueFrom.Type)
	}
}

// ResolveAllowedUserIDs returns the explicit allowlist IDs (trimmed) when set,
// otherwise resolves and splits the value referenced by from. Returns nil when
// neither is provided.
func ResolveAllowedUserIDs(ctx context.Context, kube client.Client, namespace string, ids []string, from *v1alpha2.ValueSource) ([]string, error) {
	if len(ids) > 0 {
		return TrimNonEmpty(ids), nil
	}
	if from != nil {
		raw, err := from.Resolve(ctx, kube, namespace)
		if err != nil {
			return nil, fmt.Errorf("resolve allowedUserIDsFrom: %w", err)
		}
		return SplitList(raw), nil
	}
	return nil, nil
}

// SplitList splits a comma/newline/semicolon-separated string into trimmed,
// non-empty entries.
func SplitList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == ';'
	}) {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// TrimNonEmpty trims each string and drops the empties.
func TrimNonEmpty(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
