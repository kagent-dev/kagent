package tool

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	agenttranslator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	kmcp "github.com/kagent-dev/kmcp/api/v1alpha1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	mcpAppHTMLMimeType = "text/html;profile=mcp-app"
	mcpUIExtensionName = "io.modelcontextprotocol/ui"
)

type RuntimeMCPClient struct {
	kubeClient client.Client
}

func NewRuntimeMCPClient(kubeClient client.Client) *RuntimeMCPClient {
	return &RuntimeMCPClient{kubeClient: kubeClient}
}

func (c *RuntimeMCPClient) ListTools(ctx context.Context, ref MCPServerRef) ([]MCPAppTool, error) {
	session, cleanup, err := c.connect(ctx, ref)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to connect to MCP server", err)
	}
	defer cleanup()

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to list MCP tools", err)
	}
	tools := make([]MCPAppTool, 0, len(result.Tools))
	for _, discoveredTool := range result.Tools {
		if discoveredTool == nil {
			continue
		}
		uiResourceURI, _ := extractUIResourceURI(discoveredTool.Meta)
		tools = append(tools, MCPAppTool{
			Name:          discoveredTool.Name,
			Description:   discoveredTool.Description,
			InputSchema:   discoveredTool.InputSchema,
			UIResourceURI: uiResourceURI,
			Meta:          discoveredTool.Meta,
		})
	}
	return tools, nil
}

func (c *RuntimeMCPClient) CallTool(ctx context.Context, ref MCPServerRef, toolName string, arguments any) (*mcp.CallToolResult, error) {
	session, cleanup, err := c.connect(ctx, ref)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to connect to MCP server", err)
	}
	defer cleanup()

	allowed, found, err := toolAllowsAppCall(ctx, session, toolName)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to verify MCP tool visibility", err)
	}
	if !found {
		return nil, serviceerrors.NewNotFound(fmt.Sprintf("MCP tool %q not found", toolName), nil)
	}
	if !allowed {
		return nil, serviceerrors.NewPermissionDenied(
			fmt.Sprintf("MCP tool %q is not callable by apps (visibility does not include \"app\")", toolName),
			nil,
		)
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: arguments})
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to call MCP tool", err)
	}
	return result, nil
}

func (c *RuntimeMCPClient) ReadResource(ctx context.Context, ref MCPServerRef, uri string) (*mcp.ReadResourceResult, error) {
	session, cleanup, err := c.connect(ctx, ref)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to connect to MCP server", err)
	}
	defer cleanup()

	result, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to read MCP resource", err)
	}
	if err := validateMCPAppResource(result); err != nil {
		return nil, serviceerrors.NewInvalidArgument("Invalid MCP Apps resource", err)
	}
	return result, nil
}

func (c *RuntimeMCPClient) ResolveServer(ctx context.Context, ref MCPServerRef) (*v1alpha2.RemoteMCPServer, error) {
	key := client.ObjectKey{Namespace: ref.Ref.Namespace, Name: ref.Ref.Name}
	switch mcpServerCRDKind(ref.GroupKind) {
	case "MCPServer":
		server, found, err := c.getMCPServerEndpoint(ctx, key)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("no MCPServer %s found", ref.Ref.String())
		}
		return server, nil
	case "RemoteMCPServer":
		server, found, err := c.getRemoteMCPServer(ctx, key)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("no RemoteMCPServer %s found", ref.Ref.String())
		}
		return server, nil
	default:
		if server, found, err := c.getRemoteMCPServer(ctx, key); err != nil {
			return nil, err
		} else if found {
			return server, nil
		}
		server, found, err := c.getMCPServerEndpoint(ctx, key)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("no RemoteMCPServer or MCPServer %s found", ref.Ref.String())
		}
		return server, nil
	}
}

