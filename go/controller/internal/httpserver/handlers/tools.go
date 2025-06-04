package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/kagent-dev/kagent/go/autogen/api"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// ToolsHandler handles tool-related requests
type ToolsHandler struct {
	*Base
}

// convertAnyTypeMapToInterfaceMap converts a map[string]v1alpha1.AnyType to map[string]interface{}
func convertAnyTypeMapToInterfaceMap(input map[string]v1alpha1.AnyType) map[string]interface{} {
	result := make(map[string]interface{})
	for key, value := range input {
		result[key] = value.RawMessage // Assuming v1alpha1.AnyType has a RawMessage field of type json.RawMessage
	}
	return result
}

// NewToolsHandler creates a new ToolsHandler
func NewToolsHandler(base *Base) *ToolsHandler {
	return &ToolsHandler{Base: base}
}

// HandleListTools handles GET /api/tools requests
func (h *ToolsHandler) HandleListTools(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("tools-handler").WithValues("operation", "list")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Listing tools from Autogen")
	tools, err := h.AutogenClient.ListTools(userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list tools", err))
		return
	}

	log.V(1).Info("Listing ToolServers from Kubernetes")
	var allToolServers v1alpha1.ToolServerList
	if err = h.KubeClient.List(r.Context(), &allToolServers); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list tools from Kubernetes", err))
		return
	}

	discoveredTools := make([]*api.Component, 0)
	for _, toolServer := range allToolServers.Items {
		for _, t := range toolServer.Status.DiscoveredTools {
			// Set the server name in the component label
			t.Component.Label = common.GetObjectRef(&toolServer)
			discoveredTools = append(discoveredTools, &api.Component{
				Provider:      t.Component.Provider,
				Label:         t.Component.Label,
				Description:   t.Component.Description,
				Config:        convertAnyTypeMapToInterfaceMap(t.Component.Config),
				ComponentType: t.Component.ComponentType,
			})
		}
	}

	for _, tool := range tools {
		if strings.HasPrefix(tool.Component.Provider, "kagent") {
			discoveredTools = append(discoveredTools, tool.Component)
		}
	}

	log.Info("Successfully listed tools", "count", len(tools))
	RespondWithJSON(w, http.StatusOK, discoveredTools)
}

func convertMapToMCPToolConfig(data map[string]v1alpha1.AnyType) (api.MCPToolConfig, error) {
	var config api.MCPToolConfig

	// Extract server_params if it exists
	if serverParamsRaw, ok := data["server_params"]; ok {
		// First unmarshal to a map to check the type
		var serverParamsMap map[string]interface{}
		if err := json.Unmarshal(serverParamsRaw.RawMessage, &serverParamsMap); err != nil {
			return config, fmt.Errorf("failed to unmarshal server_params: %w", err)
		}

		// Now convert to SseMcpServerConfig
		var sseConfig api.SseMcpServerConfig
		if err := json.Unmarshal(serverParamsRaw.RawMessage, &sseConfig); err != nil {
			return config, fmt.Errorf("failed to unmarshal server_params as SseMcpServerConfig: %w", err)
		}

		config.ServerParams = sseConfig
	}

	// Extract tool information
	toolRaw, ok := data["tool"]
	if !ok {
		return config, fmt.Errorf("missing required field 'tool'")
	}

	var tool api.MCPTool
	if err := json.Unmarshal(toolRaw.RawMessage, &tool); err != nil {
		return config, fmt.Errorf("failed to unmarshal tool: %w", err)
	}
	config.Tool = tool

	return config, nil
}
