package handlers

import (
	"net/http"
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
	userID, err := GetUserID(r)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	tools, err := h.AutogenClient.ListTools(userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondWithJSON(w, http.StatusOK, tools)
}
