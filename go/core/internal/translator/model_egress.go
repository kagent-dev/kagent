package translator

import (
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/types"

	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

func modelSecretKey(model *ResolvedModelConfig) string {
	if model.Config.Spec.Provider != v1alpha3.ModelProviderBedrock {
		return model.Config.Spec.APIKeySecretKey
	}
	secret := types.NamespacedName{Namespace: model.Config.Namespace, Name: model.Config.Spec.APIKeySecret}
	if slices.ContainsFunc(model.References, func(ref ModelConfigReference) bool {
		return ref.Kind == "Secret" && ref.NamespacedName == secret && ref.Key == env.AWSBearerTokenBedrock.Name()
	}) {
		return env.AWSBearerTokenBedrock.Name()
	}
	return ""
}

type modelCredential struct {
	Environment string
	Endpoint    string
	Header      string
	Prefix      string
}

func modelCredentialTarget(resolved *ResolvedModelConfig) modelCredential {
	var name, endpoint, header, prefix string
	spec := resolved.Config.Spec
	switch spec.Provider {
	case v1alpha3.ModelProviderOpenAI:
		name, endpoint, header, prefix = env.OpenAIAPIKey.Name(), "https://api.openai.com", "authorization", "Bearer "
		if spec.OpenAI != nil && spec.OpenAI.BaseURL != "" {
			endpoint = spec.OpenAI.BaseURL
		}
	case v1alpha3.ModelProviderAnthropic:
		name, endpoint, header = env.AnthropicAPIKey.Name(), "https://api.anthropic.com", "x-api-key"
		if spec.Anthropic != nil && spec.Anthropic.BaseURL != "" {
			endpoint = spec.Anthropic.BaseURL
		}
	case v1alpha3.ModelProviderAzureOpenAI:
		name, header = env.AzureOpenAIAPIKey.Name(), "api-key"
		if spec.AzureOpenAI != nil {
			endpoint = spec.AzureOpenAI.Endpoint
		}
	case v1alpha3.ModelProviderGemini:
		name, endpoint, header = env.GoogleAPIKey.Name(), "https://generativelanguage.googleapis.com", "x-goog-api-key"
	case v1alpha3.ModelProviderMistral:
		name, endpoint, header, prefix = env.MistralAPIKey.Name(), "https://api.mistral.ai", "authorization", "Bearer "
		if spec.Mistral != nil && spec.Mistral.BaseURL != nil && *spec.Mistral.BaseURL != "" {
			endpoint = *spec.Mistral.BaseURL
		}
	case v1alpha3.ModelProviderBedrock:
		name, header, prefix = env.AWSBearerTokenBedrock.Name(), "authorization", "Bearer "
		if spec.Bedrock != nil {
			endpoint = fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", spec.Bedrock.Region)
		}
	case v1alpha3.ModelProviderOllama:
		// Ollama Cloud is the only keyed Ollama endpoint, so a binding is
		// declared exactly when the model reaches it. That condition lives in
		// models.OllamaReachesCloud, which the compiler's Secret reference and
		// the egress list also use: a cloud model picked from the catalog, an
		// explicit host, and a missing key were each judged differently in each
		// place, so a valid configuration could compile with an env var that had
		// no binding to match, or with an egress list that omitted the host it
		// was about to call.
		//
		// Without a binding this leaves the name empty, and the env var is
		// treated as a plain secret reference, which is correct — no request
		// leaves for api.ollama.com.
		if spec.Ollama == nil {
			break
		}
		hasCredential := spec.APIKeySecret != "" || spec.APIKeyPassthrough
		if !models.OllamaReachesCloud(spec.Model, spec.Ollama.Host, hasCredential) {
			break
		}
		name, endpoint, header, prefix = env.OllamaAPIKey.Name(), "https://api.ollama.com", "authorization", "Bearer "
	case v1alpha3.ModelProviderFoundry:
		name, endpoint, header = env.FoundryAPIKey.Name(), resolved.FoundryEndpoint, "api-key"
		if spec.Foundry != nil && spec.Foundry.APIFormat == v1alpha3.FoundryAPIFormatAnthropic {
			header = "x-api-key"
		}
	}
	return modelCredential{Environment: name, Endpoint: endpoint, Header: header, Prefix: prefix}
}

// modelEndpoints returns application endpoints before client configuration is rendered.
func modelEndpoints(resolved *ResolvedModelConfig) []string {
	if resolved == nil {
		return nil
	}
	spec := resolved.Config.Spec
	endpoint := modelCredentialTarget(resolved).Endpoint
	var endpoints []string
	if endpoint != "" {
		endpoints = append(endpoints, endpoint)
	}
	if spec.Provider == v1alpha3.ModelProviderOllama && spec.Ollama != nil && spec.Ollama.Host != "" {
		endpoints = append(endpoints, OllamaURL(spec.Ollama.Host))
	}
	if spec.Provider == v1alpha3.ModelProviderSAPAICore && spec.SAPAICore != nil {
		endpoints = append(endpoints, spec.SAPAICore.BaseURL)
		if spec.SAPAICore.AuthURL != "" {
			endpoints = append(endpoints, spec.SAPAICore.AuthURL)
		}
	}
	return endpoints
}

// OllamaURL uses HTTPS for the cloud endpoint and HTTP for bare daemon addresses.
func OllamaURL(host string) string {
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return host
	}
	if models.IsOllamaCloudEndpoint(host) {
		return "https://" + host
	}
	return "http://" + host
}
