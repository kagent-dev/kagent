package substrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolvePodEnv_LocalSecretStringData(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	kube := fake.NewClientBuilder().WithScheme(scheme).Build()

	localSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "kagent"},
		StringData: map[string]string{"config.json": `{"a":1}`},
	}
	env := []corev1.EnvVar{{
		Name: "CONFIG",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "cfg"},
				Key:                  "config.json",
			},
		},
	}}

	resolved, err := resolvePodEnv(context.Background(), kube, "kagent", env, localSecret)
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	require.Nil(t, resolved[0].ValueFrom)
	require.Equal(t, `{"a":1}`, resolved[0].Value)
}

func TestResolvePodEnv_LocalSecretDataTakesPrecedence(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	kube := fake.NewClientBuilder().WithScheme(scheme).Build()

	localSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "kagent"},
		Data:       map[string][]byte{"key": []byte("from-data")},
		StringData: map[string]string{"key": "from-stringdata"},
	}
	env := []corev1.EnvVar{{
		Name: "VALUE",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "cfg"},
				Key:                  "key",
			},
		},
	}}

	resolved, err := resolvePodEnv(context.Background(), kube, "kagent", env, localSecret)
	require.NoError(t, err)
	require.Equal(t, "from-data", resolved[0].Value)
}

func TestResolvePodEnv_MissingKeyRequired(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	kube := fake.NewClientBuilder().WithScheme(scheme).Build()

	localSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "kagent"},
		StringData: map[string]string{"other": "value"},
	}
	env := []corev1.EnvVar{{
		Name: "MISSING",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "cfg"},
				Key:                  "does-not-exist",
			},
		},
	}}

	_, err := resolvePodEnv(context.Background(), kube, "kagent", env, localSecret)
	require.Error(t, err)
}
