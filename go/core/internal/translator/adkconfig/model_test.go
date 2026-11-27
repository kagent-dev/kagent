package adkconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

func TestBedrockProviderJSONRoundTrip(t *testing.T) {
	for _, raw := range []string{"", "{}", `{"thinking":{"budget_tokens":1024},"large":9007199254740993,"values":[0,false,null,0.25]}`} {
		t.Run(raw, func(t *testing.T) {
			bedrock := &v1alpha3.BedrockConfig{Region: "us-east-1"}
			if raw != "" {
				bedrock.AdditionalModelRequestFields = &apiextensionsv1.JSON{Raw: []byte(raw)}
			}
			model, _, err := translateModel(&translator.ResolvedModelConfig{Config: &v1alpha3.ModelConfig{
				Spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderBedrock, Model: "claude", Bedrock: bedrock},
			}})
			require.NoError(t, err)
			encoded, err := runtimeconfig.Marshal(&configv1.ADKConfig{Agent: &configv1.ADKAgentConfig{Model: model}})
			require.NoError(t, err)
			var decoded configv1.ADKConfig
			require.NoError(t, runtimeconfig.Unmarshal(encoded, &decoded))
			require.Equal(t, raw, string(decoded.GetAgent().GetModel().GetBedrock().GetAdditionalModelRequestFields()))
			if raw == "" {
				require.NotContains(t, string(encoded), "additional_model_request_fields")
			}
		})
	}
}

func TestRenderBedrockCredentialsFromReferences(t *testing.T) {
	secret := types.NamespacedName{Namespace: "test", Name: "credentials"}
	tests := []struct {
		name       string
		references []translator.ModelConfigReference
		want       []string
	}{
		{
			name:       "bearer",
			references: []translator.ModelConfigReference{{NamespacedName: secret, Kind: "Secret", Key: env.AWSBearerTokenBedrock.Name()}},
			want:       []string{env.AWSRegion.Name(), env.AWSBearerTokenBedrock.Name()},
		},
		{
			name: "IAM with session token",
			references: []translator.ModelConfigReference{
				{NamespacedName: secret, Kind: "Secret", Key: env.AWSAccessKeyID.Name()},
				{NamespacedName: secret, Kind: "Secret", Key: env.AWSSecretAccessKey.Name()},
				{NamespacedName: secret, Kind: "Secret", Key: env.AWSSessionToken.Name()},
			},
			want: []string{env.AWSRegion.Name(), env.AWSAccessKeyID.Name(), env.AWSSecretAccessKey.Name(), env.AWSSessionToken.Name()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := &translator.ResolvedModelConfig{
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
// Secret, never serialized into the agent config. The host is compiled onto
// the model only when the operator set one — an empty value lets the runtime
// apply its own cloud/local routing instead of looking like a chosen endpoint
// — and never into the shared OLLAMA_API_BASE variable, where one model's host
// would replace another's.
func TestTranslateOllamaEnvironment(t *testing.T) {
	tests := []struct {
		name      string
		spec      v1alpha3.ModelConfigSpec
		wantNames []string
		noEnv     string
		wantHost  string
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
			wantNames: []string{},
			noEnv:     env.OllamaAPIKey.Name(),
			wantHost:  "http://host.docker.internal:11434",
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
				TokenPassthrough: new(true),
				Ollama:           &v1alpha3.OllamaConfig{},
			},
			wantNames: []string{},
			noEnv:     env.OllamaAPIKey.Name(),
		},
		{
			// The chart ships a default host, so this is the ordinary case for
			// an operator who follows the values.yaml comment introducing
			// apiKeySecretRef. The request goes to that host, so no credential
			// binding exists, and mounting the key made CompileCredentials
			// reject the whole revision ("cannot use gateway header injection")
			// instead of simply using the daemon.
			name: "an explicit host does not mount the key even for a cloud model",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "deepseek-v4-flash:0731-cloud",
				APIKeySecret: "ollama-cloud", APIKeySecretKey: "OLLAMA_API_KEY",
				Ollama: &v1alpha3.OllamaConfig{Host: "host.docker.internal:11434"},
			},
			wantNames: []string{},
			noEnv:     env.OllamaAPIKey.Name(),
			wantHost:  "http://host.docker.internal:11434",
		},
		{
			// The cloud endpoint written out longhand is the cloud route, not an
			// operator host: the runtime takes its bearer token from
			// IsOllamaCloudEndpoint, so the key has to be mounted here or the
			// request goes out unauthenticated.
			name: "a host that is the cloud endpoint mounts the key",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "deepseek-v4-flash:0731-cloud",
				APIKeySecret: "ollama-cloud", APIKeySecretKey: "OLLAMA_API_KEY",
				Ollama: &v1alpha3.OllamaConfig{Host: "api.ollama.com"},
			},
			wantNames: []string{env.OllamaAPIKey.Name()},
			wantHost:  "https://api.ollama.com",
		},
		{
			// ...but it must not promote a local model to the cloud.
			name: "the cloud endpoint does not mount the key for a local model",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "llama3.2",
				APIKeySecret: "ollama-cloud", APIKeySecretKey: "OLLAMA_API_KEY",
				Ollama: &v1alpha3.OllamaConfig{Host: "api.ollama.com"},
			},
			wantNames: []string{},
			noEnv:     env.OllamaAPIKey.Name(),
			wantHost:  "https://api.ollama.com",
		},
		{
			// Same problem without a host: a local model has no cloud binding,
			// so mounting the Secret breaks compilation for a config that was
			// previously fine.
			name: "a local model with a secret does not mount the key",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "llama3.2",
				APIKeySecret: "ollama-cloud", APIKeySecretKey: "OLLAMA_API_KEY",
				Ollama: &v1alpha3.OllamaConfig{},
			},
			wantNames: []string{},
			noEnv:     env.OllamaAPIKey.Name(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := &translator.ResolvedModelConfig{
				Config: &v1alpha3.ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
					Spec:       tt.spec,
				},
			}
			model, data, err := translateModel(resolved)
			require.NoError(t, err)
			names := make([]string, 0, len(data.EnvVars))
			for _, variable := range data.EnvVars {
				names = append(names, variable.Name)
			}
			require.ElementsMatch(t, tt.wantNames, names)
			require.NotContains(t, names, tt.noEnv)
			require.NotContains(t, names, env.OllamaAPIBase.Name())
			require.Equal(t, tt.wantHost, model.GetOllama().GetHost())
		})
	}
}

