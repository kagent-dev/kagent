package substrate

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GatewayTokenSecretKey is the Secret data key used for per-harness OpenClaw gateway tokens.
const GatewayTokenSecretKey = "token"

// ResolveGatewayToken returns the per-harness gateway token.
// Token source is validated at admission via AgentHarnessSubstrateSpec CEL rules.
func ResolveGatewayToken(ctx context.Context, kube client.Client, ah *v1alpha2.AgentHarness) (string, error) {
	if ah == nil || ah.Spec.Substrate == nil {
		return "", fmt.Errorf("spec.substrate is required")
	}
	sub := ah.Spec.Substrate
	if sub.GatewayTokenSecretRef != nil && strings.TrimSpace(sub.GatewayTokenSecretRef.Name) != "" {
		return resolveGatewayTokenSecret(ctx, kube, ah.Namespace, sub.GatewayTokenSecretRef)
	}
	return strings.TrimSpace(sub.GatewayToken), nil
}

func resolveGatewayTokenSecret(ctx context.Context, kube client.Client, namespace string, ref *v1alpha2.TypedLocalReference) (string, error) {
	if kube == nil {
		return "", fmt.Errorf("kubernetes client is required to resolve gateway token secret")
	}
	var secret corev1.Secret
	if err := kube.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, &secret); err != nil {
		return "", fmt.Errorf("get gateway token secret %s/%s: %w", namespace, ref.Name, err)
	}
	if secret.Data == nil {
		return "", fmt.Errorf("gateway token secret %s/%s is empty", namespace, ref.Name)
	}
	val, ok := secret.Data[GatewayTokenSecretKey]
	if !ok {
		return "", fmt.Errorf("gateway token secret %s/%s missing key %q", namespace, ref.Name, GatewayTokenSecretKey)
	}
	token := strings.TrimSpace(string(val))
	if token == "" {
		return "", fmt.Errorf("gateway token secret %s/%s key %q must not be empty", namespace, ref.Name, GatewayTokenSecretKey)
	}
	return token, nil
}
