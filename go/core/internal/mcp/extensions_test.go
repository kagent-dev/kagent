package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type headerSession string

func (s headerSession) Principal() auth.Principal { return auth.Principal{ID: string(s)} }

// headerProvider admits only requests that carry X-User-Id.
type headerProvider struct{}

func (headerProvider) Authenticate(_ context.Context, headers http.Header, _ url.Values) (auth.Session, error) {
	id := headers.Get("X-User-Id")
	if id == "" {
		return nil, errors.New("no credentials")
	}
	return headerSession(id), nil
}

func (headerProvider) UpstreamAuth(*http.Request, auth.Session, string) error { return nil }

func extendedHandler(t *testing.T, extend func(*mcp.Server) error) (*Handler, error) {
	t.Helper()
	return New(Config{Agents: testAgentService(t), Sessions: testSessionService(), Checkpoints: testCheckpointService(), Gateway: &fakeGateway{}, Extensions: extend})
}

func TestExtensionsServeToolsAndResourcesBehindTheCoreAuthenticator(t *testing.T) {
	handler, err := extendedHandler(t, func(server *mcp.Server) error {
		server.AddTool(&mcp.Tool{
			Name:        "whoami",
			InputSchema: &jsonschema.Schema{Type: "object"},
			Meta:        mcp.Meta{"ui": map[string]any{"resourceUri": "ui://ext/app.html"}},
		}, func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			session, _ := auth.AuthSessionFrom(ctx)
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: session.Principal().ID}}}, nil
		})
		server.AddResource(&mcp.Resource{URI: "ui://ext/app.html", Name: "app", MIMEType: "text/html;profile=mcp-app"},
			func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "ui://ext/app.html", MIMEType: "text/html;profile=mcp-app", Text: "<p>hi</p>"}}}, nil
			})
		return nil
	})
	require.NoError(t, err)
	server := httptest.NewServer(auth.AuthnMiddleware(headerProvider{})(handler))
	t.Cleanup(server.Close)
	owner := http.Header{"X-User-Id": {"owner"}}

	list := rawMCPCall(t, server.URL, "tools/list", map[string]any{}, false, owner)
	tools := map[string]map[string]any{}
	for _, tool := range list["result"].(map[string]any)["tools"].([]any) {
		tools[tool.(map[string]any)["name"].(string)] = tool.(map[string]any)
	}
	require.Contains(t, tools, "whoami")
	require.Contains(t, tools, "list_agents", "core's tools stay")
	require.Equal(t, "ui://ext/app.html", tools["whoami"]["_meta"].(map[string]any)["ui"].(map[string]any)["resourceUri"])

	call := rawMCPCall(t, server.URL, "tools/call", map[string]any{"name": "whoami", "arguments": map[string]any{}}, false, owner)
	require.Equal(t, "owner", call["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"])

	read := rawMCPCall(t, server.URL, "resources/read", map[string]any{"uri": "ui://ext/app.html"}, false, owner)
	contents := read["result"].(map[string]any)["contents"].([]any)[0].(map[string]any)
	require.Equal(t, "text/html;profile=mcp-app", contents["mimeType"])
	require.Equal(t, "<p>hi</p>", contents["text"])

	request, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"whoami"}}`))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
}

func TestExtensionErrorFailsStartup(t *testing.T) {
	handler, err := extendedHandler(t, func(*mcp.Server) error { return errors.New("hook failed") })
	require.Nil(t, handler)
	require.ErrorContains(t, err, "extend /mcp: hook failed")
}

func TestExtensionsCanBeLeftOut(t *testing.T) {
	handler, err := extendedHandler(t, nil)
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	list := rawMCPCall(t, server.URL, "tools/list", map[string]any{}, false)
	require.NotEmpty(t, list["result"].(map[string]any)["tools"])
}
