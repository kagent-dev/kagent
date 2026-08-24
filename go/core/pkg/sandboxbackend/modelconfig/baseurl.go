// Package modelconfig holds backend-agnostic helpers for reading ModelConfig
// fields shared by Substrate harness adapters (OpenClaw, Hermes, …).
package modelconfig

import (
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// ExplicitBaseURL returns a non-empty provider endpoint from ModelConfig when
// the user set one (OpenAI/Anthropic baseUrl, Azure endpoint, Ollama host, etc.).
func ExplicitBaseURL(mc *v1alpha3.ModelConfig) string {
	if mc == nil {
		return ""
	}
	switch mc.Spec.Provider {
	case v1alpha3.ModelProviderOpenAI:
		if mc.Spec.OpenAI != nil && strings.TrimSpace(mc.Spec.OpenAI.BaseURL) != "" {
			return strings.TrimSpace(mc.Spec.OpenAI.BaseURL)
		}
	case v1alpha3.ModelProviderAnthropic:
		if mc.Spec.Anthropic != nil && strings.TrimSpace(mc.Spec.Anthropic.BaseURL) != "" {
			return strings.TrimSpace(mc.Spec.Anthropic.BaseURL)
		}
	case v1alpha3.ModelProviderAzureOpenAI:
		if mc.Spec.AzureOpenAI != nil && strings.TrimSpace(mc.Spec.AzureOpenAI.Endpoint) != "" {
			return strings.TrimSpace(mc.Spec.AzureOpenAI.Endpoint)
		}
	case v1alpha3.ModelProviderOllama:
		if mc.Spec.Ollama != nil && strings.TrimSpace(mc.Spec.Ollama.Host) != "" {
			return strings.TrimSpace(mc.Spec.Ollama.Host)
		}
	case v1alpha3.ModelProviderSAPAICore:
		if mc.Spec.SAPAICore != nil && strings.TrimSpace(mc.Spec.SAPAICore.BaseURL) != "" {
			return strings.TrimSpace(mc.Spec.SAPAICore.BaseURL)
		}
	}
	return ""
}
