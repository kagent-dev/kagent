package translator

import (
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// VertexAIConfig is the project and location of a Vertex AI ModelConfig, for
// either provider that reaches Vertex AI; nil for every other provider or an
// incomplete spec.
func VertexAIConfig(spec *v1alpha3.ModelConfigSpec) *v1alpha3.BaseVertexAIConfig {
	switch spec.Provider {
	case v1alpha3.ModelProviderGeminiVertexAI:
		if spec.GeminiVertexAI != nil {
			return &spec.GeminiVertexAI.BaseVertexAIConfig
		}
	case v1alpha3.ModelProviderAnthropicVertexAI:
		if spec.AnthropicVertexAI != nil {
			return &spec.AnthropicVertexAI.BaseVertexAIConfig
		}
	}
	return nil
}

// VertexAIHostname is the Vertex AI endpoint for a ModelConfig location: the
// global endpoint, a multi-region endpoint for us and eu, or the regional one.
func VertexAIHostname(location string) string {
	switch location {
	case "global":
		return "aiplatform.googleapis.com"
	case "us", "eu":
		return "aiplatform." + location + ".rep.googleapis.com"
	default:
		return location + "-aiplatform.googleapis.com"
	}
}

// VertexAIOrigin is the egress origin of VertexAIHostname.
func VertexAIOrigin(location string) string {
	return "https://" + VertexAIHostname(location) + ":443"
}
