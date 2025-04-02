package handlers

import (
	"net/http"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
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
func (h *ToolServersHandler) HandleListToolServers(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "list")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Listing tool servers from Autogen")
	toolServers, err := h.AutogenClient.ListToolServers(userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list tool servers", err))
		return
	}

	log.Info("Successfully listed tool servers", "count", len(toolServers))
	RespondWithJSON(w, http.StatusOK, toolServers)
}

// HandleCreateToolServer handles POST /api/toolservers requests
func (h *ToolServersHandler) HandleCreateToolServer(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "create")

	var toolServerRequest *autogen_client.ToolServer
	if err := DecodeJSONBody(r, &toolServerRequest); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	if toolServerRequest.UserID == "" {
		w.RespondWithError(errors.NewBadRequestError("user_id is required", nil))
		return
	}
	log = log.WithValues("userID", toolServerRequest.UserID)

	log.V(1).Info("Creating tool server in Autogen")
	toolServer, err := h.AutogenClient.CreateToolServer(toolServerRequest, toolServerRequest.UserID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create tool server", err))
		return
	}

	log.Info("Successfully created tool server", "toolServerID", toolServer.Id)
	RespondWithJSON(w, http.StatusCreated, toolServer)
}

// HandleGetToolServer handles GET /api/toolservers/{toolServerID} requests
func (h *ToolServersHandler) HandleGetToolServer(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "get")

	toolServerID, err := GetIntPathParam(r, "toolServerID")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get tool server ID from path", err))
		return
	}
	log = log.WithValues("toolServerID", toolServerID)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Getting tool server from Autogen")
	toolServer, err := h.AutogenClient.GetToolServer(toolServerID, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get tool server", err))
		return
	}

	if toolServer == nil {
		w.RespondWithError(errors.NewNotFoundError("Tool server not found", nil))
		return
	}

	log.Info("Successfully retrieved tool server")
	RespondWithJSON(w, http.StatusOK, toolServer)
}

// HandleRefreshToolServer handles POST /api/toolservers/{toolServerID}/refresh requests
func (h *ToolServersHandler) HandleRefreshToolServer(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "refresh")

	toolServerID, err := GetIntPathParam(r, "toolServerID")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get tool server ID from path", err))
		return
	}
	log = log.WithValues("toolServerID", toolServerID)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Refreshing tools for server in Autogen")
	err = h.AutogenClient.RefreshTools(&toolServerID, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to refresh tools", err))
		return
	}

	log.Info("Successfully refreshed tools for server")
	w.WriteHeader(http.StatusNoContent)
}

// HandleGetServerTools handles GET /api/toolservers/{toolServerID}/tools requests
func (h *ToolServersHandler) HandleGetServerTools(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "list-tools")

	toolServerID, err := GetIntPathParam(r, "toolServerID")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get tool server ID from path", err))
		return
	}
	log = log.WithValues("toolServerID", toolServerID)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Listing tools for server from Autogen")
	tools, err := h.AutogenClient.ListToolsForServer(&toolServerID, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list tools for server", err))
		return
	}

	log.Info("Successfully listed tools for server", "count", len(tools))
	RespondWithJSON(w, http.StatusOK, tools)
}

// HandleDeleteToolServer handles DELETE /api/toolservers/{toolServerID} requests
func (h *ToolServersHandler) HandleDeleteToolServer(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "delete")

	toolServerID, err := GetIntPathParam(r, "toolServerID")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get tool server ID from path", err))
		return
	}
	log = log.WithValues("toolServerID", toolServerID)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Deleting tool server from Autogen")
	err = h.AutogenClient.DeleteToolServer(&toolServerID, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to delete tool server", err))
		return
	}

	log.Info("Successfully deleted tool server")
	w.WriteHeader(http.StatusNoContent)
}
