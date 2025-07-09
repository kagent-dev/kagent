package handlers

import (
	"net/http"

	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
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

	models, err := h.AutogenClient.ListSupportedModels(r.Context())
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list supported models", err))
		return
	}

	data := api.NewResponse(models, "Successfully listed supported models", false)
	RespondWithJSON(w, http.StatusOK, data)
}
