package valuesource

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// KeyNotFoundError reports a missing key in an existing Secret or ConfigMap.
type KeyNotFoundError struct {
	err error
}

func (e *KeyNotFoundError) Error() string { return e.err.Error() }

func (e *KeyNotFoundError) Unwrap() error { return e.err }

// Resolve loads a value from a Secret or ConfigMap in the supplied namespace.
func Resolve(ctx context.Context, kube client.Reader, namespace string, source v1alpha3.ValueSource) (string, error) {
	name := types.NamespacedName{Namespace: namespace, Name: source.Name}
	switch source.Type {
	case v1alpha3.SecretValueSource:
		secret := &corev1.Secret{}
		if err := kube.Get(ctx, name, secret); err != nil {
			return "", fmt.Errorf("failed to get Secret %s: %w", name, err)
		}
		value, found := secret.Data[source.Key]
		if !found {
			return "", &KeyNotFoundError{err: fmt.Errorf("key %s not found in Secret %s", source.Key, name)}
		}
		return string(value), nil
	case v1alpha3.ConfigMapValueSource:
		configMap := &corev1.ConfigMap{}
		if err := kube.Get(ctx, name, configMap); err != nil {
			return "", fmt.Errorf("failed to get ConfigMap %s: %w", name, err)
		}
		value, found := configMap.Data[source.Key]
		if !found {
			return "", &KeyNotFoundError{err: fmt.Errorf("key %s not found in ConfigMap %s", source.Key, name)}
		}
		return value, nil
	default:
		return "", fmt.Errorf("unknown value source type %q", source.Type)
	}
}
