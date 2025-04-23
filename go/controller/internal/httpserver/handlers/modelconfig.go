package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// ModelConfigResponse defines the structure for the model config API response.
type ModelConfigResponse struct {
	Name             string                 `json:"name"`
	Namespace        string                 `json:"namespace"`
	ProviderName     string                 `json:"providerName"`
	Model            string                 `json:"model"`
	APIKeySecretName string                 `json:"apiKeySecretName"`
	APIKeySecretKey  string                 `json:"apiKeySecretKey"`
	ModelParams      map[string]interface{} `json:"modelParams"`
}

// ModelConfigHandler handles model configuration requests
type ModelConfigHandler struct {
	*Base
}

// NewModelConfigHandler creates a new ModelConfigHandler
func NewModelConfigHandler(base *Base) *ModelConfigHandler {
	return &ModelConfigHandler{Base: base}
}

// flattenStructToMap uses reflection to add fields of a struct to a map,
// using json tags as keys.
func flattenStructToMap(data interface{}, targetMap map[string]interface{}) {
	val := reflect.ValueOf(data)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	// Ensure it's a struct
	if val.Kind() != reflect.Struct {
		return // Or handle error appropriately
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		fieldValue := val.Field(i)

		// Get JSON tag
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			// Skip fields without json tags or explicitly ignored
			continue
		}

		// Handle tag options like ",omitempty"
		tagParts := strings.Split(jsonTag, ",")
		key := tagParts[0]

		// Add to map
		if fieldValue.Kind() == reflect.Ptr && fieldValue.IsNil() {
			targetMap[key] = nil
		} else {
			targetMap[key] = fieldValue.Interface()
		}
	}
}

// HandleListModelConfigs handles GET /api/modelconfigs requests
func (h *ModelConfigHandler) HandleListModelConfigs(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("modelconfig-handler").WithValues("operation", "list")

	modelConfigs := &v1alpha1.ModelConfigList{}
	if err := h.KubeClient.List(r.Context(), modelConfigs); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list model configs from Kubernetes", err))
		return
	}

	configs := make([]ModelConfigResponse, 0)
	for _, config := range modelConfigs.Items {
		log.V(1).Info("Processing model config", "name", config.Name, "model", config.Spec.Model)
		modelParams := make(map[string]interface{})

		if config.Spec.OpenAI != nil {
			flattenStructToMap(config.Spec.OpenAI, modelParams)
		}
		if config.Spec.Anthropic != nil {
			flattenStructToMap(config.Spec.Anthropic, modelParams)
		}
		if config.Spec.AzureOpenAI != nil {
			flattenStructToMap(config.Spec.AzureOpenAI, modelParams)
		}
		if config.Spec.Ollama != nil {
			flattenStructToMap(config.Spec.Ollama, modelParams)
		}

		responseItem := ModelConfigResponse{
			Name:             config.Name,
			Namespace:        config.Namespace,
			ProviderName:     string(config.Spec.Provider),
			Model:            config.Spec.Model,
			APIKeySecretName: config.Spec.APIKeySecretName,
			APIKeySecretKey:  config.Spec.APIKeySecretKey,
			ModelParams:      modelParams,
		}
		configs = append(configs, responseItem)
	}

	log.Info("Successfully listed model configs", "count", len(configs))
	RespondWithJSON(w, http.StatusOK, configs)
}

