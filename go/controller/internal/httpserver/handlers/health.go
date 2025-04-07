package handlers

import (
	"net/http"

	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// HealthHandler handles health check requests
type HealthHandler struct{}

// NewHealthHandler creates a new HealthHandler
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// HandleHealth handles GET /health requests
func (h *HealthHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("health-handler")
	log.V(1).Info("Handling health check request")

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
