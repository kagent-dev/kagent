package controller

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestModelProviderConfigReferencesSecret(t *testing.T) {
	tests := []struct {
		name      string
		mpc       *v1alpha2.ModelProviderConfig
		secretObj types.NamespacedName
		want      bool
	}{
		{
			name: "matching secret ref",
			mpc: &v1alpha2.ModelProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openai-provider",
					Namespace: "kagent",
				},
				Spec: v1alpha2.ModelProviderConfigSpec{
					SecretRef: &v1alpha2.SecretReference{
						Name: "openai-secret",
					},
				},
			},
			secretObj: types.NamespacedName{
				Name:      "openai-secret",
				Namespace: "kagent",
			},
			want: true,
		},
		{
			name: "non-matching secret name",
			mpc: &v1alpha2.ModelProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openai-provider",
					Namespace: "kagent",
				},
				Spec: v1alpha2.ModelProviderConfigSpec{
					SecretRef: &v1alpha2.SecretReference{
						Name: "openai-secret",
					},
				},
			},
			secretObj: types.NamespacedName{
				Name:      "anthropic-secret",
				Namespace: "kagent",
			},
			want: false,
		},
		{
			name: "nil secret ref (e.g. Ollama)",
			mpc: &v1alpha2.ModelProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ollama-provider",
					Namespace: "kagent",
				},
				Spec: v1alpha2.ModelProviderConfigSpec{
					SecretRef: nil,
				},
			},
			secretObj: types.NamespacedName{
				Name:      "some-secret",
				Namespace: "kagent",
			},
			want: false,
		},
		{
			name: "different namespace",
			mpc: &v1alpha2.ModelProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openai-provider",
					Namespace: "kagent",
				},
				Spec: v1alpha2.ModelProviderConfigSpec{
					SecretRef: &v1alpha2.SecretReference{
						Name: "openai-secret",
					},
				},
			},
			secretObj: types.NamespacedName{
				Name:      "openai-secret",
				Namespace: "other-namespace",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := modelProviderConfigReferencesSecret(tt.mpc, tt.secretObj)
			if got != tt.want {
				t.Errorf("modelProviderConfigReferencesSecret() = %v, want %v", got, tt.want)
			}
		})
	}
}
