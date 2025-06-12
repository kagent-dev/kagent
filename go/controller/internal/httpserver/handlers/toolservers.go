package handlers

import (
	"net/http"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type ToolServerResponse struct {
	Ref             string                    `json:"ref"`
	Config          v1alpha1.ToolServerConfig `json:"config"`
	DiscoveredTools []*v1alpha1.MCPTool       `json:"discoveredTools"`
}

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

	toolServerList := &v1alpha1.ToolServerList{}
	if err := h.KubeClient.List(r.Context(), toolServerList); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list ToolServers from Kubernetes", err))
		return
	}

	toolServerWithTools := make([]ToolServerResponse, len(toolServerList.Items))
	for i, toolServer := range toolServerList.Items {
		toolServerWithTools[i] = ToolServerResponse{
			Ref:             common.ResourceRefString(toolServer.Namespace, toolServer.Name),
			Config:          toolServer.Spec.Config,
			DiscoveredTools: toolServer.Status.DiscoveredTools,
		}
	}

	log.Info("Successfully listed ToolServers", "count", len(toolServerWithTools))
	RespondWithJSON(w, http.StatusOK, toolServerWithTools)
}

// HandleCreateToolServer handles POST /api/toolservers requests
func (h *ToolServersHandler) HandleCreateToolServer(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "create")
	log.Info("Received request to create ToolServer")

	var toolServerRequest *v1alpha1.ToolServer
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
	RespondWithJSON(w, http.StatusCreated, toolServerRequest)
}

// HandleDeleteToolServer handles DELETE /api/toolservers/{namespace}/{toolServerName} requests
func (h *ToolServersHandler) HandleDeleteToolServer(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "delete")
	log.Info("Received request to delete ToolServer")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		log.Error(err, "Failed to get namespace from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}

	toolServerName, err := GetPathParam(r, "toolServerName")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get ToolServerName from path", err))
		return
	}

	log = log.WithValues(
		"toolServerNamespace", namespace,
		"toolServerName", toolServerName,
	)

	log.V(1).Info("Checking if ToolServer exists")
	toolServer := &v1alpha1.ToolServer{}
	err = common.GetObject(
		r.Context(),
		h.KubeClient,
		toolServer,
		toolServerName,
		namespace,
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
	w.WriteHeader(http.StatusNoContent)
}
