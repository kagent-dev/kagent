package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	common "github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func modelConfigResource(c *v1alpha2.ModelConfig) api.ModelConfigResource {
	return api.ModelConfigResource{
		Ref:    common.GetObjectRef(c),
		Spec:   c.Spec,
		Status: c.Status,
	}
}

// HandleListModelConfigs handles GET /api/modelconfigs requests
func (h *ModelConfigHandler) HandleListModelConfigs(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("modelconfig-handler").WithValues("operation", "list")
	log.Info("Listing ModelConfigs")
	if err := Check(h.Authorizer, r, auth.Resource{Type: "ModelConfig"}); err != nil {
		w.RespondWithError(err)
		return
	}

	modelConfigs := &v1alpha2.ModelConfigList{}
	if err := h.KubeClient.List(r.Context(), modelConfigs); err != nil {
		log.Error(err, "Failed to list ModelConfigs from Kubernetes")
		w.RespondWithError(errors.NewInternalServerError("Failed to list ModelConfigs from Kubernetes", err))
		return
	}

	resources := make([]api.ModelConfigResource, 0, len(modelConfigs.Items))
	for i := range modelConfigs.Items {
		resources = append(resources, modelConfigResource(&modelConfigs.Items[i]))
	}

	log.Info("Successfully listed ModelConfigs", "count", len(resources))
	RespondWithJSON(w, http.StatusOK, api.NewResponse(resources, "Successfully listed ModelConfigs", false))
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

	log = log.WithValues("namespace", namespace, "name", configName)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "ModelConfig", Name: types.NamespacedName{Namespace: namespace, Name: configName}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	log.V(1).Info("Checking if ModelConfig exists")
	modelConfig := &v1alpha2.ModelConfig{}
	if err := h.KubeClient.Get(r.Context(), client.ObjectKey{Namespace: namespace, Name: configName}, modelConfig); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("ModelConfig not found")
			w.RespondWithError(errors.NewNotFoundError("ModelConfig not found", nil))
			return
		}
		log.Error(err, "Failed to get ModelConfig")
		w.RespondWithError(errors.NewInternalServerError("Failed to get ModelConfig", err))
		return
	}

	log.Info("Successfully retrieved ModelConfig")
	RespondWithJSON(w, http.StatusOK, api.NewResponse(modelConfigResource(modelConfig), "Successfully retrieved ModelConfig", false))
}

// HandleCreateModelConfig handles POST /api/modelconfigs requests
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

	log = log.WithValues("namespace", modelConfigRef.Namespace, "name", modelConfigRef.Name)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "ModelConfig", Name: modelConfigRef.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	if err := validateAPIKeySecretRef(req.Spec.APIKeySecret, req.Spec.APIKeySecretKey, req.Spec.Provider); err != nil {
		w.RespondWithError(errors.NewBadRequestError(err.Error(), err))
		return
	}

	log.V(1).Info("Checking if ModelConfig already exists")
	existingConfig := &v1alpha2.ModelConfig{}
	if err := h.KubeClient.Get(r.Context(), modelConfigRef, existingConfig); err == nil {
		log.Info("ModelConfig already exists")
		w.RespondWithError(errors.NewConflictError("ModelConfig already exists", nil))
		return
	} else if !apierrors.IsNotFound(err) {
		log.Error(err, "Failed to check if ModelConfig exists")
		w.RespondWithError(errors.NewInternalServerError("Failed to check if ModelConfig exists", err))
		return
	}

	// Inline apiKey takes precedence: auto-create a secret and set the refs on spec.
	if req.APIKey != "" && req.Spec.APIKeySecret == "" && req.Spec.Provider != v1alpha2.ModelProviderOllama {
		req.Spec.APIKeySecret = modelConfigRef.Name
		req.Spec.APIKeySecretKey = fmt.Sprintf("%s_API_KEY", strings.ToUpper(string(req.Spec.Provider)))
	}

	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      modelConfigRef.Name,
			Namespace: modelConfigRef.Namespace,
		},
		Spec: req.Spec,
	}

	if err := h.KubeClient.Create(r.Context(), modelConfig); err != nil {
		log.Error(err, "Failed to create ModelConfig resource")
		w.RespondWithError(errors.NewInternalServerError("Failed to create ModelConfig", err))
		return
	}

	log.V(1).Info("Successfully created ModelConfig resource")

	if req.APIKey != "" && req.Spec.Provider != v1alpha2.ModelProviderOllama {
		log.V(1).Info("Creating API key secret with OwnerReference", "secretName", modelConfig.Spec.APIKeySecretKey)
		if err := createSecretWithOwnerReference(
			r.Context(), h.KubeClient,
			map[string]string{modelConfig.Spec.APIKeySecretKey: req.APIKey},
			modelConfig,
		); err != nil {
			log.Error(err, "Failed to create API key secret")
		} else {
			log.V(1).Info("Successfully created API key secret with OwnerReference")
		}
	}

	log.Info("Successfully created ModelConfig", "ref", modelConfigRef)
	RespondWithJSON(w, http.StatusCreated, api.NewResponse(modelConfigResource(modelConfig), "Successfully created ModelConfig", false))
}

