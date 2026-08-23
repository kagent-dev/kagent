package substrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestResolvePodEnvScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	return scheme
}

func secretKeyEnvVar(name, secretName, key string, optional *bool) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  key,
				Optional:             optional,
			},
		},
	}
}

func TestResolvePodEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		namespace   string
		env         []corev1.EnvVar
		localSecret *corev1.Secret
		kubeObjects []client.Object
		wantErr     string
		check       func(t *testing.T, got []corev1.EnvVar)
	}{
		{
			name:      "literal value and non-secret ValueFrom pass through unchanged",
			namespace: "kagent",
			env: []corev1.EnvVar{
				{Name: "LITERAL", Value: "ok"},
				{
					Name: "POD_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					},
				},
			},
			check: func(t *testing.T, got []corev1.EnvVar) {
				require.Equal(t, "ok", got[0].Value)
				require.Nil(t, got[0].ValueFrom)
				require.NotNil(t, got[1].ValueFrom)
				require.Equal(t, "metadata.name", got[1].ValueFrom.FieldRef.FieldPath)
			},
		},
		{
			name:      "key present in localSecret Data resolves as before",
			namespace: "kagent",
			env: []corev1.EnvVar{
				secretKeyEnvVar("CONFIG", "sandbox-config", "config.json", nil),
			},
			localSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "sandbox-config", Namespace: "kagent"},
				Data:       map[string][]byte{"config.json": []byte(`{"foo":"bar"}`)},
			},
			check: func(t *testing.T, got []corev1.EnvVar) {
				require.Equal(t, `{"foo":"bar"}`, got[0].Value)
				require.Nil(t, got[0].ValueFrom)
			},
		},
		{
			name:      "key missing from Data but present in StringData falls back",
			namespace: "kagent",
			env: []corev1.EnvVar{
				secretKeyEnvVar("CONFIG", "sandbox-config", "config.json", nil),
			},
			localSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "sandbox-config", Namespace: "kagent"},
				StringData: map[string]string{"config.json": `{"foo":"baz"}`},
			},
			check: func(t *testing.T, got []corev1.EnvVar) {
				require.Equal(t, `{"foo":"baz"}`, got[0].Value)
				require.Nil(t, got[0].ValueFrom)
			},
		},
		{
			name:      "key missing from both Data and StringData, Optional=true clears ValueFrom without error",
			namespace: "kagent",
			env: []corev1.EnvVar{
				secretKeyEnvVar("CONFIG", "sandbox-config", "config.json", new(true)),
			},
			localSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "sandbox-config", Namespace: "kagent"},
			},
			check: func(t *testing.T, got []corev1.EnvVar) {
				require.Nil(t, got[0].ValueFrom)
				require.Empty(t, got[0].Value)
			},
		},
		{
			name:      "key missing from both, Optional nil errors",
			namespace: "kagent",
			env: []corev1.EnvVar{
				secretKeyEnvVar("CONFIG", "sandbox-config", "config.json", nil),
			},
			localSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "sandbox-config", Namespace: "kagent"},
			},
			wantErr: `secret "sandbox-config" does not contain key "config.json"`,
		},
		{
			name:      "key missing from both, Optional=false errors",
			namespace: "kagent",
			env: []corev1.EnvVar{
				secretKeyEnvVar("CONFIG", "sandbox-config", "config.json", new(false)),
			},
			localSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "sandbox-config", Namespace: "kagent"},
			},
			wantErr: `secret "sandbox-config" does not contain key "config.json"`,
		},
		{
			name:      "secret fetched from kube client when no matching localSecret",
			namespace: "kagent",
			env: []corev1.EnvVar{
				secretKeyEnvVar("API_KEY", "cluster-secret", "key", nil),
			},
			kubeObjects: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-secret", Namespace: "kagent"},
					Data:       map[string][]byte{"key": []byte("secret-value")},
				},
			},
			check: func(t *testing.T, got []corev1.EnvVar) {
				require.Equal(t, "secret-value", got[0].Value)
				require.Nil(t, got[0].ValueFrom)
			},
		},
		{
			name:      "localSecret name mismatch falls through to kube client lookup",
			namespace: "kagent",
			env: []corev1.EnvVar{
				secretKeyEnvVar("API_KEY", "cluster-secret", "key", nil),
			},
			localSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "other-secret", Namespace: "kagent"},
				Data:       map[string][]byte{"key": []byte("wrong-value")},
			},
			kubeObjects: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-secret", Namespace: "kagent"},
					Data:       map[string][]byte{"key": []byte("secret-value")},
				},
			},
			check: func(t *testing.T, got []corev1.EnvVar) {
				require.Equal(t, "secret-value", got[0].Value)
			},
		},
		{
			name:      "secret not found via kube client, Optional=true clears ValueFrom without error",
			namespace: "kagent",
			env: []corev1.EnvVar{
				secretKeyEnvVar("API_KEY", "missing-secret", "key", new(true)),
			},
			check: func(t *testing.T, got []corev1.EnvVar) {
				require.Nil(t, got[0].ValueFrom)
				require.Empty(t, got[0].Value)
			},
		},
		{
			name:      "secret not found via kube client, Optional nil errors",
			namespace: "kagent",
			env: []corev1.EnvVar{
				secretKeyEnvVar("API_KEY", "missing-secret", "key", nil),
			},
			wantErr: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scheme := newTestResolvePodEnvScheme(t)
			kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.kubeObjects...).Build()

			got, err := resolvePodEnv(context.Background(), kube, tt.namespace, tt.env, tt.localSecret)

			if tt.wantErr != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			tt.check(t, got)
		})
	}
}
