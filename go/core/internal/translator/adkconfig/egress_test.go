package adkconfig

import (
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	"slices"
	"testing"
)

func TestRetainedADKMetadataDestinations(t *testing.T) {
	for _, test := range []struct {
		name string
		spec v1alpha3.ModelConfigSpec
		want []string
	}{
		{"model and headers", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "https://model.example.com/name", DefaultHeaders: map[string]string{"HTTP-Referer": "https://app.example.com/page"}, Ollama: &v1alpha3.OllamaConfig{Options: map[string]string{"inactive": "https://unused.example.com"}}}, []string{"https://api.openai.com:443", "https://app.example.com:443", "https://model.example.com:443"}},
		{"Azure deployment replaces model name", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAzureOpenAI, Model: "https://unused.example.com", AzureOpenAI: &v1alpha3.AzureOpenAIConfig{DeploymentName: "https://deployment.example.com", APIVersion: "https://version.example.com"}}, []string{"https://deployment.example.com:443", "https://version.example.com:443"}},
		{"Ollama options", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOllama, Ollama: &v1alpha3.OllamaConfig{Options: map[string]string{"url": "https://options.example.com/path"}}}, []string{"https://options.example.com:443"}},
		{"Bedrock guardrail", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderBedrock, Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1", Guardrail: &v1alpha3.BedrockGuardrailConfig{Identifier: "https://guardrail.example.com/id", Version: "DRAFT"}}}, []string{"https://guardrail.example.com:443"}},
		{"SAP resource group", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderSAPAICore, SAPAICore: &v1alpha3.SAPAICoreConfig{ResourceGroup: "https://group.example.com"}}, []string{"https://group.example.com:443"}},
		{"Foundry metadata", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderFoundry, Foundry: &v1alpha3.FoundryConfig{Deployment: "https://deployment.example.com", APIVersion: "https://version.example.com"}}, []string{"https://deployment.example.com:443", "https://version.example.com:443"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := &translator.ResolvedModelConfig{Config: &v1alpha3.ModelConfig{Spec: test.spec}}
			got := AdditionalModelDestinations(model)
			slices.Sort(got)
			require.Equal(t, test.want, got)
		})
	}
}
