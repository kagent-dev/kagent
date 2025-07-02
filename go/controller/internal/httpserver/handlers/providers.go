package handlers

import (
	"net/http"
	"reflect"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// ProviderHandler handles provider requests
type ProviderHandler struct {
	*Base
}

// NewProviderHandler creates a new ProviderHandler
func NewProviderHandler(base *Base) *ProviderHandler {
	return &ProviderHandler{Base: base}
}

// Helper function to get JSON keys specifically marked as required
func getRequiredKeysForModelProvider(providerType v1alpha1.ModelProvider) []string {
	switch providerType {
	case v1alpha1.AzureOpenAI:
		// Based on the +required comments in the AzureOpenAIConfig struct definition
		return []string{"azureEndpoint", "apiVersion"}
	case v1alpha1.OpenAI, v1alpha1.Anthropic, v1alpha1.Ollama:
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

func (h *ProviderHandler) HandleListSupportedMemoryProviders(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("provider-handler").WithValues("operation", "list-supported-memory-providers")

	log.Info("Listing supported memory providers with parameters")

	providersData := []struct {
		providerEnum v1alpha1.MemoryProvider
		configType   reflect.Type
	}{
		{v1alpha1.Pinecone, reflect.TypeOf(v1alpha1.PineconeConfig{})},
	}

	providersResponse := []map[string]interface{}{}

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

		providersResponse = append(providersResponse, map[string]interface{}{
			"name":           string(pData.providerEnum),
			"type":           string(pData.providerEnum),
			"requiredParams": requiredKeys,
			"optionalParams": optionalKeys,
		})
	}

	RespondWithJSON(w, http.StatusOK, providersResponse)
}

func (h *ProviderHandler) HandleListSupportedModelProviders(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("provider-handler").WithValues("operation", "list-supported-model-providers")

	log.Info("Listing supported model providers with parameters")

	providersData := []struct {
		providerEnum v1alpha1.ModelProvider
		configType   reflect.Type
	}{
		{v1alpha1.OpenAI, reflect.TypeOf(v1alpha1.OpenAIConfig{})},
		{v1alpha1.Anthropic, reflect.TypeOf(v1alpha1.AnthropicConfig{})},
		{v1alpha1.AzureOpenAI, reflect.TypeOf(v1alpha1.AzureOpenAIConfig{})},
		{v1alpha1.Ollama, reflect.TypeOf(v1alpha1.OllamaConfig{})},
	}

	providersResponse := []map[string]interface{}{}

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

		providersResponse = append(providersResponse, map[string]interface{}{
			"name":           string(pData.providerEnum),
			"type":           string(pData.providerEnum),
			"requiredParams": requiredKeys,
			"optionalParams": optionalKeys,
		})
	}

	RespondWithJSON(w, http.StatusOK, providersResponse)
}
