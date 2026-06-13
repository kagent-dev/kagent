package substrate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// GatewayTokenSecretKey is the Secret data key used for per-harness OpenClaw gateway tokens.
const GatewayTokenSecretKey = "token"

// ManagedGatewayTokenSecretName returns the name of the Secret the controller
// auto-creates to hold a generated gateway token when the harness does not
// specify one inline (gatewayToken) or via gatewayTokenSecretRef.
func ManagedGatewayTokenSecretName(ah *v1alpha2.AgentHarness) string {
	return ah.Name + "-gateway-token"
}

// ResolveGatewayToken returns the per-harness gateway token.
// Precedence: gatewayTokenSecretRef, then inline gatewayToken, then the
// controller-managed Secret holding an auto-generated token.
func ResolveGatewayToken(ctx context.Context, kube client.Client, ah *v1alpha2.AgentHarness) (string, error) {
	if ah == nil || ah.Spec.Substrate == nil {
		return "", fmt.Errorf("spec.substrate is required")
	}
	sub := ah.Spec.Substrate
	if sub.GatewayTokenSecretRef != nil && strings.TrimSpace(sub.GatewayTokenSecretRef.Name) != "" {
		return resolveGatewayTokenSecret(ctx, kube, ah.Namespace, sub.GatewayTokenSecretRef)
	}
	if t := strings.TrimSpace(sub.GatewayToken); t != "" {
		return t, nil
	}
	// No token configured on the spec: fall back to the controller-managed
	// Secret. EnsureManagedGatewayToken creates it during reconciliation.
	ref := &v1alpha2.TypedLocalReference{Name: ManagedGatewayTokenSecretName(ah)}
	return resolveGatewayTokenSecret(ctx, kube, ah.Namespace, ref)
}

// EnsureManagedGatewayToken makes sure a gateway token exists for the harness.
// When the spec provides a token (inline or via gatewayTokenSecretRef) it is a
// no-op. Otherwise it creates a controller-owned Secret holding a randomly
// generated token exactly once, so the same token is reused across reconciles
// and can be retrieved later via
// `kubectl get secret <harness-name>-gateway-token`.
func (p *Lifecycle) EnsureManagedGatewayToken(ctx context.Context, ah *v1alpha2.AgentHarness) error {
	if ah == nil || ah.Spec.Substrate == nil {
		return fmt.Errorf("spec.substrate is required")
	}
	if p.Client == nil {
		return fmt.Errorf("substrate lifecycle kubernetes client is required")
	}
	sub := ah.Spec.Substrate
	if strings.TrimSpace(sub.GatewayToken) != "" {
		return nil
	}
	if sub.GatewayTokenSecretRef != nil && strings.TrimSpace(sub.GatewayTokenSecretRef.Name) != "" {
		return nil
	}

	key := types.NamespacedName{Namespace: ah.Namespace, Name: ManagedGatewayTokenSecretName(ah)}
	var existing corev1.Secret
	if err := p.Client.Get(ctx, key, &existing); err == nil {
		if len(strings.TrimSpace(string(existing.Data[GatewayTokenSecretKey]))) > 0 {
			return nil // already provisioned
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get managed gateway token secret %s: %w", key, err)
	}

	token, err := generateGatewayToken()
	if err != nil {
		return fmt.Errorf("generate gateway token: %w", err)
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
			Labels:    lifecycleLabels(ah),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{GatewayTokenSecretKey: []byte(token)},
	}
	if err := controllerutil.SetControllerReference(ah, secret, p.Client.Scheme()); err != nil {
		return fmt.Errorf("set managed gateway token secret owner ref: %w", err)
	}
	if err := p.Client.Create(ctx, secret); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("create managed gateway token secret %s: %w", key, err)
	}
	return nil
}

// generateGatewayToken returns a 256-bit random token as a hex string.
func generateGatewayToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(buf), nil
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
