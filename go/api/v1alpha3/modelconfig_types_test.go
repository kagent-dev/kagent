package v1alpha3

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestGeminiEndpointAdmission(t *testing.T) {
	env := &envtest.Environment{BinaryAssetsDirectory: envtestAssetsDir(t), CRDDirectoryPaths: []string{crdChartDir(t)}, ErrorIfCRDPathMissing: true}
	cfg, err := env.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, env.Stop()) })
	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	client, err := ctrlclient.New(cfg, ctrlclient.Options{Scheme: scheme})
	require.NoError(t, err)
	require.NoError(t, client.Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "native-endpoints"}}))
	for _, tt := range []struct {
		name    string
		spec    ModelConfigSpec
		invalid bool
	}{
		{"gemini", ModelConfigSpec{Provider: ModelProviderGemini, Gemini: &GeminiConfig{BaseURL: new("https://gateway.example/team/v1")}}, false},
		{"gemini-actor-default", ModelConfigSpec{Provider: ModelProviderGemini, ActorJWT: &ActorJWT{Audiences: []string{"models"}}}, false},
		{"gemini-actor-custom", ModelConfigSpec{Provider: ModelProviderGemini, Gemini: &GeminiConfig{BaseURL: new("https://models.example/v1")}, ActorJWT: &ActorJWT{Audiences: []string{"models"}}}, false},
		{"gemini-token", ModelConfigSpec{Provider: ModelProviderGemini, Gemini: &GeminiConfig{BaseURL: new("https://gateway.example/team/v1")}, TokenPassthrough: new(true)}, true},
		{"gemini-direct-token", ModelConfigSpec{Provider: ModelProviderGemini, TokenPassthrough: new(true)}, true},
		{"gemini-relative", ModelConfigSpec{Provider: ModelProviderGemini, Gemini: &GeminiConfig{BaseURL: new("/team/v1")}}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt.spec.Model = "catalog-alias"
			m := &ModelConfig{ObjectMeta: metav1.ObjectMeta{Name: tt.name, Namespace: "native-endpoints"}, Spec: tt.spec}
			err := client.Create(t.Context(), m)
			if tt.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			stored := &ModelConfig{}
			require.NoError(t, client.Get(t.Context(), ctrlclient.ObjectKeyFromObject(m), stored))
			if tt.spec.Gemini != nil {
				require.Equal(t, tt.spec.Gemini.BaseURL, stored.Spec.Gemini.BaseURL)
			}
		})
	}
}
