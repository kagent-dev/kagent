package handlers

import (
	"net/http"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
)

// ToolServersHandler handles tool server-related requests
type ToolServersHandler struct {
	*Base
}

// NewToolServersHandler creates a new ToolServersHandler
func NewToolServersHandler(base *Base) *ToolServersHandler {
	return &ToolServersHandler{Base: base}
}

// HandleListToolServers handles GET /api/toolservers requests
func (h *ToolServersHandler) HandleListToolServers(w http.ResponseWriter, r *http.Request) {
	userID, err := GetUserID(r)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	toolServers, err := h.AutogenClient.ListToolServers(userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondWithJSON(w, http.StatusOK, toolServers)
}

// HandleCreateToolServer handles POST /api/toolservers requests
func (h *ToolServersHandler) HandleCreateToolServer(w http.ResponseWriter, r *http.Request) {
	var toolServerRequest *autogen_client.ToolServer

	if err := DecodeJSONBody(r, &toolServerRequest); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if toolServerRequest.UserID == nil || *toolServerRequest.UserID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id is required")
		return
	}

	toolServer, err := h.AutogenClient.CreateToolServer(toolServerRequest)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondWithJSON(w, http.StatusCreated, toolServer)
}

// HandleGetToolServer handles GET /api/toolservers/{toolServerID} requests
func (h *ToolServersHandler) HandleGetToolServer(w http.ResponseWriter, r *http.Request) {
	toolServerID, err := GetIntPathParam(r, "toolServerID")
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	userID, err := GetUserID(r)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	toolServer, err := h.AutogenClient.GetToolServer(toolServerID, userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if toolServer == nil {
		RespondWithError(w, http.StatusNotFound, "Tool server not found")
		return
	}

	RespondWithJSON(w, http.StatusOK, toolServer)
}

// HandleRefreshToolServer handles POST /api/toolservers/{toolServerID}/refresh requests
func (h *ToolServersHandler) HandleRefreshToolServer(w http.ResponseWriter, r *http.Request) {
	toolServerID, err := GetIntPathParam(r, "toolServerID")
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	userID, err := GetUserID(r)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	err = h.AutogenClient.RefreshTools(&toolServerID, userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleGetServerTools handles GET /api/toolservers/{toolServerID}/tools requests
func (h *ToolServersHandler) HandleGetServerTools(w http.ResponseWriter, r *http.Request) {
	toolServerID, err := GetIntPathParam(r, "toolServerID")
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	userID, err := GetUserID(r)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	tools, err := h.AutogenClient.ListToolsForServer(&toolServerID, userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondWithJSON(w, http.StatusOK, tools)
}

func (h *ToolServersHandler) HandleDeleteToolServer(w http.ResponseWriter, r *http.Request) {
	toolServerID, err := GetIntPathParam(r, "toolServerID")
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	userID, err := GetUserID(r)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	err = h.AutogenClient.DeleteToolServer(&toolServerID, userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
