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

func (h *ProviderHandler) HandleListSupportedProviders(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("provider-handler").WithValues("operation", "list-supported-providers")

	log.Info("Listing supported providers with parameters")

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
		requiredKeys := getRequiredKeys(pData.providerEnum)
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
