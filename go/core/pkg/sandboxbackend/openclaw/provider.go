package openclaw

import (
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/modelconfig"
)

func bootstrapProviderBaseURL(mc *v1alpha3.ModelConfig, defaultWhenUnset string) string {
	if u := modelconfig.ExplicitBaseURL(mc); u != "" {
		return u
	}
	return defaultWhenUnset
}

func providerAuth(mc *v1alpha3.ModelConfig) string {
	if mc.Spec.Provider == v1alpha3.ModelProviderBedrock {
		return "aws-sdk"
	}
	return "api-key"
}

func providerAPI(mc *v1alpha3.ModelConfig) (string, error) {
	switch mc.Spec.Provider {
	case v1alpha3.ModelProviderOpenAI:
		return "openai-completions", nil
	case v1alpha3.ModelProviderAnthropic:
		return "anthropic-messages", nil
	case v1alpha3.ModelProviderAzureOpenAI:
		return "azure-openai-responses", nil
	case v1alpha3.ModelProviderOllama:
		return "ollama", nil
	case v1alpha3.ModelProviderGemini, v1alpha3.ModelProviderGeminiVertexAI:
		return "google-generative-ai", nil
	case v1alpha3.ModelProviderAnthropicVertexAI:
		return "anthropic-messages", nil
	case v1alpha3.ModelProviderBedrock:
		return "bedrock-converse-stream", nil
	case v1alpha3.ModelProviderSAPAICore:
		return "", fmt.Errorf("model provider SAPAICore is not supported for OpenClaw sandbox JSON bootstrap")
	default:
		return "", fmt.Errorf("model provider %q is not supported for OpenClaw sandbox JSON bootstrap yet", mc.Spec.Provider)
	}
}
