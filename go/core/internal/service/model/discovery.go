package model

import (
	"context"
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/types"

	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/modeldiscovery"
)

// ProviderDiscovery owns refresh scheduling and the local read-only catalog.
type ProviderDiscovery interface {
	modeldiscovery.Reader
	Refresh(context.Context, types.NamespacedName) (*modeldiscovery.Snapshot, error)
}
type ServiceOption func(*Service)

func WithProviderDiscovery(discovery ProviderDiscovery) ServiceOption {
	return func(s *Service) { s.discovery = discovery }
}

type ModelInfo struct {
	Name            string `json:"name"`
	FunctionCalling bool   `json:"function_calling"`
}

type ProviderModels map[v1alpha3.ModelProvider][]ModelInfo

type ProviderDefinition struct {
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	RequiredParams []string `json:"requiredParams"`
	OptionalParams []string `json:"optionalParams"`
}

type ConfiguredProvider = apiv1.ConfiguredProvider

type GetProviderModelsRequest struct {
	Ref     types.NamespacedName
	Refresh bool
}

// ollamaCatalogModels renders the Ollama catalog from the same list that
// decides cloud routing.
//
// The two used to be maintained separately: the catalog advertised bare names
// while cloud routing recognised only the ":cloud" tag, so picking
// a listed cloud model routed it to the local daemon. Deriving both from
// models.OllamaCloudModels makes a cloud entry unable to appear in the catalog
// without also being routed to the cloud.
//
// Tool support is true throughout — it is verified per model against /api/show
// for cloud models and a local daemon for local ones, not inferred from family.
func ollamaCatalogModels() []ModelInfo {
	// Local models, pulled with `ollama pull` and served by a daemon the
	// operator runs. A cloud tag is not accepted for either — deepseek-r1 is
	// not on Ollama Cloud at all — so these reach the daemon directly.
	local := []string{"qwen3.5", "deepseek-r1"}

	catalog := make([]ModelInfo, 0, len(models.OllamaCloudModels)+len(local))
	for _, name := range append(append([]string{}, models.OllamaCloudModels...), local...) {
		catalog = append(catalog, ModelInfo{Name: name, FunctionCalling: true})
	}
	return catalog
}

