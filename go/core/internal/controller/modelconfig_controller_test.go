package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func foundryModelWithEndpointFrom(name, namespace, cmName string) *v1alpha3.ModelConfig {
	return &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: v1alpha3.ModelConfigSpec{
			Provider: v1alpha3.ModelProviderFoundry,
			Foundry: &v1alpha3.FoundryConfig{
				Deployment: "gpt-4-1-nano",
				EndpointFrom: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: cmName},
					Key:                  "endpoint",
				},
			},
		},
	}
}

func TestModelReferencesConfigMap(t *testing.T) {
	tests := []struct {
		name  string
		model *v1alpha3.ModelConfig
		cm    types.NamespacedName
		want  bool
	}{
		{
			name:  "matches endpointFrom config map in same namespace",
			model: foundryModelWithEndpointFrom("m", "default", "foundry-endpoint"),
			cm:    types.NamespacedName{Namespace: "default", Name: "foundry-endpoint"},
			want:  true,
		},
		{
			name:  "different config map name does not match",
			model: foundryModelWithEndpointFrom("m", "default", "foundry-endpoint"),
			cm:    types.NamespacedName{Namespace: "default", Name: "other"},
			want:  false,
		},
		{
			name:  "different namespace does not match",
			model: foundryModelWithEndpointFrom("m", "default", "foundry-endpoint"),
			cm:    types.NamespacedName{Namespace: "other-ns", Name: "foundry-endpoint"},
			want:  false,
		},
		{
			name: "inline endpoint does not reference a config map",
			model: &v1alpha3.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "m", Namespace: "default"},
				Spec: v1alpha3.ModelConfigSpec{
					Provider: v1alpha3.ModelProviderFoundry,
					Foundry: &v1alpha3.FoundryConfig{
						Endpoint:   "https://example.cognitiveservices.azure.com/",
						Deployment: "gpt-4-1-nano",
					},
				},
			},
			cm:   types.NamespacedName{Namespace: "default", Name: "foundry-endpoint"},
			want: false,
		},
		{
			name: "non-foundry model does not reference a config map",
			model: &v1alpha3.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "m", Namespace: "default"},
				Spec: v1alpha3.ModelConfigSpec{
					Provider: v1alpha3.ModelProviderOpenAI,
					OpenAI:   &v1alpha3.OpenAIConfig{},
				},
			},
			cm:   types.NamespacedName{Namespace: "default", Name: "foundry-endpoint"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, modelReferencesConfigMap(tt.model, tt.cm))
		})
	}
}

func TestFindModelsUsingConfigMap(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, v1alpha3.AddToScheme(scheme))

	referencing := foundryModelWithEndpointFrom("uses-cm", "default", "foundry-endpoint")
	referencingToo := foundryModelWithEndpointFrom("also-uses-cm", "default", "foundry-endpoint")
	otherCM := foundryModelWithEndpointFrom("uses-other-cm", "default", "other")
	otherNamespace := foundryModelWithEndpointFrom("uses-cm-other-ns", "other-ns", "foundry-endpoint")

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(referencing, referencingToo, otherCM, otherNamespace).
		Build()

	r := &ModelConfigController{}
	models := r.findModelsUsingConfigMap(context.Background(), cl,
		types.NamespacedName{Namespace: "default", Name: "foundry-endpoint"})

	require.Len(t, models, 2)
	names := []string{models[0].Name, models[1].Name}
	assert.ElementsMatch(t, []string{"uses-cm", "also-uses-cm"}, names)
}
