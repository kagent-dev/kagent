package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	agent_translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	kmcp "github.com/kagent-dev/kmcp/api/v1alpha1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const mcpAppHTMLMimeType = "text/html;profile=mcp-app"

// mcpUIExtensionName is the MCP Apps extension identifier negotiated via
// capabilities.extensions during initialize. Advertising it lets conformant
// servers that gate UI tools on client support expose them to kagent.
const mcpUIExtensionName = "io.modelcontextprotocol/ui"

type MCPAppsHandler struct {
	*Base
}

type MCPAppToolResponse struct {
	Name          string         `json:"name"`
	Description   string         `json:"description,omitempty"`
	InputSchema   any            `json:"inputSchema,omitempty"`
	UIResourceURI string         `json:"uiResourceUri,omitempty"`
	Meta          map[string]any `json:"_meta,omitempty"`
}

type mcpAppToolCallRequest struct {
	Arguments any `json:"arguments,omitempty"`
}

func NewMCPAppsHandler(base *Base) *MCPAppsHandler {
	return &MCPAppsHandler{Base: base}
}

func (h *MCPAppsHandler) HandleListTools(w ErrorResponseWriter, r *http.Request) {
	namespace, name, ok := h.remoteMCPServerRef(w, r)
	if !ok {
		return
	}

	session, cancel, err := h.connect(r.Context(), namespace, name)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to connect to MCP server", err))
		return
	}
	defer cancel()
	defer session.Close()

	result, err := session.ListTools(r.Context(), &mcp.ListToolsParams{})
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list MCP tools", err))
		return
	}

	tools := make([]MCPAppToolResponse, 0, len(result.Tools))
	for _, tool := range result.Tools {
		if tool == nil {
			continue
		}
		uiResourceURI, _ := extractUIResourceURI(tool.Meta)
		tools = append(tools, MCPAppToolResponse{
			Name:          tool.Name,
			Description:   tool.Description,
			InputSchema:   tool.InputSchema,
			UIResourceURI: uiResourceURI,
			Meta:          tool.Meta,
		})
	}

	RespondWithJSON(w, http.StatusOK, api.NewResponse(tools, "Successfully listed MCP app tools", false))
}

func (h *MCPAppsHandler) HandleCallTool(w ErrorResponseWriter, r *http.Request) {
	namespace, name, ok := h.remoteMCPServerRef(w, r)
	if !ok {
		return
	}
	toolName, err := GetPathParam(r, "toolName")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get tool name from path", err))
		return
	}

	var req mcpAppToolCallRequest
	if r.Body != nil {
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			w.RespondWithError(errors.NewBadRequestError("Failed to read request body", readErr))
			return
		}
		if len(strings.TrimSpace(string(body))) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
				return
			}
		}
	}

	session, cancel, err := h.connect(r.Context(), namespace, name)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to connect to MCP server", err))
		return
	}
	defer cancel()
	defer session.Close()

	// This endpoint only serves app-originated tools/call requests. Per the MCP
	// Apps spec the host MUST reject app calls to tools whose visibility does not
	// include "app" (e.g. model-only tools), so enforce it server-side rather
	// than trusting the client.
	allowed, found, err := toolAllowsAppCall(r.Context(), session, toolName)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to verify MCP tool visibility", err))
		return
	}
	if !found {
		w.RespondWithError(errors.NewNotFoundError(fmt.Sprintf("MCP tool %q not found", toolName), nil))
		return
	}
	if !allowed {
		w.RespondWithError(errors.NewForbiddenError(fmt.Sprintf("MCP tool %q is not callable by apps (visibility does not include \"app\")", toolName), nil))
		return
	}

	result, err := session.CallTool(r.Context(), &mcp.CallToolParams{
		Name:      toolName,
		Arguments: req.Arguments,
	})
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to call MCP tool", err))
		return
	}

	RespondWithJSON(w, http.StatusOK, api.NewResponse(result, "Successfully called MCP tool", false))
}