func (s *Service) ListSupportedModels(context.Context) ProviderModels {
	// The keys need to match what the UI expects (camelCase for API keys)
	// List of models is built from the following sites:
	//   OpenAI   -> https://developers.openai.com/api/docs/models
	//   Anthropic-> https://platform.claude.com/docs/en/about-claude/models/model-ids-and-versions
	//   Azure    -> https://learn.microsoft.com/azure/foundry/foundry-models
	//   Gemini   -> https://ai.google.dev/gemini-api/docs/models
	//   Bedrock  -> https://docs.aws.amazon.com/bedrock/latest/userguide/model-cards.html
	//   Vertex   -> https://platform.claude.com/docs/en/build-with-claude/claude-in-google-cloud-vertex-ai
	//   SAP      -> SAP Note 3437766 (live model/version table)
	return ProviderModels{
		v1alpha3.ModelProviderOpenAI: {
			// GPT-5.6 family
			{Name: "gpt-5.6-terra", FunctionCalling: true},
			{Name: "gpt-5.6-luna", FunctionCalling: true},
			// GPT-5.x point releases
			{Name: "gpt-5.5", FunctionCalling: true},
			{Name: "gpt-5.5-pro", FunctionCalling: true},
			{Name: "gpt-5.4", FunctionCalling: true},
			{Name: "gpt-5.4-pro", FunctionCalling: true},
			{Name: "gpt-5.4-mini", FunctionCalling: true},
			{Name: "gpt-5.4-nano", FunctionCalling: true},
			{Name: "gpt-5.3-codex", FunctionCalling: true},
			{Name: "gpt-5.2", FunctionCalling: true},
			{Name: "gpt-5.2-pro", FunctionCalling: true},
			{Name: "gpt-5.1", FunctionCalling: true},
			{Name: "gpt-5", FunctionCalling: true},
			{Name: "gpt-5-mini", FunctionCalling: true},
			{Name: "gpt-5-nano", FunctionCalling: true},
			{Name: "gpt-5-pro", FunctionCalling: true},
			// Reasoning (o-series)
			{Name: "o3", FunctionCalling: true},
			{Name: "o3-pro", FunctionCalling: true},
			{Name: "o3-mini", FunctionCalling: true}, // deprecated
			{Name: "o4-mini", FunctionCalling: true}, // deprecated
			// GPT-4.1 family
			{Name: "gpt-4.1", FunctionCalling: true},
			{Name: "gpt-4.1-mini", FunctionCalling: true},
			{Name: "gpt-4.1-nano", FunctionCalling: true}, // deprecated
			// Legacy (still callable, but marked deprecated by OpenAI)
			{Name: "gpt-4o", FunctionCalling: true},
			{Name: "gpt-4o-mini", FunctionCalling: true},
			{Name: "gpt-4-turbo", FunctionCalling: true},
			{Name: "gpt-4", FunctionCalling: true},
			{Name: "gpt-3.5-turbo", FunctionCalling: true},
		},
		v1alpha3.ModelProviderAnthropic: {
			{Name: "claude-fable-5-1", FunctionCalling: true},
			{Name: "claude-fable-5", FunctionCalling: true},
			{Name: "claude-opus-5-5", FunctionCalling: true},
			{Name: "claude-opus-5", FunctionCalling: true},
			{Name: "claude-opus-4-8", FunctionCalling: true},
			{Name: "claude-opus-4-7", FunctionCalling: true},
			{Name: "claude-opus-4-6", FunctionCalling: true},
			{Name: "claude-sonnet-5-5", FunctionCalling: true},
			{Name: "claude-sonnet-5", FunctionCalling: true},
			{Name: "claude-sonnet-4-6", FunctionCalling: true},
			{Name: "claude-haiku-4-5", FunctionCalling: true},
			// Legacy dated snapshots
			{Name: "claude-opus-4-1-20250805", FunctionCalling: true},
			{Name: "claude-opus-4-20250514", FunctionCalling: true},
			{Name: "claude-sonnet-4-20250514", FunctionCalling: true},
			{Name: "claude-sonnet-4-5", FunctionCalling: true},
			{Name: "claude-3-7-sonnet-20250219", FunctionCalling: true},
			{Name: "claude-3-5-sonnet-20240620", FunctionCalling: true},
		},
		v1alpha3.ModelProviderAzureOpenAI: {
			// Azure rollout lags OpenAI; newer point releases may be region-limited.
			{Name: "gpt-5.5", FunctionCalling: true},
			{Name: "gpt-5.4", FunctionCalling: true},
			{Name: "gpt-5.4-mini", FunctionCalling: true},
			{Name: "gpt-5.4-nano", FunctionCalling: true},
			{Name: "gpt-5.2", FunctionCalling: true},
			{Name: "gpt-5.1", FunctionCalling: true},
			{Name: "gpt-5", FunctionCalling: true},
			{Name: "gpt-5-mini", FunctionCalling: true},
			{Name: "gpt-5-nano", FunctionCalling: true},
			{Name: "gpt-4.1", FunctionCalling: true},
			{Name: "gpt-4.1-mini", FunctionCalling: true},
			{Name: "gpt-4.1-nano", FunctionCalling: true},
			{Name: "gpt-4o", FunctionCalling: true},
			{Name: "gpt-4o-mini", FunctionCalling: true},
			{Name: "o4-mini", FunctionCalling: true},
			{Name: "o3", FunctionCalling: true},
			{Name: "o3-mini", FunctionCalling: true},
			{Name: "gpt-4", FunctionCalling: true},
			{Name: "gpt-35-turbo", FunctionCalling: true}, // Azure spelling (no dot)
			{Name: "gpt-oss-120b", FunctionCalling: true},
		},
		v1alpha3.ModelProviderFoundry: {
			// Azure AI Foundry serves many vendors' chat-completion models through
			// its OpenAI-compatible data plane. These are common suggestions; the
			// value is the model name, while the Foundry deployment name is set
			// separately in the model config.
			//
			// Claude models on Foundry are served over the Anthropic Messages API;
			// set spec.foundry.apiFormat to Anthropic when using them.
			{Name: "gpt-4.1", FunctionCalling: true},
			{Name: "gpt-4.1-mini", FunctionCalling: true},
			{Name: "gpt-4.1-nano", FunctionCalling: true},
			{Name: "gpt-4o", FunctionCalling: true},
			{Name: "gpt-4o-mini", FunctionCalling: true},
			{Name: "o4-mini", FunctionCalling: true},
			{Name: "Phi-4", FunctionCalling: true},
			{Name: "Phi-4-mini-instruct", FunctionCalling: true},
			{Name: "DeepSeek-V3.1", FunctionCalling: true},
			{Name: "Llama-3.3-70B-Instruct", FunctionCalling: true},
			{Name: "Meta-Llama-3.1-8B-Instruct", FunctionCalling: true},
			{Name: "Mistral-large", FunctionCalling: true},
			{Name: "cohere-command-a", FunctionCalling: true},
			{Name: "grok-3", FunctionCalling: true},
			// Claude models on Foundry (Anthropic Messages API).
			{Name: "claude-opus-4-8", FunctionCalling: true},
			{Name: "claude-opus-5", FunctionCalling: true},
			{Name: "claude-sonnet-5", FunctionCalling: true},
			{Name: "claude-sonnet-4-6", FunctionCalling: true},
			{Name: "claude-haiku-4-5", FunctionCalling: true},
		},
		v1alpha3.ModelProviderOllama: ollamaCatalogModels(),
		v1alpha3.ModelProviderGemini: {
			// Gemini 3 family
			{Name: "gemini-3.5-flash", FunctionCalling: true},
			{Name: "gemini-3.5-flash-lite", FunctionCalling: true},
			{Name: "gemini-3.1-pro", FunctionCalling: true},
			{Name: "gemini-3-pro", FunctionCalling: true},
			{Name: "gemini-3-flash", FunctionCalling: true},
			{Name: "gemini-3.1-flash-lite", FunctionCalling: true},
			// Gemini 2.5 family (still available)
			{Name: "gemini-2.5-pro", FunctionCalling: true},
			{Name: "gemini-2.5-flash", FunctionCalling: true},
			{Name: "gemini-2.5-flash-lite", FunctionCalling: true},
		},
		v1alpha3.ModelProviderGeminiVertexAI: {
			{Name: "gemini-3.5-flash", FunctionCalling: true},
			{Name: "gemini-3.5-flash-lite", FunctionCalling: true},
			{Name: "gemini-3.1-pro", FunctionCalling: true},
			{Name: "gemini-3-pro", FunctionCalling: true},
			{Name: "gemini-3-flash", FunctionCalling: true},
			{Name: "gemini-3.1-flash-lite", FunctionCalling: true},
			{Name: "gemini-2.5-pro", FunctionCalling: true},
			{Name: "gemini-2.5-flash", FunctionCalling: true},
			{Name: "gemini-2.5-flash-lite", FunctionCalling: true},
		},
		v1alpha3.ModelProviderAnthropicVertexAI: {
			{Name: "claude-opus-4-8", FunctionCalling: true},
			{Name: "claude-opus-4-7", FunctionCalling: true},
			{Name: "claude-sonnet-5", FunctionCalling: true},
			{Name: "claude-sonnet-4-6", FunctionCalling: true},
			// Legacy (@-dated form)
			{Name: "claude-opus-4-1@20250805", FunctionCalling: true},
			{Name: "claude-sonnet-4@20250514", FunctionCalling: true},
			{Name: "claude-haiku-4-5@20251001", FunctionCalling: true},
		},
		v1alpha3.ModelProviderBedrock: {
			{Name: "global.anthropic.claude-fable-5", FunctionCalling: true},
			{Name: "global.anthropic.claude-opus-4-8", FunctionCalling: true},
			{Name: "global.anthropic.claude-opus-4-7", FunctionCalling: true},
			{Name: "global.anthropic.claude-sonnet-5", FunctionCalling: true},
			{Name: "global.anthropic.claude-sonnet-4-6", FunctionCalling: true},
			{Name: "us.anthropic.claude-haiku-4-5-20251001-v1:0", FunctionCalling: true},
			// Older but still valid (inference-profile required)
			{Name: "global.anthropic.claude-sonnet-4-5-20250929-v1:0", FunctionCalling: true},
			{Name: "global.anthropic.claude-opus-4-5-20251101-v1:0", FunctionCalling: true},
			// Legacy Claude 3.x (mostly retired/legacy on Bedrock)
			{Name: "anthropic.claude-3-sonnet-20240229-v1:0", FunctionCalling: true},
			{Name: "us.anthropic.claude-3-5-haiku-20241022-v1:0", FunctionCalling: true},
			// Amazon Nova
			{Name: "us.amazon.nova-2-lite-v1:0", FunctionCalling: false},
		},
		v1alpha3.ModelProviderSAPAICore: {
			// Anthropic (via SAP Generative AI Hub proxy naming)
			{Name: "anthropic--claude-4.7-opus", FunctionCalling: true},
			{Name: "anthropic--claude-4.6-sonnet", FunctionCalling: true},
			{Name: "anthropic--claude-4.6-opus", FunctionCalling: true},
			{Name: "anthropic--claude-4.5-sonnet", FunctionCalling: true},
			{Name: "anthropic--claude-4.5-opus", FunctionCalling: true},
			{Name: "anthropic--claude-4.5-haiku", FunctionCalling: true},
			{Name: "anthropic--claude-4-sonnet", FunctionCalling: true},
			{Name: "anthropic--claude-4-opus", FunctionCalling: true},
			{Name: "anthropic--claude-3-haiku", FunctionCalling: true},
			// OpenAI
			{Name: "gpt-5.4", FunctionCalling: true},
			{Name: "gpt-5.4-nano", FunctionCalling: true},
			{Name: "gpt-5.2", FunctionCalling: true},
			{Name: "gpt-5", FunctionCalling: true},
			{Name: "gpt-5-mini", FunctionCalling: true},
			{Name: "gpt-5-nano", FunctionCalling: true},
			{Name: "gpt-4o", FunctionCalling: true},
			{Name: "gpt-4o-mini", FunctionCalling: true},
			{Name: "gpt-4.1", FunctionCalling: true},
			{Name: "gpt-4.1-mini", FunctionCalling: true},
			{Name: "gpt-4.1-nano", FunctionCalling: true},
			{Name: "o3", FunctionCalling: true},
			{Name: "o3-mini", FunctionCalling: true},
			{Name: "o4-mini", FunctionCalling: true},
			// Gemini
			{Name: "gemini-3-pro-preview", FunctionCalling: true},
			{Name: "gemini-3.1-flash-lite", FunctionCalling: true},
			{Name: "gemini-2.5-pro", FunctionCalling: true},
			{Name: "gemini-2.5-flash", FunctionCalling: true},
			{Name: "gemini-2.5-flash-lite", FunctionCalling: true},
			// Amazon Nova
			{Name: "amazon--nova-premier", FunctionCalling: true},
			{Name: "amazon--nova-pro", FunctionCalling: true},
			{Name: "amazon--nova-lite", FunctionCalling: false},
			{Name: "amazon--nova-micro", FunctionCalling: false},
			// Meta / Mistral / Cohere
			{Name: "meta--llama3-70b-instruct", FunctionCalling: false},
			{Name: "mistralai--mistral-large-instruct", FunctionCalling: true},
			{Name: "mistralai--mistral-small-instruct", FunctionCalling: true},
			{Name: "mistralai--mistral-medium-instruct", FunctionCalling: true},
			{Name: "cohere--command-a-reasoning", FunctionCalling: true},
			// DeepSeek / Qwen
			{Name: "deepseek-v3.2", FunctionCalling: true},
			{Name: "deepseek-r1-0528", FunctionCalling: true},
			{Name: "qwen3-max", FunctionCalling: true},
			{Name: "qwen3.5-plus", FunctionCalling: true},
			{Name: "qwen-turbo", FunctionCalling: true},
			{Name: "qwen-flash", FunctionCalling: true},
			// Perplexity Sonar
			{Name: "sonar-deep-research", FunctionCalling: false},
			{Name: "sonar-pro", FunctionCalling: false},
			{Name: "sonar", FunctionCalling: false},
			// SAP
			{Name: "sap-abap-1", FunctionCalling: false},
		},
		v1alpha3.ModelProviderMistral: {
			{Name: "mistral-large-latest", FunctionCalling: true},
			{Name: "mistral-medium-latest", FunctionCalling: true},
			{Name: "mistral-small-latest", FunctionCalling: true},
			{Name: "magistral-medium-latest", FunctionCalling: true},
			{Name: "magistral-small-latest", FunctionCalling: true},
			{Name: "codestral-latest", FunctionCalling: true},
			{Name: "ministral-8b-latest", FunctionCalling: true},
			{Name: "ministral-3b-latest", FunctionCalling: true},
			{Name: "pixtral-large-latest", FunctionCalling: true},
			{Name: "open-mistral-nemo", FunctionCalling: true},
		},
	}
}

