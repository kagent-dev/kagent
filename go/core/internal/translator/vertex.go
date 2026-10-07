package translator

import (
	"encoding/json"
	"net/url"

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

// ValidateGoogleServiceAccountKey checks a Vertex AI credential Secret entry
// when the agent is compiled, so a misconfigured key is reported on the
// AgentTemplate rather than when the gateway fetches the credential: a service
// account key of the ModelConfig's project whose token endpoint is Google's.
func ValidateGoogleServiceAccountKey(data []byte, projectID string) error {
	var key struct {
		Type      string `json:"type"`
		ProjectID string `json:"project_id"`
		TokenURI  string `json:"token_uri"`
	}
	if err := json.Unmarshal(data, &key); err != nil {
		return NewValidationError("decode Vertex AI credentials: %v", err)
	}
	if key.Type != "service_account" {
		return NewValidationError("Vertex AI credentials must be a service_account key")
	}
	if key.ProjectID != projectID {
		return NewValidationError("Vertex AI credential project_id must match the ModelConfig's projectID")
	}
	if key.TokenURI != "" {
		parsed, err := url.Parse(key.TokenURI)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "oauth2.googleapis.com" {
			return NewValidationError("Vertex AI credential token_uri must use https://oauth2.googleapis.com")
		}
	}
	return nil
}
