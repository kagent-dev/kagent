package valuesource

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestResolve(t *testing.T) {
	const namespace, name, presentKey, emptyKey, value = "team", "source", "present", "empty", "example"
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	objects := []client.Object{
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}, Data: map[string][]byte{presentKey: []byte(value), emptyKey: {}}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}, Data: map[string]string{presentKey: value, emptyKey: ""}},
	}
	for _, sourceType := range []v1alpha3.ValueSourceType{v1alpha3.SecretValueSource, v1alpha3.ConfigMapValueSource} {
		t.Run(string(sourceType), func(t *testing.T) {
			kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			for _, scenario := range []struct {
				key, expected string
			}{{presentKey, value}, {emptyKey, ""}} {
				t.Run("Given an existing "+scenario.key+" key, when resolving its reference, assert that its value is returned", func(t *testing.T) {
					resolved, err := Resolve(t.Context(), kube, namespace, v1alpha3.ValueSource{Type: sourceType, Name: name, Key: scenario.key})
					require.NoError(t, err)
					require.Equal(t, scenario.expected, resolved)
				})
			}
			t.Run("Given a missing key, when resolving its reference, assert that the error identifies a key failure without exposing values", func(t *testing.T) {
				_, err := Resolve(t.Context(), kube, namespace, v1alpha3.ValueSource{Type: sourceType, Name: name, Key: "absent"})
				var keyError *KeyNotFoundError
				require.ErrorAs(t, fmt.Errorf("resolve reference: %w", err), &keyError)
				require.EqualError(t, err, "key absent not found in "+string(sourceType)+" "+namespace+"/"+name)
				require.NotContains(t, err.Error(), value)
				require.NotNil(t, errors.Unwrap(keyError))
			})
			t.Run("Given an object in another namespace, when resolving its reference, assert that the Kubernetes not-found error is preserved", func(t *testing.T) {
				_, err := Resolve(t.Context(), kube, "other", v1alpha3.ValueSource{Type: sourceType, Name: name, Key: presentKey})
				require.True(t, apierrors.IsNotFound(err))
				var keyError *KeyNotFoundError
				require.NotErrorAs(t, err, &keyError)
			})
		})
	}
}

func TestResolveReadFailure(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	readError := errors.New("temporarily unavailable")
	kube := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			return readError
		},
	}).Build()
	for _, sourceType := range []v1alpha3.ValueSourceType{v1alpha3.SecretValueSource, v1alpha3.ConfigMapValueSource} {
		t.Run("Given an unavailable "+string(sourceType)+" reader, when resolving a reference, assert that its cause is preserved", func(t *testing.T) {
			_, err := Resolve(t.Context(), kube, "team", v1alpha3.ValueSource{Type: sourceType, Name: "source", Key: "key"})
			require.ErrorIs(t, err, readError)
			var keyError *KeyNotFoundError
			require.NotErrorAs(t, err, &keyError)
		})
	}
}
