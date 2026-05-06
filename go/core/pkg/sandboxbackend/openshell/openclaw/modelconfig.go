package openclaw

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GatewayProviderRecordName returns the OpenShell / OpenClaw provider record id for a ModelConfig provider.
func GatewayProviderRecordName(provider v1alpha2.ModelProvider) string {
	return strings.ToLower(string(provider))
}

// ResolveModelConfigAPIKey reads the API key from the Secret referenced by ModelConfig.
func ResolveModelConfigAPIKey(ctx context.Context, kube client.Client, mc *v1alpha2.ModelConfig) (string, error) {
	if mc.Spec.APIKeyPassthrough {
		return "", fmt.Errorf("APIKeyPassthrough is not supported when registering an OpenShell gateway provider from ModelConfig")
	}
	if mc.Spec.APIKeySecret == "" || mc.Spec.APIKeySecretKey == "" {
		return "", fmt.Errorf("modelConfig %s/%s requires apiKeySecret and apiKeySecretKey", mc.Namespace, mc.Name)
	}
	sec := &corev1.Secret{}
	key := types.NamespacedName{Namespace: mc.Namespace, Name: mc.Spec.APIKeySecret}
	if err := kube.Get(ctx, key, sec); err != nil {
		return "", fmt.Errorf("get API key secret %q: %w", mc.Spec.APIKeySecret, err)
	}
	raw, ok := sec.Data[mc.Spec.APIKeySecretKey]
	if !ok || len(raw) == 0 {
		return "", fmt.Errorf("secret %q missing non-empty key %q", mc.Spec.APIKeySecret, mc.Spec.APIKeySecretKey)
	}
	return string(raw), nil
}
