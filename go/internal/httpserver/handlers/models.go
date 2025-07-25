package handlers

import (
	"net/http"

	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
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

	// TODO: Implement this

	w.RespondWithError(errors.NewNotImplementedError("Not implemented", nil))
	return
}
