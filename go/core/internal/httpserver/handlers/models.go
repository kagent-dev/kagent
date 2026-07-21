package handlers

import (
	"net/http"

	kclient "github.com/kagent-dev/kagent/go/api/client"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// ModelHandler handles model requests
type ModelHandler struct {
	*Base
}

// NewModelHandler creates a new ModelHandler
func NewModelHandler(base *Base) *ModelHandler {
	return &ModelHandler{Base: base}
}

func (h *ModelHandler) HandleListSupportedModels(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("model-handler").WithValues("operation", "list-supported-models")

	log.Info("Listing supported models")

	// Create a map of provider names to their supported models
	// The keys need to match what the UI expects (camelCase for API keys)
	// List of models is built from the following sites:
	//   OpenAI   -> https://developers.openai.com/api/docs/models
	//   Anthropic-> https://platform.claude.com/docs/en/about-claude/models/model-ids-and-versions
	//   Azure    -> https://learn.microsoft.com/azure/foundry/foundry-models
	//   Gemini   -> https://ai.google.dev/gemini-api/docs/models
	//   Bedrock  -> https://docs.aws.amazon.com/bedrock/latest/userguide/model-cards.html
	//   Vertex   -> https://platform.claude.com/docs/en/build-with-claude/claude-in-google-cloud-vertex-ai
	//   SAP      -> SAP Note 3437766 (live model/version table)
	supportedModels := kclient.ProviderModels{
		v1alpha2.ModelProviderOpenAI: {
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
		v1alpha2.ModelProviderAnthropic: {
			{Name: "claude-fable-5", FunctionCalling: true},
			{Name: "claude-opus-4-8", FunctionCalling: true},
			{Name: "claude-opus-4-7", FunctionCalling: true},
			{Name: "claude-opus-4-6", FunctionCalling: true},
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
		v1alpha2.ModelProviderAzureOpenAI: {
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
		v1alpha2.ModelProviderOllama: {
			// FunctionCalling flags corrected: recent Ollama builds of these models
			// support tool calling.
			{Name: "llama3.3", FunctionCalling: true},
			{Name: "llama3.1", FunctionCalling: true},
			{Name: "qwen2.5-coder", FunctionCalling: true},
			{Name: "mistral", FunctionCalling: true},
			{Name: "mixtral", FunctionCalling: true},
			{Name: "deepseek-r1", FunctionCalling: false}, // tool support inconsistent across tags
			{Name: "llama2", FunctionCalling: false},
			{Name: "llama2:13b", FunctionCalling: false},
			{Name: "llama2:70b", FunctionCalling: false},
		},
		v1alpha2.ModelProviderGemini: {
			// Gemini 3 family
			{Name: "gemini-3.5-flash", FunctionCalling: true},
			{Name: "gemini-3.1-pro", FunctionCalling: true},
			{Name: "gemini-3-pro", FunctionCalling: true},
			{Name: "gemini-3-flash", FunctionCalling: true},
			{Name: "gemini-3.1-flash-lite", FunctionCalling: true},
			// Gemini 2.5 family (still available)
			{Name: "gemini-2.5-pro", FunctionCalling: true},
			{Name: "gemini-2.5-flash", FunctionCalling: true},
			{Name: "gemini-2.5-flash-lite", FunctionCalling: true},
		},
		v1alpha2.ModelProviderGeminiVertexAI: {
			{Name: "gemini-3.5-flash", FunctionCalling: true},
			{Name: "gemini-3.1-pro", FunctionCalling: true},
			{Name: "gemini-3-pro", FunctionCalling: true},
			{Name: "gemini-3-flash", FunctionCalling: true},
			{Name: "gemini-3.1-flash-lite", FunctionCalling: true},
			{Name: "gemini-2.5-pro", FunctionCalling: true},
			{Name: "gemini-2.5-flash", FunctionCalling: true},
			{Name: "gemini-2.5-flash-lite", FunctionCalling: true},
		},
		v1alpha2.ModelProviderAnthropicVertexAI: {
			{Name: "claude-opus-4-8", FunctionCalling: true},
			{Name: "claude-opus-4-7", FunctionCalling: true},
			{Name: "claude-sonnet-5", FunctionCalling: true},
			{Name: "claude-sonnet-4-6", FunctionCalling: true},
			// Legacy (@-dated form)
			{Name: "claude-opus-4-1@20250805", FunctionCalling: true},
			{Name: "claude-sonnet-4@20250514", FunctionCalling: true},
			{Name: "claude-haiku-4-5@20251001", FunctionCalling: true},
		},
		v1alpha2.ModelProviderBedrock: {
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
		v1alpha2.ModelProviderSAPAICore: {
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
	}

	log.Info("Successfully listed supported models", "count", len(supportedModels))
	data := api.NewResponse(supportedModels, "Successfully listed supported models", false)
	RespondWithJSON(w, http.StatusOK, data)
}
