package handlers

import (
	"net/http"
	"strings"

	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// ToolServerTypesHandler handles ToolServerType-related requests
type ToolServerTypesHandler struct {
	*Base
}

// NewToolServerTypesHandler creates a new ToolServerTypesHandler
func NewToolServerTypesHandler(base *Base) *ToolServerTypesHandler {
	mcpGk := schema.GroupKind{Group: "kagent.dev", Kind: string(ToolServerTypeMCPServer)}
	if _, err := base.KubeClient.RESTMapper().RESTMapping(mcpGk); err != nil {
		ctrllog.Log.Info("Could not find CRD for tool server - API integration will be disabled", "toolServerType", mcpGk.String())
	}

	return &ToolServerTypesHandler{Base: base}
}

// ToolServerType represents the type of tool server to create
type ToolServerType string

type ToolServerTypes []ToolServerType

func (t ToolServerTypes) Join(sep string) string {
	if len(t) == 0 {
		return ""
	}

	if len(t) == 1 {
		return string(t[0])
	}

	var joined strings.Builder
	joined.WriteString(string(t[0]))
	for _, s := range t[1:] {
		joined.WriteString(sep + string(s))
	}

	return joined.String()
}

const (
	ToolServerTypeRemoteMCPServer ToolServerType = "RemoteMCPServer"
	ToolServerTypeMCPServer       ToolServerType = "MCPServer"
)

func GetSupportedToolServerTypes(cli client.Client) (ToolServerTypes, error) {
	types := ToolServerTypes{
		ToolServerTypeRemoteMCPServer,
	}

	if _, err := cli.RESTMapper().RESTMapping(schema.GroupKind{Group: "kagent.dev", Kind: string(ToolServerTypeMCPServer)}); err == nil {
		types = append(types, ToolServerTypeMCPServer)
	}

	return types, nil
}

// HandleListToolServerTypes handles GET /api/toolservertypes requests
func (h *ToolServerTypesHandler) HandleListToolServerTypes(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("toolservertypes-handler").WithValues("operation", "list")
	log.Info("Received request to list supported ToolServerTypes")
	if err := Check(h.Authorizer, r, auth.Resource{Type: "ToolServerType"}); err != nil {
		w.RespondWithError(err)
		return
	}

	toolServerTypes, err := GetSupportedToolServerTypes(h.KubeClient)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list supported ToolServerTypes", err))
		return
	}

	data := api.NewResponse(toolServerTypes, "Successfully listed supported ToolServerTypes", false)
	RespondWithJSON(w, http.StatusOK, data)
}
