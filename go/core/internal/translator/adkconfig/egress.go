package adkconfig

import (
	"net/url"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

// AdditionalModelDestinations preserves ADK grants beyond the effective model endpoint.
// Retained defaults and metadata origins do not create credential targets.
func AdditionalModelDestinations(resolved *translator.ResolvedModelConfig) []string {
	spec := resolved.Config.Spec
	var destinations []string
	switch spec.Provider {
	case v1alpha3.ModelProviderOpenAI:
		destinations = append(destinations, "https://api.openai.com:443")
	case v1alpha3.ModelProviderAnthropic:
		destinations = append(destinations, "https://api.anthropic.com:443")
	}
	// The ADK config walk also allowed URL-valued metadata. Preserve those grants
	// without rendering a client or scanning inactive provider configurations.
	modelName := spec.Model
	if spec.Provider == v1alpha3.ModelProviderAzureOpenAI && spec.AzureOpenAI != nil {
		modelName = spec.AzureOpenAI.DeploymentName
	}
	destinations = appendModelMetadataOrigin(destinations, modelName)
	for _, value := range spec.DefaultHeaders {
		destinations = appendModelMetadataOrigin(destinations, value)
	}
	var values []string
	switch spec.Provider {
	case v1alpha3.ModelProviderAzureOpenAI:
		if spec.AzureOpenAI != nil {
			values = []string{spec.AzureOpenAI.APIVersion}
		}
	case v1alpha3.ModelProviderOllama:
		if spec.Ollama != nil {
			for _, value := range spec.Ollama.Options {
				values = append(values, value)
			}
		}
	case v1alpha3.ModelProviderBedrock:
		if spec.Bedrock != nil {
			values = []string{spec.Bedrock.Region, spec.Bedrock.CacheTTL}
			if guardrail := spec.Bedrock.Guardrail; guardrail != nil {
				values = append(values, guardrail.Identifier, guardrail.Version, guardrail.Trace)
			}
		}
	case v1alpha3.ModelProviderSAPAICore:
		if spec.SAPAICore != nil {
			values = []string{spec.SAPAICore.ResourceGroup}
		}
	case v1alpha3.ModelProviderFoundry:
		if spec.Foundry != nil {
			values = []string{spec.Foundry.Deployment, spec.Foundry.APIVersion}
		}
	}
	for _, value := range values {
		destinations = appendModelMetadataOrigin(destinations, value)
	}
	return destinations
}

func appendModelMetadataOrigin(destinations []string, value string) []string {
	parsed, err := url.Parse(value)
	if err == nil && parsed.Hostname() != "" {
		return append(destinations, egress.Origin(parsed))
	}
	return destinations
}