func (s *Service) ListSupportedModelProviders(context.Context) []ProviderDefinition {
	providersData := []struct {
		providerEnum v1alpha3.ModelProvider
		configType   reflect.Type
	}{
		{v1alpha3.ModelProviderOpenAI, reflect.TypeFor[v1alpha3.OpenAIConfig]()},
		{v1alpha3.ModelProviderAnthropic, reflect.TypeFor[v1alpha3.AnthropicConfig]()},
		{v1alpha3.ModelProviderAzureOpenAI, reflect.TypeFor[v1alpha3.AzureOpenAIConfig]()},
		{v1alpha3.ModelProviderFoundry, reflect.TypeFor[v1alpha3.FoundryConfig]()},
		{v1alpha3.ModelProviderOllama, reflect.TypeFor[v1alpha3.OllamaConfig]()},
		{v1alpha3.ModelProviderGemini, reflect.TypeFor[v1alpha3.GeminiConfig]()},
		{v1alpha3.ModelProviderGeminiVertexAI, reflect.TypeFor[v1alpha3.GeminiVertexAIConfig]()},
		{v1alpha3.ModelProviderAnthropicVertexAI, reflect.TypeFor[v1alpha3.AnthropicVertexAIConfig]()},
		{v1alpha3.ModelProviderBedrock, reflect.TypeFor[v1alpha3.BedrockConfig]()},
		{v1alpha3.ModelProviderSAPAICore, reflect.TypeFor[v1alpha3.SAPAICoreConfig]()},
		{v1alpha3.ModelProviderMistral, reflect.TypeFor[v1alpha3.MistralConfig]()},
	}

	providers := []ProviderDefinition{}
	for _, providerData := range providersData {
		providers = append(providers, providerDefinition(
			string(providerData.providerEnum),
			getStructJSONKeys(providerData.configType),
			getRequiredKeysForModelProvider(providerData.providerEnum),
		))
	}
	return providers
}