// HandleGetModelConfig handles GET /api/modelconfigs/{configName} requests
func (h *ModelConfigHandler) HandleGetModelConfig(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("modelconfig-handler").WithValues("operation", "get")

	configName, err := GetPathParam(r, "configName")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get config name from path", err))
		return
	}
	log = log.WithValues("configName", configName)

	log.V(1).Info("Getting model config from Kubernetes")
	modelConfig := &v1alpha1.ModelConfig{}
	if err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      configName,
		Namespace: common.GetResourceNamespace(),
	}, modelConfig); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("Model config not found")
			w.RespondWithError(errors.NewNotFoundError("Model config not found", nil))
			return
		}
		log.Error(err, "Failed to get model config")
		w.RespondWithError(errors.NewInternalServerError("Failed to get model config", err))
		return
	}

	log.V(1).Info("Constructing response object")
	modelParams := make(map[string]interface{})
	if modelConfig.Spec.OpenAI != nil {
		flattenStructToMap(modelConfig.Spec.OpenAI, modelParams)
	}
	if modelConfig.Spec.Anthropic != nil {
		flattenStructToMap(modelConfig.Spec.Anthropic, modelParams)
	}
	if modelConfig.Spec.AzureOpenAI != nil {
		flattenStructToMap(modelConfig.Spec.AzureOpenAI, modelParams)
	}
	if modelConfig.Spec.Ollama != nil {
		flattenStructToMap(modelConfig.Spec.Ollama, modelParams)
	}

	responseItem := ModelConfigResponse{
		Name:             modelConfig.Name,
		Namespace:        modelConfig.Namespace,
		ProviderName:     string(modelConfig.Spec.Provider),
		Model:            modelConfig.Spec.Model,
		APIKeySecretName: modelConfig.Spec.APIKeySecretName,
		APIKeySecretKey:  modelConfig.Spec.APIKeySecretKey,
		ModelParams:      modelParams,
	}

	log.Info("Successfully retrieved and formatted model config")
	RespondWithJSON(w, http.StatusOK, responseItem)
}

// Helper function to get all JSON keys from a struct type
func getStructJSONKeys(structType reflect.Type) []string {
	keys := []string{}
	if structType.Kind() != reflect.Struct {
		return keys
	}
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag != "" && jsonTag != "-" {
			tagParts := strings.Split(jsonTag, ",")
			keys = append(keys, tagParts[0])
		}
	}
	return keys
}

