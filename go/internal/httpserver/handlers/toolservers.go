package handlers

import (
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
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

// ToolServerType represents the type of tool server to create
type ToolServerType string

const (
	ToolServerTypeRemoteMCPServer ToolServerType = "RemoteMCPServer"
	ToolServerTypeMCPServer       ToolServerType = "MCPServer"
)

// ToolServerCreateRequest represents a request to create either a RemoteMCPServer or MCPServer
type ToolServerCreateRequest struct {
	// Type specifies which kind of tool server to create
	Type ToolServerType `json:"type"`

	// RemoteMCPServer is used when Type is "RemoteMCPServer"
	RemoteMCPServer *v1alpha2.RemoteMCPServer `json:"remoteMCPServer,omitempty"`

	// MCPServer is used when Type is "MCPServer"
	MCPServer *v1alpha1.MCPServer `json:"mcpServer,omitempty"`
}

// HandleListToolServers handles GET /api/toolservers requests
func (h *ToolServersHandler) HandleListToolServers(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservers-handler").WithValues("operation", "list")
	log.Info("Received request to list ToolServers")
	if err := Check(h.Authorizer, r, auth.Resource{Type: "ToolServer"}); err != nil {
		w.RespondWithError(err)
		return
	}

	toolServers, err := h.DatabaseService.ListToolServers()
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list ToolServers from database", err))
		return
	}

	toolServerWithTools := make([]api.ToolServerResponse, len(toolServers))
	for i, toolServer := range toolServers {

		tools, err := h.DatabaseService.ListToolsForServer(toolServer.Name, toolServer.GroupKind)
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

	var toolServerRequest ToolServerCreateRequest
	if err := DecodeJSONBody(r, &toolServerRequest); err != nil {
		log.Error(err, "Invalid request body")
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	switch toolServerRequest.Type {
	case ToolServerTypeRemoteMCPServer:
		if toolServerRequest.RemoteMCPServer == nil {
			w.RespondWithError(errors.NewBadRequestError("RemoteMCPServer data is required when type is RemoteMCPServer", nil))
			return
		}
		h.handleCreateRemoteMCPServer(w, r, toolServerRequest.RemoteMCPServer, log)
	case ToolServerTypeMCPServer:
		if toolServerRequest.MCPServer == nil {
			w.RespondWithError(errors.NewBadRequestError("MCPServer data is required when type is MCPServer", nil))
			return
		}
		h.handleCreateMCPServer(w, r, toolServerRequest.MCPServer, log)
	default:
		w.RespondWithError(errors.NewBadRequestError("Invalid tool server type. Must be either 'RemoteMCPServer' or 'MCPServer'", nil))
	}
}

// handleCreateRemoteMCPServer handles the creation of a RemoteMCPServer
func (h *ToolServersHandler) handleCreateRemoteMCPServer(w ErrorResponseWriter, r *http.Request, toolServerRequest *v1alpha2.RemoteMCPServer, log logr.Logger) {
	if toolServerRequest.Namespace == "" {
		toolServerRequest.Namespace = common.GetResourceNamespace()
	}
	toolRef, err := common.ParseRefString(toolServerRequest.Name, toolServerRequest.Namespace)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid ToolServer metadata", err))
		return
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
		w.RespondWithError(errors.NewInternalServerError("Failed to create RemoteMCPServer in Kubernetes", err))
		return
	}

	log.Info("Successfully created RemoteMCPServer")
	data := api.NewResponse(toolServerRequest, "Successfully created RemoteMCPServer", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

// handleCreateMCPServer handles the creation of an MCPServer (stdio-based)
func (h *ToolServersHandler) handleCreateMCPServer(w ErrorResponseWriter, r *http.Request, toolServerRequest *v1alpha1.MCPServer, log logr.Logger) {
	if toolServerRequest.Namespace == "" {
		toolServerRequest.Namespace = common.GetResourceNamespace()
	}
	toolRef, err := common.ParseRefString(toolServerRequest.Name, toolServerRequest.Namespace)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid ToolServer metadata", err))
		return
	}
	if toolRef.Namespace == common.GetResourceNamespace() {
		log.V(4).Info("Namespace not provided in request. Creating in controller installation namespace",
			"namespace", toolRef.Namespace)
	}

	log = log.WithValues(
		"toolServerName", toolRef.Name,
		"toolServerNamespace", toolRef.Namespace,
	)
	if err := Check(h.Authorizer, r, auth.Resource{Type: "ToolServer", Name: toolRef.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	if err := h.KubeClient.Create(r.Context(), toolServerRequest); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create MCPServer in Kubernetes", err))
		return
	}

	log.Info("Successfully created MCPServer")
	data := api.NewResponse(toolServerRequest, "Successfully created MCPServer", false)
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
	if err := Check(h.Authorizer, r, auth.Resource{Type: "ToolServer", Name: types.NamespacedName{Namespace: namespace, Name: toolServerName}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	// Find the tool server in the database to get its groupKind
	ref := fmt.Sprintf("%s/%s", namespace, toolServerName)
	toolServers, err := h.DatabaseService.ListToolServers()
	if err != nil {
		log.Error(err, "Failed to list tool servers from database")
		w.RespondWithError(errors.NewInternalServerError("Failed to list tool servers from database", err))
		return
	}

	var groupKind string
	for _, ts := range toolServers {
		if ts.Name == ref {
			groupKind = ts.GroupKind
			break
		}
	}

	if groupKind == "" {
		log.Info("ToolServer not found in database")
		w.RespondWithError(errors.NewNotFoundError("ToolServer not found", nil))
		return
	}

	log.V(1).Info("Checking if ToolServer exists", "groupKind", groupKind)

	// Delete based on the groupKind
	switch groupKind {
	case "RemoteMCPServer.kagent.dev":
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
				log.Info("RemoteMCPServer not found")
				w.RespondWithError(errors.NewNotFoundError("RemoteMCPServer not found", nil))
				return
			}
			log.Error(err, "Failed to get RemoteMCPServer")
			w.RespondWithError(errors.NewInternalServerError("Failed to get RemoteMCPServer", err))
			return
		}

		log.V(1).Info("Deleting RemoteMCPServer from Kubernetes")
		if err := h.KubeClient.Delete(r.Context(), toolServer); err != nil {
			log.Error(err, "Failed to delete RemoteMCPServer resource")
			w.RespondWithError(errors.NewInternalServerError("Failed to delete RemoteMCPServer from Kubernetes", err))
			return
		}

	case "MCPServer.kagent.dev":
		toolServer := &v1alpha1.MCPServer{}
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
				log.Info("MCPServer not found")
				w.RespondWithError(errors.NewNotFoundError("MCPServer not found", nil))
				return
			}
			log.Error(err, "Failed to get MCPServer")
			w.RespondWithError(errors.NewInternalServerError("Failed to get MCPServer", err))
			return
		}

		log.V(1).Info("Deleting MCPServer from Kubernetes")
		if err := h.KubeClient.Delete(r.Context(), toolServer); err != nil {
			log.Error(err, "Failed to delete MCPServer resource")
			w.RespondWithError(errors.NewInternalServerError("Failed to delete MCPServer from Kubernetes", err))
			return
		}

	case "Service":
		service := &corev1.Service{}
		err = h.KubeClient.Get(
			r.Context(),
			client.ObjectKey{
				Namespace: namespace,
				Name:      toolServerName,
			},
			service,
		)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				log.Info("Service not found")
				w.RespondWithError(errors.NewNotFoundError("Service not found", nil))
				return
			}
			log.Error(err, "Failed to get Service")
			w.RespondWithError(errors.NewInternalServerError("Failed to get Service", err))
			return
		}

		log.V(1).Info("Deleting Service from Kubernetes")
		if err := h.KubeClient.Delete(r.Context(), service); err != nil {
			log.Error(err, "Failed to delete Service resource")
			w.RespondWithError(errors.NewInternalServerError("Failed to delete Service from Kubernetes", err))
			return
		}

	default:
		log.Error(nil, "Unknown groupKind", "groupKind", groupKind)
		w.RespondWithError(errors.NewBadRequestError("Unknown tool server type", nil))
		return
	}

	log.Info("Successfully deleted ToolServer from Kubernetes")
	data := api.NewResponse(struct{}{}, "Successfully deleted ToolServer", false)
	RespondWithJSON(w, http.StatusOK, data)
}
