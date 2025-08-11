package handlers

import (
	"net/http"

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
		"openAI": {
			{Name: "gpt-5", FunctionCalling: true},
			{Name: "gpt-5-mini", FunctionCalling: true},
			{Name: "gpt-5-nano", FunctionCalling: true},
			{Name: "gpt-4o", FunctionCalling: true},
			{Name: "gpt-4-turbo", FunctionCalling: true},
			{Name: "gpt-4", FunctionCalling: true},
			{Name: "gpt-3.5-turbo", FunctionCalling: true},
		},
		"anthropic": {
			{Name: "claude-3-opus-20240229", FunctionCalling: true},
			{Name: "claude-3-sonnet-20240229", FunctionCalling: true},
			{Name: "claude-3-haiku-20240307", FunctionCalling: true},
			{Name: "claude-2.1", FunctionCalling: false},
			{Name: "claude-2.0", FunctionCalling: false},
		},
		"azureOpenAI": {
			{Name: "gpt-4", FunctionCalling: true},
			{Name: "gpt-35-turbo", FunctionCalling: true},
		},
		"ollama": {
			{Name: "llama2", FunctionCalling: false},
			{Name: "llama2:13b", FunctionCalling: false},
			{Name: "llama2:70b", FunctionCalling: false},
			{Name: "mistral", FunctionCalling: false},
			{Name: "mixtral", FunctionCalling: false},
		},
		"gemini": {
			{Name: "gemini-pro", FunctionCalling: true},
			{Name: "gemini-pro-vision", FunctionCalling: false},
		},
		"geminiVertexAI": {
			{Name: "gemini-pro", FunctionCalling: true},
			{Name: "gemini-pro-vision", FunctionCalling: false},
		},
		"anthropicVertexAI": {
			{Name: "claude-3-opus-20240229", FunctionCalling: true},
			{Name: "claude-3-sonnet-20240229", FunctionCalling: true},
			{Name: "claude-3-haiku-20240307", FunctionCalling: true},
		},
	}

	log.Info("Successfully listed supported models", "count", len(supportedModels))
	data := api.NewResponse(supportedModels, "Successfully listed supported models", false)
	RespondWithJSON(w, http.StatusOK, data)
}
