package handlers

import (
	"net/http"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
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
