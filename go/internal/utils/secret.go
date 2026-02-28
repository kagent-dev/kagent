package utils

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetSecretValue fetches a value from a Secret
func GetSecretValue(ctx context.Context, c client.Client, ref client.ObjectKey, key string) (string, error) {
	secret := &corev1.Secret{}
	err := c.Get(ctx, ref, secret)
	if err != nil {
		return "", fmt.Errorf("failed to find Secret %s: %v", ref.String(), err)
	}

	value, exists := secret.Data[key]
	if !exists {
		return "", fmt.Errorf("key %s not found in Secret %s", key, ref.String())
	}
	return string(value), nil
}
