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

func extendedHandler(t *testing.T, extend func(Registry) error) (*Handler, error) {
	t.Helper()
	return New(Config{Agents: testAgentService(t), Sessions: testSessionService(), Checkpoints: testCheckpointService(), Gateway: &fakeGateway{}, Extensions: extend})
}

func objectSchema() *jsonschema.Schema { return &jsonschema.Schema{Type: "object"} }

func noopTool(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{}, nil
}

func noopResource(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return &mcp.ReadResourceResult{}, nil
}

func TestExtensionsServeToolsAndResourcesBehindTheCoreAuthenticator(t *testing.T) {
	handler, err := extendedHandler(t, func(registry Registry) error {
		if err := registry.AddTool(&mcp.Tool{
			Name:        "whoami",
			InputSchema: objectSchema(),
			Meta:        mcp.Meta{"ui": map[string]any{"resourceUri": "ui://ext/app.html"}},
		}, func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			session, _ := auth.AuthSessionFrom(ctx)
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: session.Principal().ID}}}, nil
		}); err != nil {
			return err
		}
		return registry.AddResource(&mcp.Resource{URI: "ui://ext/app.html", Name: "app", MIMEType: "text/html;profile=mcp-app"},
			func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "ui://ext/app.html", MIMEType: "text/html;profile=mcp-app", Text: "<p>hi</p>"}}}, nil
			})
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

func TestExtensionsRefuseWhatCannotBeRegistered(t *testing.T) {
	hookErr := errors.New("hook failed")
	for _, test := range []struct {
		name    string
		extend  func(Registry) error
		wantErr string
	}{
		{"core tool name", func(r Registry) error {
			return r.AddTool(&mcp.Tool{Name: listToolName, InputSchema: objectSchema()}, noopTool)
		}, `tool "list_sessions" is already registered`},
		{"core tool name, error dropped", func(r Registry) error {
			_ = r.AddTool(&mcp.Tool{Name: invokeToolName, InputSchema: objectSchema()}, noopTool)
			return nil
		}, `tool "invoke_agent" is already registered`},
		{"repeated tool name", func(r Registry) error {
			_ = r.AddTool(&mcp.Tool{Name: "own", InputSchema: objectSchema()}, noopTool)
			return r.AddTool(&mcp.Tool{Name: "own", InputSchema: objectSchema()}, noopTool)
		}, `tool "own" is already registered`},
		{"repeated resource URI", func(r Registry) error {
			_ = r.AddResource(&mcp.Resource{URI: "ui://own"}, noopResource)
			return r.AddResource(&mcp.Resource{URI: "ui://own"}, noopResource)
		}, `resource "ui://own" is already registered`},
		{"repeated resource template", func(r Registry) error {
			_ = r.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: "ui://own/{id}"}, noopResource)
			return r.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: "ui://own/{id}"}, noopResource)
		}, `resource template "ui://own/{id}" is already registered`},
		{"missing input schema", func(r Registry) error {
			return r.AddTool(&mcp.Tool{Name: "own"}, noopTool)
		}, `tool "own" needs an input schema`},
		{"input schema that is not an object", func(r Registry) error {
			return r.AddTool(&mcp.Tool{Name: "own", InputSchema: &jsonschema.Schema{Type: "string"}}, noopTool)
		}, `tool "own": `},
		{"nil tool handler", func(r Registry) error {
			return r.AddTool(&mcp.Tool{Name: "own", InputSchema: objectSchema()}, nil)
		}, "a tool needs a name and a handler"},
		{"empty resource URI", func(r Registry) error {
			return r.AddResource(&mcp.Resource{}, noopResource)
		}, "a resource needs a URI and a handler"},
		{"invalid URI template", func(r Registry) error {
			return r.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: "ui://own/{"}, noopResource)
		}, `resource template "ui://own/{": `},
		{"hook error", func(Registry) error { return hookErr }, "hook failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, err := extendedHandler(t, test.extend)
			require.Nil(t, handler)
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestExtensionsCanBeLeftOut(t *testing.T) {
	handler, err := extendedHandler(t, nil)
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	list := rawMCPCall(t, server.URL, "tools/list", map[string]any{}, false)
	require.NotEmpty(t, list["result"].(map[string]any)["tools"])
}
