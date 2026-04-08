package utils

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetMetadataValue looks up an unprefixed key in A2A metadata, checking
// "adk_<key>" first then falling back to "kagent_<key>".  This allows
// interoperability with upstream ADK (adk_ prefix) while preserving
// backward-compatibility with kagent's own kagent_ prefix.
func GetMetadataValue(metadata map[string]any, key string) (any, bool) {
	if metadata == nil {
		return nil, false
	}
	if v, ok := metadata["adk_"+key]; ok {
		return v, true
	}
	if v, ok := metadata["kagent_"+key]; ok {
		return v, true
	}
	return nil, false
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