// Helper function to get JSON keys specifically marked as required
func getRequiredKeys(providerType v1alpha1.ModelProvider) []string {
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

func (h *ModelConfigHandler) HandleListSupportedProviders(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("modelconfig-handler").WithValues("operation", "list-supported-providers")

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

type CreateModelConfigRequest struct {
	Name            string                      `json:"name"`
	Provider        Provider                    `json:"provider"`
	Model           string                      `json:"model"`
	APIKey          string                      `json:"apiKey"`
	OpenAIParams    *v1alpha1.OpenAIConfig      `json:"openAI,omitempty"`
	AnthropicParams *v1alpha1.AnthropicConfig   `json:"anthropic,omitempty"`
	AzureParams     *v1alpha1.AzureOpenAIConfig `json:"azureOpenAI,omitempty"`
	OllamaParams    *v1alpha1.OllamaConfig      `json:"ollama,omitempty"`
}

type Provider struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func (h *ModelConfigHandler) HandleCreateModelConfig(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("modelconfig-handler").WithValues("operation", "create")

	var req CreateModelConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error(err, "Failed to decode request body")
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}
	log = log.WithValues("configName", req.Name, "provider", req.Provider.Type, "model", req.Model)
	log.Info("Received request to create model config")

	log.V(1).Info("Checking if model config already exists")
	existingConfig := &v1alpha1.ModelConfig{}
	err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      req.Name,
		Namespace: common.GetResourceNamespace(),
	}, existingConfig)
	if err == nil {
		log.Info("Model config already exists")
		w.RespondWithError(errors.NewConflictError("Model config already exists", nil))
		return
	} else if !k8serrors.IsNotFound(err) {
		log.Error(err, "Failed to check if model config exists")
		w.RespondWithError(errors.NewInternalServerError("Failed to check if model config exists", err))
		return
	}

	// --- Secret Creation ---
	providerTypeEnum := v1alpha1.ModelProvider(req.Provider.Type)
	modelConfigSpec := v1alpha1.ModelConfigSpec{
		Model:    req.Model,
		Provider: providerTypeEnum,
	}
	secret := &corev1.Secret{}

	// If the provider is Ollama, we don't need to create a secret.
	if providerTypeEnum == v1alpha1.Ollama {
		log.V(1).Info("Ollama provider, skipping secret creation")
	} else {
		apiKey := req.APIKey
		secretName := req.Name
		secretKey := fmt.Sprintf("%s_API_KEY", strings.ToUpper(req.Provider.Type))
		log.V(1).Info("Creating API key secret", "secretName", secretName, "secretKey", secretKey)
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: common.GetResourceNamespace(),
			},
			StringData: map[string]string{
				secretKey: apiKey,
			},
		}

		if err := h.KubeClient.Create(r.Context(), secret); err != nil {
			log.Error(err, "Failed to create API key secret")
			w.RespondWithError(errors.NewInternalServerError("Failed to create API key secret", err))
			return
		}
		log.V(1).Info("Successfully created API key secret")
		modelConfigSpec.APIKeySecretName = secretName
		modelConfigSpec.APIKeySecretKey = secretKey
	}

	modelConfig := &v1alpha1.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: common.GetResourceNamespace(),
		},
		Spec: modelConfigSpec,
	}

	var providerConfigErr error
	switch providerTypeEnum {
	case v1alpha1.OpenAI:
		if req.OpenAIParams != nil {
			modelConfig.Spec.OpenAI = req.OpenAIParams
			log.V(1).Info("Assigned OpenAI params to spec")
		} else {
			log.V(1).Info("No OpenAI params provided in create.")
		}
	case v1alpha1.Anthropic:
		if req.AnthropicParams != nil {
			modelConfig.Spec.Anthropic = req.AnthropicParams
			log.V(1).Info("Assigned Anthropic params to spec")
		} else {
			log.V(1).Info("No Anthropic params provided in create.")
		}
	case v1alpha1.AzureOpenAI:
		if req.AzureParams == nil {
			providerConfigErr = fmt.Errorf("azureOpenAI parameters are required for AzureOpenAI provider")
		} else {
			// Basic validation for required Azure fields (can be enhanced)
			if req.AzureParams.Endpoint == "" || req.AzureParams.APIVersion == "" {
				providerConfigErr = fmt.Errorf("missing required AzureOpenAI parameters: azureEndpoint, apiVersion")
			} else {
				modelConfig.Spec.AzureOpenAI = req.AzureParams
				log.V(1).Info("Assigned AzureOpenAI params to spec")
			}
		}
	case v1alpha1.Ollama:
		if req.OllamaParams != nil {
			modelConfig.Spec.Ollama = req.OllamaParams
			log.V(1).Info("Assigned Ollama params to spec")
		} else {
			log.V(1).Info("No Ollama params provided in create.")
		}
	default:
		providerConfigErr = fmt.Errorf("unsupported provider type: %s", req.Provider.Type)
	}

	if providerConfigErr != nil {
		log.Error(providerConfigErr, "Failed to assign provider config")
		// Clean up the created secret if config assignment fails
		log.V(1).Info("Attempting to clean up secret due to config assignment failure")
		if providerTypeEnum != v1alpha1.Ollama {
			if cleanupErr := h.KubeClient.Delete(r.Context(), secret); cleanupErr != nil {
				log.Error(cleanupErr, "Failed to cleanup secret after config assignment failure")
			}
		}
		w.RespondWithError(errors.NewBadRequestError(providerConfigErr.Error(), providerConfigErr))
		return
	}

	if err := h.KubeClient.Create(r.Context(), modelConfig); err != nil {
		log.Error(err, "Failed to create ModelConfig resource")
		// If we fail to create the ModelConfig, we should clean up the secret
		log.V(1).Info("Attempting to clean up secret after ModelConfig creation failure")
		if providerTypeEnum != v1alpha1.Ollama {
			if cleanupErr := h.KubeClient.Delete(r.Context(), secret); cleanupErr != nil {
				log.Error(cleanupErr, "Failed to cleanup secret after ModelConfig creation failure")
			}
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to create model config", err))
		return
	}

	log.Info("Successfully created model config", "name", req.Name)
	RespondWithJSON(w, http.StatusCreated, modelConfig)
}

// UpdateModelConfigRequest defines the structure for updating a model config.
// It's similar to Create, but APIKey is optional.
type UpdateModelConfigRequest struct {
	Provider        Provider                    `json:"provider"`
	Model           string                      `json:"model"`
	APIKey          *string                     `json:"apiKey,omitempty"`
	OpenAIParams    *v1alpha1.OpenAIConfig      `json:"openAI,omitempty"`
	AnthropicParams *v1alpha1.AnthropicConfig   `json:"anthropic,omitempty"`
	AzureParams     *v1alpha1.AzureOpenAIConfig `json:"azureOpenAI,omitempty"`
	OllamaParams    *v1alpha1.OllamaConfig      `json:"ollama,omitempty"`
}

