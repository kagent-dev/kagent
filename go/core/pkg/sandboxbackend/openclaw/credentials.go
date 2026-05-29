package openclaw

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
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

// channelCredentialContainerEnv maps a harness channel credential to an ActorTemplate env var.
// Inline values use env.Value; Secret/ConfigMap sources use valueFrom refs resolved by Substrate ate-api at resume.
func channelCredentialContainerEnv(cred v1alpha2.AgentHarnessChannelCredential, envKey string) (corev1.EnvVar, error) {
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
