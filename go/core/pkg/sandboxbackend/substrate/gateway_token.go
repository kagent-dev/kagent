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

// ValidateGatewayTokenSpec requires exactly one per-harness OpenClaw gateway token source.
func ValidateGatewayTokenSpec(sub *v1alpha2.AgentHarnessSubstrateSpec) error {
	if sub == nil {
		return fmt.Errorf("spec.substrate is required")
	}
	hasToken := strings.TrimSpace(sub.GatewayToken) != ""
	hasSecretRef := sub.GatewayTokenSecretRef != nil && strings.TrimSpace(sub.GatewayTokenSecretRef.Name) != ""
	if hasToken == hasSecretRef {
		return fmt.Errorf("exactly one of spec.substrate.gatewayToken or gatewayTokenSecretRef must be specified")
	}
	return nil
}

// ResolveGatewayToken returns the per-harness gateway token.
func ResolveGatewayToken(ctx context.Context, kube client.Client, ah *v1alpha2.AgentHarness) (string, error) {
	if ah == nil || ah.Spec.Substrate == nil {
		return "", fmt.Errorf("spec.substrate is required")
	}
	if err := ValidateGatewayTokenSpec(ah.Spec.Substrate); err != nil {
		return "", err
	}
	sub := ah.Spec.Substrate
	if sub.GatewayTokenSecretRef != nil {
		return resolveGatewayTokenSecret(ctx, kube, ah.Namespace, sub.GatewayTokenSecretRef)
	}
	return strings.TrimSpace(sub.GatewayToken), nil
}

func resolveGatewayTokenSecret(ctx context.Context, kube client.Client, defaultNamespace string, ref *v1alpha2.TypedReference) (string, error) {
	if kube == nil {
		return "", fmt.Errorf("kubernetes client is required to resolve gateway token secret")
	}
	ns := ref.Namespace
	if ns == "" {
		ns = defaultNamespace
	}
	var secret corev1.Secret
	if err := kube.Get(ctx, types.NamespacedName{Namespace: ns, Name: ref.Name}, &secret); err != nil {
		return "", fmt.Errorf("get gateway token secret %s/%s: %w", ns, ref.Name, err)
	}
	if secret.Data == nil {
		return "", fmt.Errorf("gateway token secret %s/%s is empty", ns, ref.Name)
	}
	val, ok := secret.Data[GatewayTokenSecretKey]
	if !ok {
		return "", fmt.Errorf("gateway token secret %s/%s missing key %q", ns, ref.Name, GatewayTokenSecretKey)
	}
	return strings.TrimSpace(string(val)), nil
}
