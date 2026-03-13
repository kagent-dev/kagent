package handlers

import (
	"net/http"
	"reflect"

	"github.com/kagent-dev/kagent/go/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/controller/reconciler"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// ModelProviderResponse is the API response format for listing model providers
type ModelProviderResponse struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Endpoint string `json:"endpoint"`
}

// ModelsResponse is the API response format for listing models
type ModelsResponse struct {
	Provider string   `json:"provider"`
	Models   []string `json:"models"`
}

// ModelProviderConfigHandler handles model provider config requests
type ModelProviderConfigHandler struct {
	*Base
	reconciler reconciler.KagentReconciler
}

// NewModelProviderConfigHandler creates a new ModelProviderConfigHandler
func NewModelProviderConfigHandler(base *Base, rcnclr reconciler.KagentReconciler) *ModelProviderConfigHandler {
	return &ModelProviderConfigHandler{
		Base:       base,
		reconciler: rcnclr,
	}
}

// Helper function to get JSON keys specifically marked as required
func getRequiredKeysForModelProvider(providerType v1alpha2.ModelProvider) []string {
	switch providerType {
	case v1alpha2.ModelProviderAzureOpenAI:
		// Based on the +required comments in the AzureOpenAIConfig struct definition
		return []string{"azureEndpoint", "apiVersion"}
	case v1alpha2.ModelProviderBedrock:
		// Region is required for Bedrock
		return []string{"region"}
	case v1alpha2.ModelProviderOpenAI, v1alpha2.ModelProviderAnthropic, v1alpha2.ModelProviderOllama:
		// These providers currently have no fields marked as strictly required in the API definition
		return []string{}
	default:
		// Unknown provider, return empty
		return []string{}
	}
}

func getRequiredKeysForMemoryProvider(providerType v1alpha1.MemoryProvider) []string {
	switch providerType {
	case v1alpha1.Pinecone:
		return []string{"indexHost"}
	default:
		return []string{}
	}
}

func (h *ModelProviderConfigHandler) HandleListSupportedMemoryProviders(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("provider-handler").WithValues("operation", "list-supported-memory-providers")

	log.Info("Listing supported memory providers with parameters")

	providersData := []struct {
		providerEnum v1alpha1.MemoryProvider
		configType   reflect.Type
	}{
		{v1alpha1.Pinecone, reflect.TypeFor[v1alpha1.PineconeConfig]()},
	}

	providersResponse := []map[string]any{}

	for _, pData := range providersData {
		allKeys := getStructJSONKeys(pData.configType)
		requiredKeys := getRequiredKeysForMemoryProvider(pData.providerEnum)
		requiredSet := make(map[string]struct{})
		for _, k := range requiredKeys {
			requiredSet[k] = struct{}{}
		}

		optionalKeys := []string{}
		for _, k := range allKeys {
			if _, isRequired := requiredSet[k]; !isRequired {
				optionalKeys = append(optionalKeys, k)
			}
		}

		providersResponse = append(providersResponse, map[string]any{
			"name":           string(pData.providerEnum),
			"type":           string(pData.providerEnum),
			"requiredParams": requiredKeys,
			"optionalParams": optionalKeys,
		})
	}

	data := api.NewResponse(providersResponse, "Successfully listed supported memory providers", false)
	RespondWithJSON(w, http.StatusOK, data)
}