func TestTranslateAnthropicPromptCaching(t *testing.T) {
	tests := []struct {
		name        string
		spec        *v1alpha3.AnthropicConfig
		wantCaching bool
		wantTTL     string
	}{
		{name: "no provider block", spec: nil},
		// cacheTTL is passed through as configured; the runtime only reads it when caching is on.
		{name: "disabled", spec: &v1alpha3.AnthropicConfig{CacheTTL: "5m"}, wantTTL: "5m"},
		{name: "enabled with the default TTL", spec: &v1alpha3.AnthropicConfig{PromptCaching: true, CacheTTL: "5m"}, wantCaching: true, wantTTL: "5m"},
		{name: "enabled with the 1h TTL", spec: &v1alpha3.AnthropicConfig{PromptCaching: true, CacheTTL: "1h"}, wantCaching: true, wantTTL: "1h"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := &translator.ResolvedModelConfig{
				Config: &v1alpha3.ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
					Spec: v1alpha3.ModelConfigSpec{
						Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-6",
						Anthropic: tt.spec,
					},
				},
			}
			got, _, err := translateModel(resolved)
			require.NoError(t, err)
			anthropic := got.GetAnthropic()
			ok := anthropic != nil
			require.True(t, ok, "model is %T, want *configv1.ADKAnthropic", got)
			require.Equal(t, tt.wantCaching, anthropic.PromptCaching)
			require.Equal(t, tt.wantTTL, anthropic.CacheTtl)
		})
	}
}

func TestTranslateAzureOpenAIAPIFormat(t *testing.T) {
	responses := v1alpha3.OpenAIAPIFormatResponses
	got, _, err := translateModel(&translator.ResolvedModelConfig{Config: &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
		Spec: v1alpha3.ModelConfigSpec{
			Provider: v1alpha3.ModelProviderAzureOpenAI, Model: "gpt-4.1-mini",
			AzureOpenAI: &v1alpha3.AzureOpenAIConfig{Endpoint: "https://team.openai.azure.com", APIFormat: &responses},
		},
	}})
	require.NoError(t, err)
	require.Equal(t, "responses", got.GetAzureOpenai().ApiFormat)
}
