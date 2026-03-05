package handlers

import (
	"net/http"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// PluginsHandler handles plugin-related requests
type PluginsHandler struct {
	*Base
}

// NewPluginsHandler creates a new PluginsHandler
func NewPluginsHandler(base *Base) *PluginsHandler {
	return &PluginsHandler{Base: base}
}

// PluginResponse represents a plugin in the API response
type PluginResponse struct {
	Name        string `json:"name"`
	PathPrefix  string `json:"pathPrefix"`
	DisplayName string `json:"displayName"`
	Icon        string `json:"icon"`
	Section     string `json:"section"`
}

// HandleListPlugins handles GET /api/plugins - returns all plugins with UI metadata
func (h *PluginsHandler) HandleListPlugins(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("plugins-handler").WithValues("operation", "list")
	log.Info("Received request to list plugins")

	plugins, err := h.DatabaseService.ListPlugins()
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list plugins", err))
		return
	}

	resp := make([]PluginResponse, len(plugins))
	for i, p := range plugins {
		resp[i] = PluginResponse{
			Name:        p.Name,
			PathPrefix:  p.PathPrefix,
			DisplayName: p.DisplayName,
			Icon:        p.Icon,
			Section:     p.Section,
		}
	}

	data := api.NewResponse(resp, "Successfully listed plugins", false)
	RespondWithJSON(w, http.StatusOK, data)
}
