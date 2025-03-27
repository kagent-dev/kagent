package handlers

import (
	"github.com/kagent-dev/kagent/go/controller/internal/autogen"
	"net/http"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
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
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "list")
	log.Info("Handling list tool servers request")

	userID, err := GetUserID(r)
	if err != nil {
		log.Error(err, "Failed to get user ID")
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Listing tool servers from Autogen")
	toolServers, err := h.AutogenClient.ListToolServers(userID)
	if err != nil {
		log.Error(err, "Failed to list tool servers")
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Info("Successfully listed tool servers", "count", len(toolServers))
	RespondWithJSON(w, http.StatusOK, toolServers)
}

// HandleCreateToolServer handles POST /api/toolservers requests
func (h *ToolServersHandler) HandleCreateToolServer(w http.ResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "create")
	log.Info("Handling create tool server request")

	var toolServerRequest *autogen_client.ToolServer

	if err := DecodeJSONBody(r, &toolServerRequest); err != nil {
		log.Error(err, "Invalid request body")
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if toolServerRequest.UserID == "" {
	if toolServerRequest.UserID == nil || *toolServerRequest.UserID == "" {
		log.Error(nil, "Missing user_id in request")
		RespondWithError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	log = log.WithValues("userID", *toolServerRequest.UserID)

	toolServer, err := h.AutogenClient.CreateToolServer(toolServerRequest, autogen.GlobalUserID)
	log.V(1).Info("Creating tool server in Autogen")
	toolServer, err := h.AutogenClient.CreateToolServer(toolServerRequest)
	if err != nil {
		log.Error(err, "Failed to create tool server")
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Info("Successfully created tool server", "toolServerID", toolServer.Id)
	RespondWithJSON(w, http.StatusCreated, toolServer)
}

// HandleGetToolServer handles GET /api/toolservers/{toolServerID} requests
func (h *ToolServersHandler) HandleGetToolServer(w http.ResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "get")
	log.Info("Handling get tool server request")

	toolServerID, err := GetIntPathParam(r, "toolServerID")
	if err != nil {
		log.Error(err, "Failed to get tool server ID from path")
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	log = log.WithValues("toolServerID", toolServerID)

	userID, err := GetUserID(r)
	if err != nil {
		log.Error(err, "Failed to get user ID")
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Getting tool server from Autogen")
	toolServer, err := h.AutogenClient.GetToolServer(toolServerID, userID)
	if err != nil {
		log.Error(err, "Failed to get tool server")
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if toolServer == nil {
		log.Info("Tool server not found")
		RespondWithError(w, http.StatusNotFound, "Tool server not found")
		return
	}

	log.Info("Successfully retrieved tool server")
	RespondWithJSON(w, http.StatusOK, toolServer)
}

// HandleRefreshToolServer handles POST /api/toolservers/{toolServerID}/refresh requests
func (h *ToolServersHandler) HandleRefreshToolServer(w http.ResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "refresh")
	log.Info("Handling refresh tool server request")

	toolServerID, err := GetIntPathParam(r, "toolServerID")
	if err != nil {
		log.Error(err, "Failed to get tool server ID from path")
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	log = log.WithValues("toolServerID", toolServerID)

	userID, err := GetUserID(r)
	if err != nil {
		log.Error(err, "Failed to get user ID")
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Refreshing tools for server in Autogen")
	err = h.AutogenClient.RefreshTools(&toolServerID, userID)
	if err != nil {
		log.Error(err, "Failed to refresh tools")
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Info("Successfully refreshed tools for server")
	w.WriteHeader(http.StatusNoContent)
}

// HandleGetServerTools handles GET /api/toolservers/{toolServerID}/tools requests
func (h *ToolServersHandler) HandleGetServerTools(w http.ResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "list-tools")
	log.Info("Handling get server tools request")

	toolServerID, err := GetIntPathParam(r, "toolServerID")
	if err != nil {
		log.Error(err, "Failed to get tool server ID from path")
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	log = log.WithValues("toolServerID", toolServerID)

	userID, err := GetUserID(r)
	if err != nil {
		log.Error(err, "Failed to get user ID")
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Listing tools for server from Autogen")
	tools, err := h.AutogenClient.ListToolsForServer(&toolServerID, userID)
	if err != nil {
		log.Error(err, "Failed to list tools for server")
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Info("Successfully listed tools for server", "count", len(tools))
	RespondWithJSON(w, http.StatusOK, tools)
}

func (h *ToolServersHandler) HandleDeleteToolServer(w http.ResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "delete")
	log.Info("Handling delete tool server request")

	toolServerID, err := GetIntPathParam(r, "toolServerID")
	if err != nil {
		log.Error(err, "Failed to get tool server ID from path")
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	log = log.WithValues("toolServerID", toolServerID)

	userID, err := GetUserID(r)
	if err != nil {
		log.Error(err, "Failed to get user ID")
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Deleting tool server from Autogen")
	err = h.AutogenClient.DeleteToolServer(&toolServerID, userID)
	if err != nil {
		log.Error(err, "Failed to delete tool server")
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Info("Successfully deleted tool server")
	w.WriteHeader(http.StatusNoContent)
}