func (s *Service) ListConfiguredProviders(ctx context.Context) ([]*ConfiguredProvider, error) {
	configs, err := s.providerConfigs.List(ctx, "")
	if err != nil {
		return nil, err
	}
	result := make([]*ConfiguredProvider, 0, len(configs))
	for _, config := range configs {
		snapshot := s.providerSnapshot(config)
		result = append(result, &ConfiguredProvider{Ref: snapshot.Ref, Type: snapshot.Type, Endpoint: snapshot.Endpoint, State: snapshot.State, Error: snapshot.Error})
	}
	return result, nil
}

func (s *Service) providerSnapshot(config *v1alpha3.ModelProviderConfig) *modeldiscovery.Snapshot {
	if s.discovery != nil {
		snapshot := s.discovery.Get(types.NamespacedName{Namespace: config.Namespace, Name: config.Name})
		if snapshot != nil && snapshot.Uid == string(config.UID) && snapshot.Generation == config.Generation {
			return snapshot
		}
	}
	return &modeldiscovery.Snapshot{Ref: &apiv1.ResourceReference{Namespace: config.Namespace, Name: config.Name}, Uid: string(config.UID), Generation: config.Generation, Type: string(config.Spec.Type), Endpoint: config.Spec.GetEndpoint(), State: apiv1.DiscoveryState_DISCOVERY_STATE_PENDING}
}

