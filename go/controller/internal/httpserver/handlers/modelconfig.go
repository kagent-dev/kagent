package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
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
		targetMap[key] = fieldValue.Interface()
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
	Name        string            `json:"name"`
	Provider    Provider          `json:"provider"`
	Model       string            `json:"model"`
	APIKey      string            `json:"apiKey"`
	ModelParams map[string]string `json:"modelParams"`
}

type Provider struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// getProviderConfig returns the appropriate provider config struct based on the provider type
func getProviderConfig(providerType string) (interface{}, error) {
	switch strings.ToLower(providerType) {
	case "openai":
		return &v1alpha1.OpenAIConfig{}, nil
	case "anthropic":
		return &v1alpha1.AnthropicConfig{}, nil
	case "azureopenai":
		return &v1alpha1.AzureOpenAIConfig{}, nil
	case "ollama":
		return &v1alpha1.OllamaConfig{}, nil
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", providerType)
	}
}

// setProviderConfig sets the appropriate provider config in the ModelConfigSpec
func setProviderConfig(spec *v1alpha1.ModelConfigSpec, providerType string, config interface{}) error {
	switch strings.ToLower(providerType) {
	case "openai":
		spec.OpenAI = config.(*v1alpha1.OpenAIConfig)
	case "anthropic":
		spec.Anthropic = config.(*v1alpha1.AnthropicConfig)
	case "azureopenai":
		spec.AzureOpenAI = config.(*v1alpha1.AzureOpenAIConfig)
	case "ollama":
		spec.Ollama = config.(*v1alpha1.OllamaConfig)
	default:
		return fmt.Errorf("unsupported provider type: %s", providerType)
	}
	return nil
}

