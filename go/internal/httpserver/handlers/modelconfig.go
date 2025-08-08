package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// ModelConfigHandler handles ModelConfiguration requests
type ModelConfigHandler struct {
	*Base
}

// NewModelConfigHandler creates a new ModelConfigHandler
func NewModelConfigHandler(base *Base) *ModelConfigHandler {
	return &ModelConfigHandler{Base: base}
}

// HandleListModelConfigs handles GET /api/modelconfigs requests
func (h *ModelConfigHandler) HandleListModelConfigs(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("modelconfig-handler").WithValues("operation", "list")
	log.Info("Listing ModelConfigs")

	modelConfigs := &v1alpha1.ModelConfigList{}
	if err := h.KubeClient.List(r.Context(), modelConfigs); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list ModelConfigs from Kubernetes", err))
		return
	}

	configs := make([]api.ModelConfigResponse, 0)
	for _, config := range modelConfigs.Items {
		modelParams := make(map[string]interface{})

		if config.Spec.OpenAI != nil {
			FlattenStructToMap(config.Spec.OpenAI, modelParams)
		}
		if config.Spec.Anthropic != nil {
			FlattenStructToMap(config.Spec.Anthropic, modelParams)
		}
		if config.Spec.AzureOpenAI != nil {
			FlattenStructToMap(config.Spec.AzureOpenAI, modelParams)
		}
		if config.Spec.Ollama != nil {
			FlattenStructToMap(config.Spec.Ollama, modelParams)
		}

		responseItem := api.ModelConfigResponse{
			Ref:             common.GetObjectRef(&config),
			ProviderName:    string(config.Spec.Provider),
			Model:           config.Spec.Model,
			APIKeySecretRef: config.Spec.APIKeySecretRef,
			APIKeySecretKey: config.Spec.APIKeySecretKey,
			ModelParams:     modelParams,
		}
		configs = append(configs, responseItem)
	}

	log.Info("Successfully listed ModelConfigs", "count", len(configs))
	data := api.NewResponse(configs, "Successfully listed ModelConfigs", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleGetModelConfig handles GET /api/modelconfigs/{namespace}/{name} requests
func (h *ModelConfigHandler) HandleGetModelConfig(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("modelconfig-handler").WithValues("operation", "get")
	log.Info("Received request to get ModelConfig")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		log.Error(err, "Failed to get namespace from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}

	configName, err := GetPathParam(r, "name")
	if err != nil {
		log.Error(err, "Failed to get name from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}

	log = log.WithValues(
		"configNamespace", namespace,
		"configName", configName,
	)

	log.V(1).Info("Checking if ModelConfig exists")
	modelConfig := &v1alpha1.ModelConfig{}
	err = common.GetObject(
		r.Context(),
		h.KubeClient,
		modelConfig,
		configName,
		namespace,
	)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("ModelConfig not found")
			w.RespondWithError(errors.NewNotFoundError("ModelConfig not found", nil))
			return
		}
		log.Error(err, "Failed to get ModelConfig")
		w.RespondWithError(errors.NewInternalServerError("Failed to get ModelConfig", err))
		return
	}

	log.V(1).Info("Constructing response object")
	modelParams := make(map[string]interface{})
	if modelConfig.Spec.OpenAI != nil {
		FlattenStructToMap(modelConfig.Spec.OpenAI, modelParams)
	}
	if modelConfig.Spec.Anthropic != nil {
		FlattenStructToMap(modelConfig.Spec.Anthropic, modelParams)
	}
	if modelConfig.Spec.AzureOpenAI != nil {
		FlattenStructToMap(modelConfig.Spec.AzureOpenAI, modelParams)
	}
	if modelConfig.Spec.Ollama != nil {
		FlattenStructToMap(modelConfig.Spec.Ollama, modelParams)
	}

	responseItem := api.ModelConfigResponse{
		Ref:             common.GetObjectRef(modelConfig),
		ProviderName:    string(modelConfig.Spec.Provider),
		Model:           modelConfig.Spec.Model,
		APIKeySecretRef: modelConfig.Spec.APIKeySecretRef,
		APIKeySecretKey: modelConfig.Spec.APIKeySecretKey,
		ModelParams:     modelParams,
	}

	log.Info("Successfully retrieved and formatted ModelConfig")
	data := api.NewResponse(responseItem, "Successfully retrieved ModelConfig", false)
	RespondWithJSON(w, http.StatusOK, data)
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

type Provider struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// HandleCreateToolServer handles POST /api/modelconfigs requests
func (h *ModelConfigHandler) HandleCreateModelConfig(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("modelconfig-handler").WithValues("operation", "create")
	log.Info("Received request to create ModelConfig")

	var req api.CreateModelConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error(err, "Failed to decode request body")
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	modelConfigRef, err := common.ParseRefString(req.Ref, common.GetResourceNamespace())
	if err != nil {
		log.Error(err, "Failed to parse Ref")
		w.RespondWithError(errors.NewBadRequestError("Invalid Ref", err))
		return
	}
	if !strings.Contains(req.Ref, "/") {
		log.V(4).Info("Namespace not provided in request. Creating in controller installation namespace",
			"defaultNamespace", modelConfigRef.Namespace)
	}

	log = log.WithValues(
		"configNamespace", modelConfigRef.Namespace,
		"configName", modelConfigRef.Name,
		"provider", req.Provider.Type,
		"model", req.Model,
	)

	log.V(1).Info("Checking if ModelConfig already exists")
	existingConfig := &v1alpha1.ModelConfig{}
	err = common.GetObject(
		r.Context(),
		h.KubeClient,
		existingConfig,
		modelConfigRef.Name,
		modelConfigRef.Namespace,
	)
	if err == nil {
		log.Info("ModelConfig already exists")
		w.RespondWithError(errors.NewConflictError("ModelConfig already exists", nil))
		return
	} else if !k8serrors.IsNotFound(err) {
		log.Error(err, "Failed to check if ModelConfig exists")
		w.RespondWithError(errors.NewInternalServerError("Failed to check if ModelConfig exists", err))
		return
	}

	// --- ModelConfig Creation First ---
	providerTypeEnum := v1alpha1.ModelProvider(req.Provider.Type)
	modelConfigSpec := v1alpha1.ModelConfigSpec{
		Model:    req.Model,
		Provider: providerTypeEnum,
	}

	// Set secret references if needed, but don't create secret yet
	if providerTypeEnum != v1alpha1.ModelProviderOllama && req.APIKey != "" {
		secretName := modelConfigRef.Name
		secretKey := fmt.Sprintf("%s_API_KEY", strings.ToUpper(req.Provider.Type))
		modelConfigSpec.APIKeySecretRef = secretName
		modelConfigSpec.APIKeySecretKey = secretKey
	}

	modelConfig := &v1alpha1.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      modelConfigRef.Name,
			Namespace: modelConfigRef.Namespace,
		},
		Spec: modelConfigSpec,
	}

	var providerConfigErr error
	switch providerTypeEnum {
	case v1alpha1.ModelProviderOpenAI:
		if req.OpenAIParams != nil {
			modelConfig.Spec.OpenAI = req.OpenAIParams
			log.V(1).Info("Assigned OpenAI params to spec")
		} else {
			log.V(1).Info("No OpenAI params provided in create.")
		}
	case v1alpha1.ModelProviderAnthropic:
		if req.AnthropicParams != nil {
			modelConfig.Spec.Anthropic = req.AnthropicParams
			log.V(1).Info("Assigned Anthropic params to spec")
		} else {
			log.V(1).Info("No Anthropic params provided in create.")
		}
	case v1alpha1.ModelProviderAzureOpenAI:
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
	case v1alpha1.ModelProviderOllama:
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
		w.RespondWithError(errors.NewBadRequestError(providerConfigErr.Error(), providerConfigErr))
		return
	}

	if err := h.KubeClient.Create(r.Context(), modelConfig); err != nil {
		log.Error(err, "Failed to create ModelConfig resource")
		w.RespondWithError(errors.NewInternalServerError("Failed to create ModelConfig", err))
		return
	}
	log.V(1).Info("Successfully created ModelConfig")

	if providerTypeEnum != v1alpha1.ModelProviderOllama && req.APIKey != "" {
		secretName := modelConfigRef.Name
		secretNamespace := modelConfigRef.Namespace
		secretKey := fmt.Sprintf("%s_API_KEY", strings.ToUpper(req.Provider.Type))

		log.V(1).Info("Creating API key secret with OwnerReference",
			"secretName", secretName,
			"secretNamespace", secretNamespace,
			"secretKey", secretKey,
		)

		err := createSecretWithOwnerReference(
			r.Context(),
			h.KubeClient,
			map[string]string{secretKey: req.APIKey},
			modelConfig,
		)
		if err != nil {
			log.Error(err, "Failed to create API key secret")
		} else {
			log.V(1).Info("Successfully created API key secret with OwnerReference")
		}
	}

	log.Info("Successfully created ModelConfig")
	data := api.NewResponse(modelConfig, "Successfully created ModelConfig", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

// UpdateModelConfigRequest defines the structure for updating a ModelConfig.
// It's similar to Create, but APIKey is optional.

// HandleUpdateModelConfig handles POST /api/modelconfigs/{namespace}/{name} requests
func (h *ModelConfigHandler) HandleUpdateModelConfig(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("modelconfig-handler").WithValues("operation", "update")
	log.Info("Received request to update ModelConfig")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		log.Error(err, "Failed to get namespace from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}

	configName, err := GetPathParam(r, "name")
	if err != nil {
		log.Error(err, "Failed to get name from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}

	var req api.UpdateModelConfigRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error(err, "Failed to decode request body")
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	log = log.WithValues(
		"configNamespace", namespace,
		"configName", configName,
		"provider", req.Provider.Type,
		"model", req.Model,
	)

	log.V(1).Info("Getting existing ModelConfig")
	modelConfig := &v1alpha1.ModelConfig{}
	err = common.GetObject(
		r.Context(),
		h.KubeClient,
		modelConfig,
		configName,
		namespace,
	)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("ModelConfig not found")
			w.RespondWithError(errors.NewNotFoundError("ModelConfig not found", nil))
			return
		}
		log.Error(err, "Failed to get ModelConfig")
		w.RespondWithError(errors.NewInternalServerError("Failed to get ModelConfig", err))
		return
	}

	modelConfig.Spec = v1alpha1.ModelConfigSpec{
		Model:           req.Model,
		Provider:        v1alpha1.ModelProvider(req.Provider.Type),
		APIKeySecretRef: modelConfig.Spec.APIKeySecretRef,
		APIKeySecretKey: modelConfig.Spec.APIKeySecretKey,
		OpenAI:          nil,
		Anthropic:       nil,
		AzureOpenAI:     nil,
		Ollama:          nil,
	}

	// --- Update Secret if API Key is provided (and not Ollama) ---
	shouldUpdateSecret := req.APIKey != nil && *req.APIKey != "" && modelConfig.Spec.Provider != v1alpha1.ModelProviderOllama
	if shouldUpdateSecret {
		log.V(1).Info("Updating API key secret")

		err := createOrUpdateSecretWithOwnerReference(
			r.Context(),
			h.KubeClient,
			map[string]string{modelConfig.Spec.APIKeySecretKey: *req.APIKey},
			modelConfig,
		)
		if err != nil {
			log.Error(err, "Failed to create or update API key secret")
			w.RespondWithError(errors.NewInternalServerError("Failed to create or update API key secret", err))
			return
		}
		log.V(1).Info("Successfully updated API key secret")
	}

	var providerConfigErr error
	switch modelConfig.Spec.Provider {
	case v1alpha1.ModelProviderOpenAI:
		if req.OpenAIParams != nil {
			modelConfig.Spec.OpenAI = req.OpenAIParams
			log.V(1).Info("Assigned updated OpenAI params to spec")
		} else {
			log.V(1).Info("No OpenAI params provided in update.")
		}
	case v1alpha1.ModelProviderAnthropic:
		if req.AnthropicParams != nil {
			modelConfig.Spec.Anthropic = req.AnthropicParams
			log.V(1).Info("Assigned updated Anthropic params to spec")
		} else {
			log.V(1).Info("No Anthropic params provided in update.")
		}
	case v1alpha1.ModelProviderAzureOpenAI:
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
	case v1alpha1.ModelProviderOllama:
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
		w.RespondWithError(errors.NewInternalServerError("Failed to update ModelConfig", err))
		return
	}

	updatedParams := make(map[string]interface{})
	if modelConfig.Spec.OpenAI != nil {
		FlattenStructToMap(modelConfig.Spec.OpenAI, updatedParams)
	} else if modelConfig.Spec.Anthropic != nil {
		FlattenStructToMap(modelConfig.Spec.Anthropic, updatedParams)
	} else if modelConfig.Spec.AzureOpenAI != nil {
		FlattenStructToMap(modelConfig.Spec.AzureOpenAI, updatedParams)
	} else if modelConfig.Spec.Ollama != nil {
		FlattenStructToMap(modelConfig.Spec.Ollama, updatedParams)
	}

	responseItem := api.ModelConfigResponse{
		Ref:             common.GetObjectRef(modelConfig),
		ProviderName:    string(modelConfig.Spec.Provider),
		APIKeySecretRef: modelConfig.Spec.APIKeySecretRef,
		APIKeySecretKey: modelConfig.Spec.APIKeySecretKey,
		Model:           modelConfig.Spec.Model,
		ModelParams:     updatedParams,
	}

	log.V(1).Info("Successfully updated ModelConfig")
	data := api.NewResponse(responseItem, "Successfully updated ModelConfig", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleDeleteModelConfig handles DELETE /api/modelconfigs/{namespace}/{name} requests
func (h *ModelConfigHandler) HandleDeleteModelConfig(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("modelconfig-handler").WithValues("operation", "delete")
	log.Info("Received request to delete ModelConfig")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		log.Error(err, "Failed to get namespace from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}

	configName, err := GetPathParam(r, "name")
	if err != nil {
		log.Error(err, "Failed to get name from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}

	log = log.WithValues(
		"configNamespace", namespace,
		"configName", configName,
	)

	log.V(1).Info("Checking if ModelConfig exists")
	existingConfig := &v1alpha1.ModelConfig{}
	err = common.GetObject(
		r.Context(),
		h.KubeClient,
		existingConfig,
		configName,
		namespace,
	)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("ModelConfig not found")
			w.RespondWithError(errors.NewNotFoundError("ModelConfig not found", nil))
			return
		}
		log.Error(err, "Failed to get ModelConfig")
		w.RespondWithError(errors.NewInternalServerError("Failed to get ModelConfig", err))
		return
	}

	log.V(1).Info("Deleting ModelConfig resource")
	if err := h.KubeClient.Delete(r.Context(), existingConfig); err != nil {
		log.Error(err, "Failed to delete ModelConfig resource")
		w.RespondWithError(errors.NewInternalServerError("Failed to delete ModelConfig", err))
		return
	}

	log.V(1).Info("Successfully deleted ModelConfig")
	data := api.NewResponse(struct{}{}, "Successfully deleted ModelConfig", false)
	RespondWithJSON(w, http.StatusOK, data)
}