// HandleUpdateModelConfig handles PUT /api/modelconfigs/{namespace}/{name} requests
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

	log = log.WithValues("namespace", namespace, "name", configName)

	var req api.UpdateModelConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error(err, "Failed to decode request body")
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	if err := Check(h.Authorizer, r, auth.Resource{Type: "ModelConfig", Name: types.NamespacedName{Namespace: namespace, Name: configName}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	if err := validateAPIKeySecretRef(req.Spec.APIKeySecret, req.Spec.APIKeySecretKey, req.Spec.Provider); err != nil {
		w.RespondWithError(errors.NewBadRequestError(err.Error(), err))
		return
	}

	log.V(1).Info("Getting existing ModelConfig")
	modelConfig := &v1alpha2.ModelConfig{}
	if err := h.KubeClient.Get(r.Context(), client.ObjectKey{Namespace: namespace, Name: configName}, modelConfig); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("ModelConfig not found")
			w.RespondWithError(errors.NewNotFoundError("ModelConfig not found", nil))
			return
		}
		log.Error(err, "Failed to get ModelConfig")
		w.RespondWithError(errors.NewInternalServerError("Failed to get ModelConfig", err))
		return
	}

	// Inline apiKey: auto-set secret refs and create/update the secret.
	if req.APIKey != nil && *req.APIKey != "" && req.Spec.APIKeySecret == "" && req.Spec.Provider != v1alpha2.ModelProviderOllama {
		req.Spec.APIKeySecret = configName
		req.Spec.APIKeySecretKey = fmt.Sprintf("%s_API_KEY", strings.ToUpper(string(req.Spec.Provider)))
	}
	if req.APIKey != nil && *req.APIKey != "" && req.Spec.Provider != v1alpha2.ModelProviderOllama {
		log.V(1).Info("Updating API key secret")
		if err := createOrUpdateSecretWithOwnerReference(
			r.Context(), h.KubeClient,
			map[string]string{req.Spec.APIKeySecretKey: *req.APIKey},
			modelConfig,
		); err != nil {
			log.Error(err, "Failed to create or update API key secret")
			w.RespondWithError(errors.NewInternalServerError("Failed to update API key secret", err))
			return
		}
		log.V(1).Info("Successfully updated API key secret")
	}

	modelConfig.Spec = req.Spec
	if err := h.KubeClient.Update(r.Context(), modelConfig); err != nil {
		log.Error(err, "Failed to update ModelConfig resource")
		w.RespondWithError(errors.NewInternalServerError("Failed to update ModelConfig", err))
		return
	}

	log.Info("Successfully updated ModelConfig")
	RespondWithJSON(w, http.StatusOK, api.NewResponse(modelConfigResource(modelConfig), "Successfully updated ModelConfig", false))
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

	log = log.WithValues("namespace", namespace, "name", configName)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "ModelConfig", Name: types.NamespacedName{Namespace: namespace, Name: configName}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	log.V(1).Info("Checking if ModelConfig exists")
	existingConfig := &v1alpha2.ModelConfig{}
	if err := h.KubeClient.Get(r.Context(), client.ObjectKey{Namespace: namespace, Name: configName}, existingConfig); err != nil {
		if apierrors.IsNotFound(err) {
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

	log.Info("Successfully deleted ModelConfig")
	RespondWithJSON(w, http.StatusOK, api.NewResponse(struct{}{}, "Successfully deleted ModelConfig", false))
}

// validateAPIKeySecretRef returns an error if apiKeySecret is set without apiKeySecretKey
// for providers that require it (all except Bedrock and SAPAICore).
func validateAPIKeySecretRef(apiKeySecret, apiKeySecretKey string, provider v1alpha2.ModelProvider) error {
	if apiKeySecret != "" && apiKeySecretKey == "" &&
		provider != v1alpha2.ModelProviderBedrock &&
		provider != v1alpha2.ModelProviderSAPAICore {
		return fmt.Errorf("apiKeySecretKey is required when apiKeySecret is set")
	}
	return nil
}