func (h *MCPAppsHandler) HandleReadResource(w ErrorResponseWriter, r *http.Request) {
	namespace, name, ok := h.remoteMCPServerRef(w, r)
	if !ok {
		return
	}
	uri := r.URL.Query().Get("uri")
	if uri == "" {
		w.RespondWithError(errors.NewBadRequestError("Missing required uri query parameter", nil))
		return
	}
	if !strings.HasPrefix(uri, "ui://") {
		w.RespondWithError(errors.NewBadRequestError("MCP Apps resources must use ui:// URIs", nil))
		return
	}

	session, cancel, err := h.connect(r.Context(), namespace, name)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to connect to MCP server", err))
		return
	}
	defer cancel()
	defer session.Close()

	result, err := session.ReadResource(r.Context(), &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to read MCP resource", err))
		return
	}
	if err := validateMCPAppResource(result); err != nil {
		w.RespondWithError(errors.NewValidationError("Invalid MCP Apps resource", err))
		return
	}

	RespondWithJSON(w, http.StatusOK, api.NewResponse(result, "Successfully read MCP app resource", false))
}

func (h *MCPAppsHandler) remoteMCPServerRef(w ErrorResponseWriter, r *http.Request) (string, string, bool) {
	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return "", "", false
	}
	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return "", "", false
	}
	if err := Check(h.Authorizer, r, auth.Resource{Type: "ToolServer", Name: types.NamespacedName{Namespace: namespace, Name: name}.String()}); err != nil {
		w.RespondWithError(err)
		return "", "", false
	}
	return namespace, name, true
}

// resolveRemoteMCPServer locates the MCP endpoint for the given ref, supporting
// both RemoteMCPServer (external URL) and the kmcp MCPServer CRD (an in-cluster
// Deployment+Service). An MCPServer is converted to the same RemoteMCPServer
// shape the controller uses for tool discovery, so both kinds share one connect
// path.
func (h *MCPAppsHandler) resolveRemoteMCPServer(ctx context.Context, namespace, name string) (*v1alpha2.RemoteMCPServer, error) {
	key := client.ObjectKey{Namespace: namespace, Name: name}

	server := &v1alpha2.RemoteMCPServer{}
	err := h.KubeClient.Get(ctx, key, server)
	if err == nil {
		return server, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get RemoteMCPServer %s/%s: %w", namespace, name, err)
	}

	mcpServer := &kmcp.MCPServer{}
	if err := h.KubeClient.Get(ctx, key, mcpServer); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("no RemoteMCPServer or MCPServer %s/%s found", namespace, name)
		}
		return nil, fmt.Errorf("failed to get MCPServer %s/%s: %w", namespace, name, err)
	}
	server, err = agent_translator.ConvertMCPServerToRemoteMCPServer(mcpServer)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve MCPServer %s/%s endpoint: %w", namespace, name, err)
	}
	return server, nil
}

func (h *MCPAppsHandler) connect(ctx context.Context, namespace, name string) (*mcp.ClientSession, context.CancelFunc, error) {
	log := ctrllog.FromContext(ctx).WithName("mcp-apps-handler").WithValues("namespace", namespace, "name", name)

	server, err := h.resolveRemoteMCPServer(ctx, namespace, name)
	if err != nil {
		return nil, nil, err
	}

	timeout := 30 * time.Second
	if server.Spec.Timeout != nil && server.Spec.Timeout.Duration > 0 {
		timeout = server.Spec.Timeout.Duration
	}
	connectCtx, cancel := context.WithTimeout(ctx, timeout)

	headers, err := server.ResolveHeaders(connectCtx, h.KubeClient)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("failed to resolve RemoteMCPServer headers: %w", err)
	}

	httpClient := newMCPAppsHTTPClient(headers)
	var transport mcp.Transport
	switch server.Spec.Protocol {
	case v1alpha2.RemoteMCPServerProtocolSse:
		transport = &mcp.SSEClientTransport{
			Endpoint:   server.Spec.URL,
			HTTPClient: httpClient,
		}
	default:
		transport = &mcp.StreamableClientTransport{
			Endpoint:   server.Spec.URL,
			HTTPClient: httpClient,
		}
	}

	impl := &mcp.Implementation{
		Name:    "kagent-controller",
		Version: version.Version,
	}
	caps := &mcp.ClientCapabilities{}
	caps.AddExtension(mcpUIExtensionName, map[string]any{"mimeTypes": []string{mcpAppHTMLMimeType}})
	client := mcp.NewClient(impl, &mcp.ClientOptions{Capabilities: caps})
	session, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("failed to connect MCP client: %w", err)
	}

	log.V(2).Info("Connected to MCP server for MCP Apps")
	return session, cancel, nil
}

