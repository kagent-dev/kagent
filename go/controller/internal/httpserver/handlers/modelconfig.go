package handlers

import (
	"net/http"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
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
func (h *ModelConfigHandler) HandleListModelConfigs(w http.ResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("modelconfig-handler").WithValues("operation", "list")
	log.Info("Handling list model configs request")

	modelConfigs := &v1alpha1.ModelConfigList{}
	if err := h.KubeClient.List(r.Context(), modelConfigs); err != nil {
		log.Error(err, "Failed to list model configs from Kubernetes")
		RespondWithError(w, http.StatusInternalServerError, err.Error())
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

func (h *ModelConfigHandler) HandleGetModelConfig(w http.ResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("modelconfig-handler").WithValues("operation", "get")
	log.Info("Handling get model config request")

	configName, err := GetPathParam(r, "configName")
	if err != nil {
		log.Error(err, "Failed to get config name from path")
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	log = log.WithValues("configName", configName)

	log.V(1).Info("Getting model config from Kubernetes")
	modelConfig := &v1alpha1.ModelConfig{}
	if err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      configName,
		Namespace: DefaultResourceNamespace,
	}, modelConfig); err != nil {
		log.Error(err, "Failed to get model config")
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Info("Successfully retrieved model config")
	RespondWithJSON(w, http.StatusOK, modelConfig)
}
