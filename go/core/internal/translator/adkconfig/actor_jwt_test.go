package adkconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestActorJWTModelPlaceholders(t *testing.T) {
	for _, tc := range []struct {
		provider    v1alpha3.ModelProvider
		environment string
	}{
		{v1alpha3.ModelProviderOpenAI, "OPENAI_API_KEY"},
		{v1alpha3.ModelProviderAnthropic, "ANTHROPIC_API_KEY"},
		{v1alpha3.ModelProviderAzureOpenAI, "AZURE_OPENAI_API_KEY"},
		{v1alpha3.ModelProviderMistral, "MISTRAL_API_KEY"},
		{v1alpha3.ModelProviderFoundry, "FOUNDRY_API_KEY"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			resolved := &translator.ResolvedModelConfig{FoundryEndpoint: "https://model.example.com", Config: &v1alpha3.ModelConfig{Spec: v1alpha3.ModelConfigSpec{
				Provider: tc.provider, Model: "test:cloud", ActorJWT: &v1alpha3.ActorJWT{Audiences: []string{"model"}},
				AzureOpenAI: &v1alpha3.AzureOpenAIConfig{Endpoint: "https://model.example.com"}, Foundry: &v1alpha3.FoundryConfig{Endpoint: "https://model.example.com"},
			}}}
			model, data, err := translateModel(resolved)
			require.NoError(t, err)
			require.False(t, model.ApiKeyPassthrough)
			require.Contains(t, data.EnvVars, corev1.EnvVar{Name: tc.environment, Value: translator.CredentialPlaceholder})
			for _, variable := range data.EnvVars {
				require.Nil(t, variable.ValueFrom)
			}
		})
	}
}

func TestActorJWTUnsupportedProviders(t *testing.T) {
	for _, spec := range []v1alpha3.ModelConfigSpec{
		{Provider: v1alpha3.ModelProviderBedrock, Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1"}},
		{Provider: v1alpha3.ModelProviderGemini},
		{Provider: v1alpha3.ModelProviderOllama, Ollama: &v1alpha3.OllamaConfig{}},
	} {
		t.Run(string(spec.Provider), func(t *testing.T) {
			spec.Model = "test:cloud"
			spec.ActorJWT = &v1alpha3.ActorJWT{Audiences: []string{"model"}}
			resolved := &translator.ResolvedModelConfig{Config: &v1alpha3.ModelConfig{Spec: spec}}
			_, _, err := translateModel(resolved)
			require.ErrorContains(t, err, "does not support actorJwt")
		})
	}
}

func TestActorJWTMCPPlaceholder(t *testing.T) {
	for _, protocol := range []v1alpha3.RemoteMCPServerProtocol{v1alpha3.RemoteMCPServerProtocolSse, v1alpha3.RemoteMCPServerProtocolStreamableHttp} {
		config := &configv1.ADKAgentConfig{}
		server := &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://mcp.example.com", Protocol: protocol, ActorJWT: &v1alpha3.ActorJWT{Audiences: []string{"mcp"}}}}
		original := map[string]string{"X-Other": "value"}
		require.NoError(t, (&Builder{}).addRemoteMCPServer(config, server, nil, false, original))
		var headers map[string]string
		if len(config.HttpTools) > 0 {
			headers = config.HttpTools[0].Params.Headers
		} else {
			headers = config.SseTools[0].Params.Headers
		}
		require.Equal(t, "Bearer "+translator.CredentialPlaceholder, headers["Authorization"])
		require.Equal(t, "value", headers["X-Other"])
		require.NotContains(t, original, "Authorization")
	}
}
