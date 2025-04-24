package handlers

import (
	"net/http"

	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// ModelHandler handles model requests
type ModelHandler struct {
	*Base
}

// NewModelHandler creates a new ModelHandler
func NewModelHandler(base *Base) *ModelHandler {
	return &ModelHandler{Base: base}
}

func (h *ModelHandler) HandleListSupportedModels(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("model-handler").WithValues("operation", "list-supported-models")

	log.Info("Listing supported models")

	models, err := h.AutogenClient.ListSupportedModels()
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list supported models", err))
		return
	}

	RespondWithJSON(w, http.StatusOK, models)
}
