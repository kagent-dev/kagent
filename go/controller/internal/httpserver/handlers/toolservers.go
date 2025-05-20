package handlers

import (
	"net/http"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
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

	toolServerList := &v1alpha1.ToolServerList{}
	if err := h.KubeClient.List(r.Context(), toolServerList); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list ToolServers from Kubernetes", err))
		return
	}

	toolServerWithTools := make([]map[string]interface{}, 0)
	for _, toolServer := range toolServerList.Items {
		log.V(1).Info("Processing ToolServer",
			"namespace", toolServer.Namespace,
			"toolServerName", toolServer.Name,
		)

		toolServerWithTools = append(toolServerWithTools, map[string]interface{}{
			"name":            toolServer.Name,
			"namespace":       toolServer.Namespace,
			"config":          toolServer.Spec.Config,
			"discoveredTools": toolServer.Status.DiscoveredTools,
		})
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
		log.Error(err, "Failed to decode request body")
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	if toolServerRequest.Namespace == "" {
		toolServerRequest.Namespace = common.GetResourceNamespace()
		log.V(1).Info("Namespace not provided in request. Creating in", toolServerRequest.Namespace, "namespace")
	}

	log.Info("Received request to create ToolServer")

	if toolServerRequest.Namespace == "" {
		toolServerRequest.Namespace = common.GetResourceNamespace()
		log.V(1).Info("Namespace not provided in request. Creating in", toolServerRequest.Namespace)
	}

	log = log.WithValues(
		"namespace", toolServerRequest.Namespace,
		"toolServerName", toolServerRequest.Name,
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
		w.RespondWithError(errors.NewBadRequestError("Failed to get ToolServer name from path", err))
		return
	}

	log = log.WithValues(
		"namespace", namespace,
		"toolServerName", toolServerName,
	)

	log.V(1).Info("Checking if ToolServer exists")
	toolServer := &v1alpha1.ToolServer{}
	if err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      toolServerName,
		Namespace: namespace,
	}, toolServer); err != nil {
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