func extractUIResourceURI(meta map[string]any) (string, bool) {
	if len(meta) == 0 {
		return "", false
	}
	if ui, ok := meta["ui"].(map[string]any); ok {
		if uri, ok := ui["resourceUri"].(string); ok && uri != "" {
			return uri, true
		}
	}
	if uri, ok := meta["ui/resourceUri"].(string); ok && uri != "" {
		return uri, true
	}
	return "", false
}

// extractUIVisibility reads `_meta.ui.visibility`, which the MCP Apps spec
// allows as either a single string or a list of strings.
func extractUIVisibility(meta map[string]any) []string {
	ui, ok := meta["ui"].(map[string]any)
	if !ok {
		return nil
	}
	switch v := ui["visibility"].(type) {
	case string:
		return []string{v}
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// visibilityAllowsApp reports whether an app may call a tool. Per the MCP Apps
// spec visibility defaults to ["model","app"], so absent/empty visibility is
// app-callable; otherwise "app" must be present.
func visibilityAllowsApp(meta map[string]any) bool {
	visibility := extractUIVisibility(meta)
	if len(visibility) == 0 {
		return true
	}
	return slices.Contains(visibility, "app")
}

// toolAllowsAppCall lists the server's tools (following pagination), finds the
// named tool, and reports whether it is app-callable. found is false when no
// tool with that name exists.
func toolAllowsAppCall(ctx context.Context, session *mcp.ClientSession, toolName string) (allowed bool, found bool, err error) {
	params := &mcp.ListToolsParams{}
	for {
		result, err := session.ListTools(ctx, params)
		if err != nil {
			return false, false, err
		}
		for _, tool := range result.Tools {
			if tool != nil && tool.Name == toolName {
				return visibilityAllowsApp(tool.Meta), true, nil
			}
		}
		if result.NextCursor == "" {
			return false, false, nil
		}
		params.Cursor = result.NextCursor
	}
}

func validateMCPAppResource(result *mcp.ReadResourceResult) error {
	if result == nil || len(result.Contents) == 0 {
		return fmt.Errorf("resource read returned no contents")
	}
	for _, content := range result.Contents {
		if content == nil {
			return fmt.Errorf("resource read returned empty content")
		}
		if content.MIMEType != mcpAppHTMLMimeType {
			return fmt.Errorf("resource %s has MIME type %q, expected %q", content.URI, content.MIMEType, mcpAppHTMLMimeType)
		}
	}
	return nil
}

func newMCPAppsHTTPClient(headers map[string]string) *http.Client {
	if len(headers) == 0 {
		return http.DefaultClient
	}
	return &http.Client{
		Transport: &mcpAppsHeaderTransport{
			headers: headers,
			base:    http.DefaultTransport,
		},
	}
}

type mcpAppsHeaderTransport struct {
	headers map[string]string
	base    http.RoundTripper
}

func (t *mcpAppsHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	if t.base == nil {
		t.base = http.DefaultTransport
	}
	return t.base.RoundTrip(req)
}
