package utils

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetSecretData fetches all data from a Secret, converting byte values to strings.
func GetSecretData(ctx context.Context, c client.Client, ref client.ObjectKey) (map[string]string, error) {
	secret := &corev1.Secret{}
	if err := c.Get(ctx, ref, secret); err != nil {
		return nil, fmt.Errorf("failed to find Secret %s: %v", ref.String(), err)
	}
	data := make(map[string]string, len(secret.Data))
	for k, v := range secret.Data {
		data[k] = string(v)
	}
	return data, nil
}

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