func (c *RuntimeMCPClient) connect(ctx context.Context, ref MCPServerRef) (*mcp.ClientSession, func(), error) {
	server, err := c.ResolveServer(ctx, ref)
	if err != nil {
		return nil, nil, err
	}
	timeout := 30 * time.Second
	if server.Spec.Timeout != nil && server.Spec.Timeout.Duration > 0 {
		timeout = server.Spec.Timeout.Duration
	}
	connectCtx, cancel := context.WithTimeout(ctx, timeout)

	headers, err := server.ResolveHeaders(connectCtx, c.kubeClient)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("failed to resolve RemoteMCPServer headers: %w", err)
	}

	httpClient := newMCPAppsHTTPClient(headers)
	var transport mcp.Transport
	switch server.Spec.Protocol {
	case v1alpha2.RemoteMCPServerProtocolSse:
		transport = &mcp.SSEClientTransport{Endpoint: server.Spec.URL, HTTPClient: httpClient}
	default:
		transport = &mcp.StreamableClientTransport{Endpoint: server.Spec.URL, HTTPClient: httpClient}
	}

	capabilities := &mcp.ClientCapabilities{}
	capabilities.AddExtension(mcpUIExtensionName, map[string]any{"mimeTypes": []string{mcpAppHTMLMimeType}})
	mcpClient := mcp.NewClient(
		&mcp.Implementation{Name: "kagent-controller", Version: version.Version},
		&mcp.ClientOptions{Capabilities: capabilities},
	)
	session, err := mcpClient.Connect(connectCtx, transport, nil)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("failed to connect MCP client: %w", err)
	}
	cleanup := func() {
		session.Close()
		cancel()
	}
	return session, cleanup, nil
}

func (c *RuntimeMCPClient) getRemoteMCPServer(ctx context.Context, key client.ObjectKey) (*v1alpha2.RemoteMCPServer, bool, error) {
	server := &v1alpha2.RemoteMCPServer{}
	if err := c.kubeClient.Get(ctx, key, server); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to get RemoteMCPServer %s: %w", key.String(), err)
	}
	return server, true, nil
}

func (c *RuntimeMCPClient) getMCPServerEndpoint(ctx context.Context, key client.ObjectKey) (*v1alpha2.RemoteMCPServer, bool, error) {
	server := &kmcp.MCPServer{}
	if err := c.kubeClient.Get(ctx, key, server); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to get MCPServer %s: %w", key.String(), err)
	}
	converted, err := agenttranslator.ConvertMCPServerToRemoteMCPServer(server)
	if err != nil {
		return nil, true, fmt.Errorf("failed to resolve MCPServer %s endpoint: %w", key.String(), err)
	}
	return converted, true, nil
}

func mcpServerCRDKind(groupKind string) string {
	kind, _, _ := strings.Cut(groupKind, ".")
	return kind
}

func extractUIResourceURI(meta map[string]any) (string, bool) {
	if ui, ok := meta["ui"].(map[string]any); ok {
		if uri, ok := ui["resourceUri"].(string); ok && uri != "" {
			return uri, true
		}
	}
	uri, ok := meta["ui/resourceUri"].(string)
	return uri, ok && uri != ""
}

func visibilityAllowsApp(meta map[string]any) bool {
	ui, ok := meta["ui"].(map[string]any)
	if !ok {
		return true
	}
	visibility := make([]string, 0)
	switch value := ui["visibility"].(type) {
	case string:
		visibility = append(visibility, value)
	case []string:
		visibility = append(visibility, value...)
	case []any:
		for _, item := range value {
			if text, ok := item.(string); ok {
				visibility = append(visibility, text)
			}
		}
	}
	return len(visibility) == 0 || slices.Contains(visibility, "app")
}

func toolAllowsAppCall(ctx context.Context, session *mcp.ClientSession, toolName string) (bool, bool, error) {
	params := &mcp.ListToolsParams{}
	for {
		result, err := session.ListTools(ctx, params)
		if err != nil {
			return false, false, err
		}
		for _, discoveredTool := range result.Tools {
			if discoveredTool != nil && discoveredTool.Name == toolName {
				return visibilityAllowsApp(discoveredTool.Meta), true, nil
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
	return &http.Client{Transport: &mcpAppsHeaderTransport{headers: headers, base: http.DefaultTransport}}
}

type mcpAppsHeaderTransport struct {
	headers map[string]string
	base    http.RoundTripper
}

func (t *mcpAppsHeaderTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	for key, value := range t.headers {
		request.Header.Set(key, value)
	}
	return t.base.RoundTrip(request)
}
