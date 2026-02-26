package utils

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetConfigMapValue fetches a value from a ConfigMap
func GetConfigMapValue(ctx context.Context, c client.Client, ref client.ObjectKey, key string) (string, error) {
	configMap := &corev1.ConfigMap{}
	err := c.Get(ctx, ref, configMap)
	if err != nil {
		return "", fmt.Errorf("failed to find ConfigMap for %s: %v", ref.String(), err)
	}

	value, exists := configMap.Data[key]
	if !exists {
		return "", fmt.Errorf("key %s not found in ConfigMap %s", key, ref)
	}
	return value, nil
}