func (h *ModelProviderConfigHandler) HandleListSupportedModelProviders(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("provider-handler").WithValues("operation", "list-supported-model-providers")

	log.Info("Listing supported model providers with parameters")

	providersData := []struct {
		providerEnum v1alpha2.ModelProvider
		configType   reflect.Type
	}{
		{v1alpha2.ModelProviderOpenAI, reflect.TypeFor[v1alpha2.OpenAIConfig]()},
		{v1alpha2.ModelProviderAnthropic, reflect.TypeFor[v1alpha2.AnthropicConfig]()},
		{v1alpha2.ModelProviderAzureOpenAI, reflect.TypeFor[v1alpha2.AzureOpenAIConfig]()},
		{v1alpha2.ModelProviderOllama, reflect.TypeFor[v1alpha2.OllamaConfig]()},
		{v1alpha2.ModelProviderGemini, reflect.TypeFor[v1alpha2.GeminiConfig]()},
		{v1alpha2.ModelProviderGeminiVertexAI, reflect.TypeFor[v1alpha2.GeminiVertexAIConfig]()},
		{v1alpha2.ModelProviderAnthropicVertexAI, reflect.TypeFor[v1alpha2.AnthropicVertexAIConfig]()},
		{v1alpha2.ModelProviderBedrock, reflect.TypeFor[v1alpha2.BedrockConfig]()},
	}

	providersResponse := []map[string]any{}

	for _, pData := range providersData {
		allKeys := getStructJSONKeys(pData.configType)
		requiredKeys := getRequiredKeysForModelProvider(pData.providerEnum)
		requiredSet := make(map[string]struct{})
		for _, k := range requiredKeys {
			requiredSet[k] = struct{}{}
		}

		optionalKeys := []string{}
		for _, k := range allKeys {
			if _, isRequired := requiredSet[k]; !isRequired {
				optionalKeys = append(optionalKeys, k)
			}
		}

		providersResponse = append(providersResponse, map[string]any{
			"name":           string(pData.providerEnum),
			"type":           string(pData.providerEnum),
			"requiredParams": requiredKeys,
			"optionalParams": optionalKeys,
		})
	}

	data := api.NewResponse(providersResponse, "Successfully listed supported model providers", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleListConfiguredProviders returns the list of model providers configured via ModelProviderConfig CRDs.
// GET /api/modelproviderconfigs/configured
func (h *ModelProviderConfigHandler) HandleListConfiguredProviders(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("modelprovider-handler").WithValues("operation", "list-configured-providers")

	log.Info("Listing configured model providers")

	// List ModelProviderConfig CRs directly from Kubernetes
	namespace := utils.GetResourceNamespace()
	var modelProviderConfigList v1alpha2.ModelProviderConfigList
	if err := h.KubeClient.List(r.Context(), &modelProviderConfigList, client.InNamespace(namespace)); err != nil {
		log.Error(err, "Failed to list model provider configs")
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Filter for Ready model providers and transform to API response format
	var response []ModelProviderResponse
	for _, p := range modelProviderConfigList.Items {
		// Only include Ready model providers
		if meta.IsStatusConditionTrue(p.Status.Conditions, v1alpha2.ModelProviderConfigConditionTypeReady) {
			response = append(response, ModelProviderResponse{
				Name:     p.Name,
				Type:     string(p.Spec.Type),
				Endpoint: p.Spec.GetEndpoint(),
			})
		}
	}

	log.Info("Successfully listed configured model providers", "count", len(response))
	data := api.NewResponse(response, "Successfully listed configured model providers", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleGetProviderModels discovers and returns available models for a specific model provider.
// GET /api/modelproviderconfigs/configured/{name}/models?refresh=true
func (h *ModelProviderConfigHandler) HandleGetProviderModels(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("modelprovider-handler").WithValues("operation", "get-provider-models")

	providerName, err := GetPathParam(r, "name")
	if err != nil {
		log.Info("Missing model provider name parameter")
		RespondWithError(w, http.StatusBadRequest, "Model provider name is required")
		return
	}

	log = log.WithValues("provider", providerName)
	log.Info("Getting models for model provider")

	// Check for refresh query parameter
	forceRefresh := r.URL.Query().Get("refresh") == "true"

	namespace := utils.GetResourceNamespace()
	var models []string
	if forceRefresh {
		// Call reconciler to trigger fresh discovery
		log.Info("Forcing fresh model discovery")
		models, err = h.reconciler.RefreshModelProviderConfigModels(r.Context(), namespace, providerName)
		if err != nil {
			log.Error(err, "Failed to refresh models for model provider")
			RespondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		// Read cached models from ModelProviderConfig.Status
		p := &v1alpha2.ModelProviderConfig{}
		if err := h.KubeClient.Get(r.Context(), client.ObjectKey{
			Namespace: namespace,
			Name:      providerName,
		}, p); err != nil {
			log.Error(err, "Failed to get model provider config")
			RespondWithError(w, http.StatusNotFound, err.Error())
			return
		}

		if len(p.Status.DiscoveredModels) == 0 {
			log.Info("No models discovered for model provider, try refreshing")
			RespondWithError(w, http.StatusNotFound, "No models discovered for model provider, try refreshing")
			return
		}

		models = p.Status.DiscoveredModels
	}

	response := ModelsResponse{
		Provider: providerName,
		Models:   models,
	}

	log.Info("Successfully retrieved models for model provider", "count", len(models))
	data := api.NewResponse(response, "Successfully retrieved models", false)
	RespondWithJSON(w, http.StatusOK, data)
}