// convertValue converts a string value to the appropriate type based on the field's type
func convertValue(value string, fieldType reflect.Type) (interface{}, error) {
	switch fieldType.Kind() {
	case reflect.String:
		return value, nil
	case reflect.Int:
		return strconv.Atoi(value)
	case reflect.Ptr:
		if fieldType.Elem().Kind() == reflect.Int {
			if value == "" {
				return nil, nil
			}
			v, err := strconv.Atoi(value)
			if err != nil {
				return nil, err
			}
			return &v, nil
		}
	case reflect.Map:
		if fieldType.Key().Kind() == reflect.String && fieldType.Elem().Kind() == reflect.String {
			return value, nil
		}
	}
	return nil, fmt.Errorf("unsupported field type: %v", fieldType)
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
	isOllama := strings.EqualFold(req.Provider.Type, string(v1alpha1.Ollama))
	apiKey := req.APIKey

	// Validate API key presence *unless* it's Ollama
	if !isOllama && apiKey == "" {
		log.Error(nil, "API key is required for non-Ollama providers")
		w.RespondWithError(errors.NewBadRequestError("API key is required for this provider", nil))
		return
	}

	// For Ollama, use a placeholder value if apiKey is empty, as secret data cannot be truly empty
	if isOllama && apiKey == "" {
		apiKey = "ollama-no-key"
		log.V(1).Info("Ollama provider selected, using placeholder for secret data")
	}

	secretName := req.Name
	secretKey := fmt.Sprintf("%s_API_KEY", strings.ToUpper(req.Provider.Type))
	log.V(1).Info("Creating API key secret", "secretName", secretName, "secretKey", secretKey)
	secret := &corev1.Secret{
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

	log.V(1).Info("Validating provider config and model parameters")
	providerConfig, err := getProviderConfig(req.Provider.Type)
	if err != nil {
		log.Error(err, "Invalid provider type")
		w.RespondWithError(errors.NewBadRequestError("Invalid provider type", err))
		return
	}

	// Set model params in provider config
	providerValue := reflect.ValueOf(providerConfig).Elem()
	providerType := providerValue.Type()

	for key, value := range req.ModelParams {
		field, found := providerType.FieldByNameFunc(func(fieldName string) bool {
			field, _ := providerType.FieldByName(fieldName)
			jsonTag := field.Tag.Get("json")
			jsonName := strings.Split(jsonTag, ",")[0]
			return strings.EqualFold(jsonName, key)
		})

		if !found {
			log.Error(nil, "Invalid model parameter provided", "parameter", key)
			w.RespondWithError(errors.NewBadRequestError("Invalid model parameter provided", nil))
			return
		}

		// Convert and set the value
		convertedValue, err := convertValue(value, field.Type)
		if err != nil {
			errMsg := fmt.Sprintf("Invalid value for parameter %s: %v", key, err)
			log.Error(err, "Failed to convert model parameter value", "parameter", key, "value", value)
			w.RespondWithError(errors.NewBadRequestError(errMsg, nil))
			return
		}

		fieldValue := providerValue.FieldByName(field.Name)
		if fieldValue.CanSet() {
			fieldValue.Set(reflect.ValueOf(convertedValue))
			log.V(1).Info("Set model parameter", "parameter", key, "value", value)
		} else {
			log.V(1).Info("Cannot set model parameter", "parameter", key)
		}
	}
	log.V(1).Info("Successfully validated and set model parameters")

	log.V(1).Info("Creating ModelConfig resource")
	modelConfig := &v1alpha1.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: common.GetResourceNamespace(),
		},
		Spec: v1alpha1.ModelConfigSpec{
			Model:            req.Model,
			Provider:         v1alpha1.ModelProvider(req.Provider.Name),
			APIKeySecretName: secretName,
			APIKeySecretKey:  secretKey,
		},
	}

	if err := setProviderConfig(&modelConfig.Spec, req.Provider.Type, providerConfig); err != nil {
		log.Error(err, "Failed to set provider config in spec")
		w.RespondWithError(errors.NewInternalServerError("Failed to set provider config", err))
		return
	}

	if err := h.KubeClient.Create(r.Context(), modelConfig); err != nil {
		log.Error(err, "Failed to create ModelConfig resource")
		// If we fail to create the ModelConfig, we should clean up the secret
		log.V(1).Info("Attempting to clean up secret after ModelConfig creation failure")
		if cleanupErr := h.KubeClient.Delete(r.Context(), secret); cleanupErr != nil {
			log.Error(cleanupErr, "Failed to cleanup secret after ModelConfig creation failure")
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
	Provider    Provider          `json:"provider"`
	Model       string            `json:"model"`
	APIKey      *string           `json:"apiKey,omitempty"`
	ModelParams map[string]string `json:"modelParams"`
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

	// Get and validate provider config based on new request data
	log.V(1).Info("Validating provider config and model parameters for update")
	providerConfig, err := getProviderConfig(req.Provider.Type)
	if err != nil {
		log.Error(err, "Invalid provider type")
		w.RespondWithError(errors.NewBadRequestError("Invalid provider type", err))
		return
	}

	// Set model params in the new provider config struct
	providerValue := reflect.ValueOf(providerConfig).Elem()
	providerType := providerValue.Type()
	for key, value := range req.ModelParams {
		field, found := providerType.FieldByNameFunc(func(fieldName string) bool {
			field, _ := providerType.FieldByName(fieldName)
			jsonTag := field.Tag.Get("json")
			jsonName := strings.Split(jsonTag, ",")[0]
			return strings.EqualFold(jsonName, key)
		})
		if !found {
			log.Error(nil, "Invalid model parameter provided during update", "parameter", key)
			w.RespondWithError(errors.NewBadRequestError(fmt.Sprintf("Invalid model parameter: %s", key), nil))
			return
		}
		convertedValue, err := convertValue(value, field.Type)
		if err != nil {
			errMsg := fmt.Sprintf("Invalid value for parameter %s: %v", key, err)
			log.Error(err, "Failed to convert model parameter value during update", "parameter", key, "value", value)
			w.RespondWithError(errors.NewBadRequestError(errMsg, nil))
			return
		}
		fieldValue := providerValue.FieldByName(field.Name)
		if fieldValue.CanSet() {
			fieldValue.Set(reflect.ValueOf(convertedValue))
		} else {
			log.V(1).Info("Cannot set model parameter during update", "parameter", key)
		}
	}
	log.V(1).Info("Successfully validated and populated new provider config for update")

	// --- Update Secret if API Key is provided (and not Ollama) ---
	secretName := configName
	secretKey := fmt.Sprintf("%s_API_KEY", strings.ToUpper(req.Provider.Type))
	isOllama := strings.EqualFold(req.Provider.Type, string(v1alpha1.Ollama))
	shouldUpdateSecret := req.APIKey != nil && *req.APIKey != ""

	if !isOllama && shouldUpdateSecret {
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
	} else if isOllama {
		log.V(1).Info("Skipping secret update for Ollama provider")
	} else {
		log.V(1).Info("API key not provided, secret will not be updated")
	}

	log.V(1).Info("Updating ModelConfig spec")
	modelConfig.Spec.OpenAI = nil
	modelConfig.Spec.Anthropic = nil
	modelConfig.Spec.AzureOpenAI = nil
	modelConfig.Spec.Ollama = nil

	modelConfig.Spec.Model = req.Model
	modelConfig.Spec.Provider = v1alpha1.ModelProvider(req.Provider.Name)
	modelConfig.Spec.APIKeySecretKey = secretKey

	if err := setProviderConfig(&modelConfig.Spec, req.Provider.Type, providerConfig); err != nil {
		log.Error(err, "Failed to set provider config in spec during update")
		w.RespondWithError(errors.NewInternalServerError("Failed to set provider config", err))
		return
	}

	if err := h.KubeClient.Update(r.Context(), modelConfig); err != nil {
		log.Error(err, "Failed to update ModelConfig resource")
		w.RespondWithError(errors.NewInternalServerError("Failed to update model config", err))
		return
	}

	log.Info("Successfully updated model config", "name", configName)
	updatedParams := make(map[string]interface{})
	flattenStructToMap(providerConfig, updatedParams)
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
