package handlers

import (
	"net/http"

	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	kclient "github.com/kagent-dev/kagent/go/pkg/client"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
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
	supportedModels := kclient.ProviderModels{
		v1alpha2.ModelProviderOpenAI: {
			{Name: "gpt-5", FunctionCalling: true},
			{Name: "gpt-5-mini", FunctionCalling: true},
			{Name: "gpt-5-nano", FunctionCalling: true},
			{Name: "gpt-4o", FunctionCalling: true},
			{Name: "o4-mini", FunctionCalling: true},
			{Name: "gpt-4-turbo", FunctionCalling: true},
			{Name: "gpt-4", FunctionCalling: true},
			{Name: "gpt-3.5-turbo", FunctionCalling: true},
		},
		v1alpha2.ModelProviderAnthropic: {
			{Name: "claude-opus-4-1-20250805", FunctionCalling: true},
			{Name: "claude-opus-4-20250514", FunctionCalling: true},
			{Name: "claude-sonnet-4-20250514", FunctionCalling: true},
			{Name: "claude-3-7-sonnet-20250219", FunctionCalling: true},
			{Name: "claude-3-5-sonnet-20240620", FunctionCalling: true},
		},
		v1alpha2.ModelProviderAzureOpenAI: {
			{Name: "gpt-4", FunctionCalling: true},
			{Name: "gpt-35-turbo", FunctionCalling: true},
			{Name: "gpt-oss-120b", FunctionCalling: true},
			{Name: "gpt-4.1", FunctionCalling: true},
			{Name: "gpt-4.1-mini", FunctionCalling: true},
			{Name: "gpt-4.1-nano", FunctionCalling: true},
			{Name: "gpt-4o", FunctionCalling: true},
			{Name: "gpt-4o-mini", FunctionCalling: true}, {Name: "o4-mini", FunctionCalling: true},
			{Name: "o3", FunctionCalling: true},
			{Name: "o3-mini", FunctionCalling: true},
		},
		v1alpha2.ModelProviderOllama: {
			{Name: "llama2", FunctionCalling: false},
			{Name: "llama2:13b", FunctionCalling: false},
			{Name: "llama2:70b", FunctionCalling: false},
			{Name: "mistral", FunctionCalling: false},
			{Name: "mixtral", FunctionCalling: false},
		},
		v1alpha2.ModelProviderGemini: {
			{Name: "gemini-2.5-pro", FunctionCalling: true},
			{Name: "gemini-2.5-flash", FunctionCalling: true},
			{Name: "gemini-2.5-flash-lite", FunctionCalling: true},
			{Name: "gemini-2.0-flash", FunctionCalling: true},
			{Name: "gemini-2.0-flash-lite", FunctionCalling: true},
		},
		v1alpha2.ModelProviderGeminiVertexAI: {
			{Name: "gemini-2.5-pro", FunctionCalling: true},
			{Name: "gemini-2.5-flash", FunctionCalling: true},
			{Name: "gemini-2.5-flash-lite", FunctionCalling: true},
			{Name: "gemini-2.0-flash", FunctionCalling: true},
			{Name: "gemini-2.0-flash-lite", FunctionCalling: true},
		},
		v1alpha2.ModelProviderAnthropicVertexAI: {
			{Name: "claude-opus-4-1@20250805", FunctionCalling: true},
			{Name: "claude-sonnet-4@20250514", FunctionCalling: true},
			{Name: "claude-3-5-haiku@20241022", FunctionCalling: true},
		},
	}

	log.Info("Successfully listed supported models", "count", len(supportedModels))
	data := api.NewResponse(supportedModels, "Successfully listed supported models", false)
	RespondWithJSON(w, http.StatusOK, data)
}
