package handlers

import (
	"net/http"

	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// ToolsHandler handles tool-related requests
type ToolsHandler struct {
	*Base
}

// NewToolsHandler creates a new ToolsHandler
func NewToolsHandler(base *Base) *ToolsHandler {
	return &ToolsHandler{Base: base}
}

// HandleListTools handles GET /api/tools requests
func (h *ToolsHandler) HandleListTools(w http.ResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("tools-handler").WithValues("operation", "list")
	log.Info("Handling list tools request")

	userID, err := GetUserID(r)
	if err != nil {
		log.Error(err, "Failed to get user ID")
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Listing tools from Autogen")
	tools, err := h.AutogenClient.ListTools(userID)
	if err != nil {
		log.Error(err, "Failed to list tools")
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Info("Successfully listed tools", "count", len(tools))
	RespondWithJSON(w, http.StatusOK, tools)
}