func (s *Service) GetProviderModels(ctx context.Context, request GetProviderModelsRequest) (*modeldiscovery.Snapshot, error) {
	if request.Refresh {
		if _, shared := auth.ShareContextFrom(ctx); shared {
			return nil, serviceerrors.NewPermissionDenied("Session shares cannot refresh provider models", nil)
		}
		if err := s.providerConfigs.Authorize(ctx, auth.VerbUpdate, request.Ref); err != nil {
			return nil, err
		}
		if s.discovery == nil {
			return nil, serviceerrors.NewUnavailable("Model discovery is starting", nil)
		}
		return s.discovery.Refresh(ctx, request.Ref)
	}
	config, err := s.providerConfigs.Get(ctx, request.Ref)
	if err != nil {
		return nil, err
	}
	return s.providerSnapshot(config), nil
}

func providerDefinition(name string, allKeys, requiredKeys []string) ProviderDefinition {
	requiredSet := make(map[string]struct{}, len(requiredKeys))
	for _, key := range requiredKeys {
		requiredSet[key] = struct{}{}
	}

	optionalKeys := []string{}
	for _, key := range allKeys {
		if key == "endpointFrom" {
			continue
		}
		if _, required := requiredSet[key]; !required {
			optionalKeys = append(optionalKeys, key)
		}
	}

	return ProviderDefinition{
		Name:           name,
		Type:           name,
		RequiredParams: requiredKeys,
		OptionalParams: optionalKeys,
	}
}

func getRequiredKeysForModelProvider(providerType v1alpha3.ModelProvider) []string {
	switch providerType {
	case v1alpha3.ModelProviderAzureOpenAI:
		return []string{"azureEndpoint"}
	case v1alpha3.ModelProviderBedrock:
		return []string{"region"}
	case v1alpha3.ModelProviderSAPAICore:
		return []string{"baseUrl"}
	case v1alpha3.ModelProviderFoundry:
		return []string{"deployment", "endpoint"}
	case v1alpha3.ModelProviderOpenAI, v1alpha3.ModelProviderAnthropic, v1alpha3.ModelProviderOllama:
		return []string{}
	default:
		return []string{}
	}
}

func getStructJSONKeys(structType reflect.Type) []string {
	keys := []string{}
	if structType.Kind() != reflect.Struct {
		return keys
	}
	for field := range structType.Fields() {
		jsonTag := field.Tag.Get("json")
		if jsonTag != "" && jsonTag != "-" {
			tagParts := strings.Split(jsonTag, ",")
			keys = append(keys, tagParts[0])
		}
	}
	return keys
}
