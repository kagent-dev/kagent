package adkconfig

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestRenderBedrockCredentialsFromReferences(t *testing.T) {
	secret := types.NamespacedName{Namespace: "test", Name: "credentials"}
	tests := []struct {
		name       string
		references []v2translator.ModelConfigReference
		want       []string
	}{
		{
			name:       "bearer",
			references: []v2translator.ModelConfigReference{{NamespacedName: secret, Kind: "Secret", Key: env.AWSBearerTokenBedrock.Name()}},
			want:       []string{env.AWSRegion.Name(), env.AWSBearerTokenBedrock.Name()},
		},
		{
			name: "IAM with session token",
			references: []v2translator.ModelConfigReference{
				{NamespacedName: secret, Kind: "Secret", Key: env.AWSAccessKeyID.Name()},
				{NamespacedName: secret, Kind: "Secret", Key: env.AWSSecretAccessKey.Name()},
				{NamespacedName: secret, Kind: "Secret", Key: env.AWSSessionToken.Name()},
			},
			want: []string{env.AWSRegion.Name(), env.AWSAccessKeyID.Name(), env.AWSSecretAccessKey.Name(), env.AWSSessionToken.Name()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := &v2translator.ResolvedModelConfig{
				Config: &v1alpha3.ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Namespace: secret.Namespace},
					Spec: v1alpha3.ModelConfigSpec{
						Provider: v1alpha3.ModelProviderBedrock, Model: "claude", APIKeySecret: secret.Name,
						Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1"},
					},
				},
				References: tt.references,
			}
			_, data, err := translateModel(resolved)
			require.NoError(t, err)
			names := make([]string, 0, len(data.EnvVars))
			for _, variable := range data.EnvVars {
				names = append(names, variable.Name)
			}
			require.ElementsMatch(t, tt.want, names)
		})
	}
}

// The Ollama key reaches the agent as an environment variable read from a
// Secret, never serialized into the agent config, and the host is written only
// when the operator set one — an empty value lets the runtime apply its own
// cloud/local routing instead of looking like a chosen endpoint.
func TestTranslateOllamaEnvironment(t *testing.T) {
	tests := []struct {
		name      string
		spec      v1alpha3.ModelConfigSpec
		wantNames []string
		noEnv     string
	}{
		{
			name: "cloud model mounts the key from a Secret",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "deepseek-v4-flash:0731-cloud",
				APIKeySecret: "ollama-cloud", APIKeySecretKey: "OLLAMA_API_KEY",
				Ollama: &v1alpha3.OllamaConfig{},
			},
			wantNames: []string{env.OllamaAPIKey.Name()},
			noEnv:     env.OllamaAPIBase.Name(),
		},
		{
			name: "an explicit host is passed through",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "llama3.2",
				Ollama: &v1alpha3.OllamaConfig{Host: "host.docker.internal:11434"},
			},
			wantNames: []string{env.OllamaAPIBase.Name()},
			noEnv:     env.OllamaAPIKey.Name(),
		},
		{
			name: "local model with no credential sets neither",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "llama3.2",
				Ollama: &v1alpha3.OllamaConfig{},
			},
			wantNames: []string{},
			noEnv:     env.OllamaAPIKey.Name(),
		},
		{
			// Passthrough carries the caller's own token, so a stale secret must
			// not be mounted alongside it.
			name: "passthrough does not mount a key",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "deepseek-v4-flash:0731-cloud",
				APIKeySecret: "ollama-cloud", APIKeySecretKey: "OLLAMA_API_KEY",
				APIKeyPassthrough: true,
				Ollama:            &v1alpha3.OllamaConfig{},
			},
			wantNames: []string{},
			noEnv:     env.OllamaAPIKey.Name(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := &v2translator.ResolvedModelConfig{
				Config: &v1alpha3.ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
					Spec:       tt.spec,
				},
			}
			_, data, err := translateModel(resolved)
			require.NoError(t, err)
			names := make([]string, 0, len(data.EnvVars))
			for _, variable := range data.EnvVars {
				names = append(names, variable.Name)
			}
			require.ElementsMatch(t, tt.wantNames, names)
			require.NotContains(t, names, tt.noEnv)
		})
	}
}