func (h *ModelConfigHandler) HandleUpdateModelConfig(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("modelconfig-handler").WithValues("operation", "update")

	configName, err := GetPathParam(r, "configName")
	if err != nil {
		log.Error(err, "Failed to get config name from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get config name from path", err))
		return
	}
	log = log.WithValues("configName", configName)

	var req UpdateModelConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error(err, "Failed to decode request body")
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}
	log = log.WithValues("provider", req.Provider.Type, "model", req.Model)
	log.Info("Received request to update model config")

	log.V(1).Info("Getting existing model config")
	modelConfig := &v1alpha1.ModelConfig{}
	if err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      configName,
		Namespace: common.GetResourceNamespace(),
	}, modelConfig); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("Model config not found")
			w.RespondWithError(errors.NewNotFoundError("Model config not found", nil))
			return
		}
		log.Error(err, "Failed to get model config")
		w.RespondWithError(errors.NewInternalServerError("Failed to get model config", err))
		return
	}

	modelConfig.Spec = v1alpha1.ModelConfigSpec{
		Model:       req.Model,
		Provider:    v1alpha1.ModelProvider(req.Provider.Type),
		OpenAI:      nil,
		Anthropic:   nil,
		AzureOpenAI: nil,
		Ollama:      nil,
	}

	// --- Update Secret if API Key is provided (and not Ollama) ---
	shouldUpdateSecret := req.APIKey != nil && *req.APIKey != "" && modelConfig.Spec.Provider != v1alpha1.Ollama
	if shouldUpdateSecret {
		secretName := configName
		secretKey := fmt.Sprintf("%s_API_KEY", strings.ToUpper(req.Provider.Type))
		log.V(1).Info("Updating API key secret", "secretName", secretName, "secretKey", secretKey)
		existingSecret := &corev1.Secret{}
		err = h.KubeClient.Get(r.Context(), types.NamespacedName{Name: secretName, Namespace: common.GetResourceNamespace()}, existingSecret)
		if err != nil && !k8serrors.IsNotFound(err) {
			log.Error(err, "Failed to get existing secret for update")
			w.RespondWithError(errors.NewInternalServerError("Failed to get API key secret", err))
			return
		}

		if k8serrors.IsNotFound(err) {
			// Secret doesn't exist, create it (edge case, should normally exist)
			log.Info("Secret not found for update, creating new one", "secretName", secretName)
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: common.GetResourceNamespace()},
				StringData: map[string]string{secretKey: *req.APIKey},
			}
			if err := h.KubeClient.Create(r.Context(), secret); err != nil {
				log.Error(err, "Failed to create new API key secret during update")
				w.RespondWithError(errors.NewInternalServerError("Failed to create API key secret", err))
				return
			}
		} else {
			// Secret exists, update it
			if existingSecret.StringData == nil {
				existingSecret.StringData = make(map[string]string)
			}
			existingSecret.StringData[secretKey] = *req.APIKey
			if err := h.KubeClient.Update(r.Context(), existingSecret); err != nil {
				log.Error(err, "Failed to update API key secret")
				w.RespondWithError(errors.NewInternalServerError("Failed to update API key secret", err))
				return
			}
		}
		log.V(1).Info("Successfully updated API key secret")
		modelConfig.Spec.APIKeySecretName = secretName
		modelConfig.Spec.APIKeySecretKey = secretKey
	}

	var providerConfigErr error
	switch modelConfig.Spec.Provider {
	case v1alpha1.OpenAI:
		if req.OpenAIParams != nil {
			modelConfig.Spec.OpenAI = req.OpenAIParams
			log.V(1).Info("Assigned updated OpenAI params to spec")
		} else {
			log.V(1).Info("No OpenAI params provided in update.")
		}
	case v1alpha1.Anthropic:
		if req.AnthropicParams != nil {
			modelConfig.Spec.Anthropic = req.AnthropicParams
			log.V(1).Info("Assigned updated Anthropic params to spec")
		} else {
			log.V(1).Info("No Anthropic params provided in update.")
		}
	case v1alpha1.AzureOpenAI:
		if req.AzureParams == nil {
			// Allow clearing Azure params if provider changes AWAY from Azure,
			// but require params if provider IS Azure.
			providerConfigErr = fmt.Errorf("azureOpenAI parameters are required when provider is AzureOpenAI")
		} else {
			// Basic validation for required Azure fields
			if req.AzureParams.Endpoint == "" || req.AzureParams.APIVersion == "" {
				providerConfigErr = fmt.Errorf("missing required AzureOpenAI parameters: azureEndpoint, apiVersion")
			} else {
				modelConfig.Spec.AzureOpenAI = req.AzureParams
				log.V(1).Info("Assigned updated AzureOpenAI params to spec")
			}
		}
	case v1alpha1.Ollama:
		if req.OllamaParams != nil {
			modelConfig.Spec.Ollama = req.OllamaParams
			log.V(1).Info("Assigned updated Ollama params to spec")
		} else {
			log.V(1).Info("No Ollama params provided in update.")
		}
	default:
		providerConfigErr = fmt.Errorf("unsupported provider type specified: %s", req.Provider.Type)
	}

	if providerConfigErr != nil {
		log.Error(providerConfigErr, "Failed to assign provider config during update")
		w.RespondWithError(errors.NewBadRequestError(providerConfigErr.Error(), providerConfigErr))
		return
	}

	if err := h.KubeClient.Update(r.Context(), modelConfig); err != nil {
		log.Error(err, "Failed to update ModelConfig resource")
		w.RespondWithError(errors.NewInternalServerError("Failed to update model config", err))
		return
	}

	log.Info("Successfully updated model config", "name", configName)
	updatedParams := make(map[string]interface{})
	if modelConfig.Spec.OpenAI != nil {
		flattenStructToMap(modelConfig.Spec.OpenAI, updatedParams)
	} else if modelConfig.Spec.Anthropic != nil {
		flattenStructToMap(modelConfig.Spec.Anthropic, updatedParams)
	} else if modelConfig.Spec.AzureOpenAI != nil {
		flattenStructToMap(modelConfig.Spec.AzureOpenAI, updatedParams)
	} else if modelConfig.Spec.Ollama != nil {
		flattenStructToMap(modelConfig.Spec.Ollama, updatedParams)
	}

	responseItem := ModelConfigResponse{
		Name:             modelConfig.Name,
		Namespace:        modelConfig.Namespace,
		ProviderName:     string(modelConfig.Spec.Provider),
		Model:            modelConfig.Spec.Model,
		APIKeySecretName: modelConfig.Spec.APIKeySecretName,
		APIKeySecretKey:  modelConfig.Spec.APIKeySecretKey,
		ModelParams:      updatedParams,
	}
	RespondWithJSON(w, http.StatusOK, responseItem)
}

func (h *ModelConfigHandler) HandleDeleteModelConfig(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("modelconfig-handler").WithValues("operation", "delete")

	configName, err := GetPathParam(r, "configName")
	if err != nil {
		log.Error(err, "Failed to get config name from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get config name from path", err))
		return
	}
	log = log.WithValues("configName", configName)

	log.Info("Received request to delete model config")

	log.V(1).Info("Checking if model config exists")
	existingConfig := &v1alpha1.ModelConfig{}
	err = h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      configName,
		Namespace: common.GetResourceNamespace(),
	}, existingConfig)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("Model config not found")
			w.RespondWithError(errors.NewNotFoundError("Model config not found", nil))
			return
		}
		log.Error(err, "Failed to get model config")
		w.RespondWithError(errors.NewInternalServerError("Failed to get model config", err))
		return
	}

	log.V(1).Info("Deleting ModelConfig resource")
	if err := h.KubeClient.Delete(r.Context(), existingConfig); err != nil {
		log.Error(err, "Failed to delete ModelConfig resource")
		w.RespondWithError(errors.NewInternalServerError("Failed to delete model config", err))
		return
	}

	log.Info("Successfully deleted model config", "name", configName)
	RespondWithJSON(w, http.StatusOK, nil)
}
