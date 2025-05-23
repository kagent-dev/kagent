package handlers

import (
	"net/http"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
	"k8s.io/apimachinery/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// getErrorFromConditions extracts error information from conditions
func getErrorFromConditions(conditions []metav1.Condition) *string {
	for _, condition := range conditions {
		if condition.Status == metav1.ConditionFalse {
			return &condition.Message
		}
	}
	return nil
}

// HandleListToolServers handles GET /api/toolservers requests
func (h *ToolServersHandler) HandleListToolServers(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "list")

	log.V(1).Info("Listing tool servers from Kubernetes")
	toolServerList := &v1alpha1.ToolServerList{}
	if err := h.KubeClient.List(r.Context(), toolServerList); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list tool servers from Kubernetes", err))
		return
	}

	toolServerWithTools := make([]map[string]interface{}, 0)
	for _, toolServer := range toolServerList.Items {
		log.V(1).Info("Processing tool server", "toolServerName", toolServer.Name)

		errorMsg := getErrorFromConditions(toolServer.Status.Conditions)
		if errorMsg != nil {
			log.Info("Tool server has error condition", "toolServerName", toolServer.Name, "error", *errorMsg)
		}

		discoveredTools := toolServer.Status.DiscoveredTools
		if discoveredTools == nil {
			discoveredTools = []*v1alpha1.MCPTool{}
		}
		toolServerWithTools = append(toolServerWithTools, map[string]interface{}{ 
			"name":            toolServer.Name,
			"config":          toolServer.Spec.Config,
			"discoveredTools": discoveredTools,
			"status": map[string]interface{}{
				"conditions": toolServer.Status.Conditions,
				"error":     errorMsg,
			},
		})
	}

	RespondWithJSON(w, http.StatusOK, toolServerWithTools)
}

// HandleCreateToolServer handles POST /api/toolservers requests
func (h *ToolServersHandler) HandleCreateToolServer(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "create")

	var toolServerRequest *v1alpha1.ToolServer
	if err := DecodeJSONBody(r, &toolServerRequest); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	log = log.WithValues("toolServerName", toolServerRequest.Name)
	toolServerRequest.Namespace = common.GetResourceNamespace()

	if err := h.KubeClient.Create(r.Context(), toolServerRequest); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create tool server in Kubernetes", err))
		return
	}

	log.Info("Successfully created tool server")
	RespondWithJSON(w, http.StatusCreated, toolServerRequest)
}

// HandleDeleteToolServer handles DELETE /api/toolservers/{toolServerName} requests
func (h *ToolServersHandler) HandleDeleteToolServer(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "delete")

	toolServerName, err := GetPathParam(r, "toolServerName")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get tool server name from path", err))
		return
	}
	log = log.WithValues("toolServerName", toolServerName)

	toolServer := &v1alpha1.ToolServer{}
	if err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      toolServerName,
		Namespace: common.GetResourceNamespace(),
	}, toolServer); err != nil {
		w.RespondWithError(errors.NewNotFoundError("Tool server not found in Kubernetes", err))
		return
	}

	log.V(1).Info("Deleting tool server from Kubernetes")
	if err := h.KubeClient.Delete(r.Context(), toolServer); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to delete tool server from Kubernetes", err))
		return
	}

	log.Info("Successfully deleted tool server from Kubernetes")
	w.WriteHeader(http.StatusNoContent)
}
