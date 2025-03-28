package handlers

import (
	"net/http"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
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
	modelConfigs := &v1alpha1.ModelConfigList{}
	if err := h.KubeClient.List(r.Context(), modelConfigs); err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	configs := make([]map[string]string, 0)
	for _, config := range modelConfigs.Items {
		configs = append(configs, map[string]string{
			"name":  config.Name,
			"model": config.Spec.Model,
		})
	}

	RespondWithJSON(w, http.StatusOK, configs)
}

func (h *ModelConfigHandler) HandleGetModelConfig(w http.ResponseWriter, r *http.Request) {
	configName, err := GetPathParam(r, "configName")
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	modelConfig := &v1alpha1.ModelConfig{}
	if err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      configName,
		Namespace: DefaultResourceNamespace,
	}, modelConfig); err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondWithJSON(w, http.StatusOK, modelConfig)
}
