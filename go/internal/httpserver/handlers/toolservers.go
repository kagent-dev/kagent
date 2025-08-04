package handlers

import (
	"net/http"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// ToolServersHandler handles ToolServer-related requests
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
	log.Info("Received request to list ToolServers")

	toolServers, err := h.DatabaseService.ListToolServers()
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list ToolServers from database", err))
		return
	}

	toolServerWithTools := make([]api.ToolServerResponse, len(toolServers))
	for i, toolServer := range toolServers {

		tools, err := h.DatabaseService.ListToolsForServer(toolServer.Name)
		if err != nil {
			w.RespondWithError(errors.NewInternalServerError("Failed to list tools for ToolServer from database", err))
			return
		}

		discoveredTools := make([]*v1alpha2.MCPTool, len(tools))
		for j, tool := range tools {
			discoveredTools[j] = &v1alpha2.MCPTool{
				Name:        tool.ID,
				Description: tool.Description,
			}
		}

		toolServerWithTools[i] = api.ToolServerResponse{
			Ref:             toolServer.Name,
			GroupKind:       toolServer.GroupKind,
			DiscoveredTools: discoveredTools,
		}
	}

	log.Info("Successfully listed ToolServers", "count", len(toolServerWithTools))
	data := api.NewResponse(toolServerWithTools, "Successfully listed ToolServers", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleCreateToolServer handles POST /api/toolservers requests
func (h *ToolServersHandler) HandleCreateToolServer(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "create")
	log.Info("Received request to create ToolServer")

	var toolServerRequest *v1alpha2.RemoteMCPServer
	if err := DecodeJSONBody(r, &toolServerRequest); err != nil {
		log.Error(err, "Invalid request body")
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	if toolServerRequest.Namespace == "" {
		toolServerRequest.Namespace = common.GetResourceNamespace()
	}
	toolRef, err := common.ParseRefString(toolServerRequest.Name, toolServerRequest.Namespace)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid ToolServer metadata", err))
	}
	if toolRef.Namespace == common.GetResourceNamespace() {
		log.V(4).Info("Namespace not provided in request. Creating in controller installation namespace",
			"namespace", toolRef.Namespace)
	}

	log = log.WithValues(
		"toolServerName", toolRef.Name,
		"toolServerNamespace", toolRef.Namespace,
	)

	if err := h.KubeClient.Create(r.Context(), toolServerRequest); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create ToolServer in Kubernetes", err))
		return
	}

	log.Info("Successfully created ToolServer")
	data := api.NewResponse(toolServerRequest, "Successfully created ToolServer", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

// HandleDeleteToolServer handles DELETE /api/toolservers/{namespace}/{name} requests
func (h *ToolServersHandler) HandleDeleteToolServer(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "delete")
	log.Info("Received request to delete ToolServer")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		log.Error(err, "Failed to get namespace from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}

	toolServerName, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}

	log = log.WithValues(
		"toolServerNamespace", namespace,
		"toolServerName", toolServerName,
	)

	log.V(1).Info("Checking if ToolServer exists")
	toolServer := &v1alpha2.RemoteMCPServer{}
	err = h.KubeClient.Get(
		r.Context(),
		client.ObjectKey{
			Namespace: namespace,
			Name:      toolServerName,
		},
		toolServer,
	)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("ToolServer not found")
			w.RespondWithError(errors.NewNotFoundError("ToolServer not found", nil))
			return
		}
		log.Error(err, "Failed to get ToolServer")
		w.RespondWithError(errors.NewInternalServerError("Failed to get ToolServer", err))
		return
	}

	log.V(1).Info("Deleting ToolServer from Kubernetes")
	if err := h.KubeClient.Delete(r.Context(), toolServer); err != nil {
		log.Error(err, "Failed to delete ToolServer resource")
		w.RespondWithError(errors.NewInternalServerError("Failed to delete ToolServer from Kubernetes", err))
		return
	}

	log.Info("Successfully deleted ToolServer from Kubernetes")
	data := api.NewResponse(struct{}{}, "Successfully deleted ToolServer", false)
	RespondWithJSON(w, http.StatusOK, data)
}
