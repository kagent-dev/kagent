package handlers

import (
	"net/http"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// ModelConfigHandler handles model configuration requests
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

	modelConfigs := &v1alpha1.ModelConfigList{}
	if err := h.KubeClient.List(r.Context(), modelConfigs); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list model configs from Kubernetes", err))
		return
	}

	configs := make([]map[string]string, 0)
	for _, config := range modelConfigs.Items {
		log.V(1).Info("Processing model config", "name", config.Name, "model", config.Spec.Model)
		configs = append(configs, map[string]string{
			"name":  config.Name,
			"model": config.Spec.Model,
		})
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
		Namespace: DefaultResourceNamespace,
	}, modelConfig); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get model config", err))
		return
	}

	log.Info("Successfully retrieved model config")
	RespondWithJSON(w, http.StatusOK, modelConfig)
}
