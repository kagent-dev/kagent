package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/tool/mcptoolset"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"

	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

type passthroughResultContext struct{ toolContext }

func (passthroughResultContext) ToolConfirmation() *toolconfirmation.ToolConfirmation { return nil }

func TestPassthroughRunSuccessParity(t *testing.T) {
	type testCase struct {
		name   string
		result *mcpsdk.CallToolResult
		want   any
	}
	tests := []testCase{
		{name: "empty success", result: &mcpsdk.CallToolResult{}, want: ""},
		{name: "empty text", result: &mcpsdk.CallToolResult{Content: []mcpsdk.Content{
			&mcpsdk.TextContent{},
		}}, want: ""},
		{name: "adjacent text", result: &mcpsdk.CallToolResult{Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: "one"}, &mcpsdk.TextContent{Text: "two"},
		}}, want: "onetwo"},
		{name: "embedded file text", result: &mcpsdk.CallToolResult{Content: []mcpsdk.Content{
			&mcpsdk.EmbeddedResource{Resource: &mcpsdk.ResourceContents{
				URI: "repo://test/file.go", MIMEType: "text/plain", Text: "package test\n",
			}},
		}}, want: "[MCP embedded resource: uri=\"repo://test/file.go\", mimeType=\"text/plain\"]\npackage test\n"},
		{name: "ordered mixed blocks", result: &mcpsdk.CallToolResult{Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: "before"}, &mcpsdk.TextContent{Text: " file\n"},
			&mcpsdk.EmbeddedResource{Resource: &mcpsdk.ResourceContents{
				URI: "file:///notes", Text: "line one\nline two",
			}},
			&mcpsdk.TextContent{}, &mcpsdk.TextContent{Text: "after"},
			&mcpsdk.ImageContent{MIMEType: "image/png", Data: []byte{1, 2}},
			&mcpsdk.TextContent{Text: "\nend"},
		}}, want: "before file\n[MCP embedded resource: uri=\"file:///notes\"]\nline one\nline two\nafter\n" +
			"[MCP image: mimeType=\"image/png\", size=2 bytes]\nend"},
		{name: "resource link and media metadata", result: &mcpsdk.CallToolResult{Content: []mcpsdk.Content{
			&mcpsdk.ResourceLink{URI: "file:///report", MIMEType: "application/pdf",
				Name: "report", Title: "Report", Description: "A report", Size: new(int64(42))},
			&mcpsdk.ImageContent{MIMEType: "image/png", Data: []byte{1, 2, 3}},
			&mcpsdk.AudioContent{MIMEType: "audio/wav", Data: []byte{4, 5}},
		}}, want: "[MCP resource link: uri=\"file:///report\", mimeType=\"application/pdf\", " +
			"name=\"report\", title=\"Report\", description=\"A report\", size=42 bytes]\n" +
			"[MCP image: mimeType=\"image/png\", size=3 bytes]\n[MCP audio: mimeType=\"audio/wav\", size=2 bytes]"},
		{name: "text takes precedence over blob", result: &mcpsdk.CallToolResult{Content: []mcpsdk.Content{
			&mcpsdk.EmbeddedResource{Resource: &mcpsdk.ResourceContents{
				URI: "file:///both", Text: "caption", Blob: []byte("blob"),
			}},
		}}, want: "[MCP embedded resource: uri=\"file:///both\", size=4 bytes]\ncaption"},
		{name: "structured content takes precedence", result: &mcpsdk.CallToolResult{
			StructuredContent: map[string]any{"done": true},
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "ignored"},
				&mcpsdk.ImageContent{MIMEType: "image/png", Data: []byte{1}}},
		}, want: map[string]any{"done": true}},
		{name: "empty structured content takes precedence", result: &mcpsdk.CallToolResult{
			StructuredContent: map[string]any{},
			Content:           []mcpsdk.Content{&mcpsdk.TextContent{Text: "ignored"}},
		}, want: map[string]any{}},
	}
	// Exercise upstream's MIME/charset rules over the wire as well as text
	// resources: unsupported or ambiguous encodings must stay metadata-only.
	for _, blob := range []struct {
		name, mimeType string
		data           []byte
		decoded        bool
	}{
		{"UTF-8 JSON", "application/problem+json; charset=utf-8", []byte(`{"ok":true}`), true},
		{"ASCII", "text/plain; charset=us-ascii", []byte("hello"), true},
		{"binary", "application/pdf", []byte{1, 2, 3}, false},
		{"empty blob", "text/plain", nil, false},
		{"invalid UTF-8", "text/plain", []byte{0xff}, false},
		{"non-ASCII", "text/plain; charset=ascii", []byte("é"), false},
		{"unsupported charset", "text/plain; charset=utf-16", []byte("hello"), false},
		{"invalid charset", "text/plain; charset", []byte("hello"), false},
		{"extended charset", "text/plain; charset*=utf-16", []byte("hello"), false},
		{"unclosed quote", `text/plain; name="a; charset=utf-16`, []byte("hello"), false},
		{"misplaced quote", `text/plain; name=a"; charset=utf-16; x="b`, []byte("hello"), false},
		{"invalid quoted name", `text/plain; "charset"=utf-16`, []byte("hello"), false},
		{"quoted charset text", `text/plain; name="a; charset=utf-16"`, []byte("hello"), true},
		{"escaped quote", `text/plain; name="a\"; charset=utf-16"`, []byte("hello"), true},
		{"unrelated invalid parameter", "text/plain; broken", []byte("hello"), true},
	} {
		want := fmt.Sprintf("[MCP embedded resource: uri=\"file:///blob\", mimeType=%q", blob.mimeType)
		if blob.decoded {
			want += "]\n" + string(blob.data)
		} else if len(blob.data) > 0 {
			want += fmt.Sprintf(", size=%d bytes]", len(blob.data))
		} else {
			want += "]"
		}
		tests = append(tests, testCase{name: blob.name, result: &mcpsdk.CallToolResult{Content: []mcpsdk.Content{
			&mcpsdk.EmbeddedResource{Resource: &mcpsdk.ResourceContents{
				URI: "file:///blob", MIMEType: blob.mimeType, Blob: blob.data,
			}},
		}}, want: want})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
			server.AddTool(&mcpsdk.Tool{Name: "result", InputSchema: map[string]any{"type": "object"}},
				func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
					return tt.result, nil
				})
			srv := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
				return server
			}, nil))
			t.Cleanup(srv.Close)
			ctx := context.WithValue(t.Context(), models.BearerTokenKey, "test-caller")
			ts := &passthroughToolset{params: mcpServerParams{
				URL: srv.URL, ServerType: "http", BearerTokenPassthrough: true,
			}}
			tools, err := ts.Tools(testReadonlyContext{Context: ctx})
			require.NoError(t, err)
			require.Len(t, tools, 1)
			callCtx := passthroughResultContext{toolContext{ctx: ctx}}
			got, err := tools[0].(functionTool).Run(callCtx, map[string]any{})
			require.NoError(t, err)
			require.Equal(t, map[string]any{"output": tt.want}, got)

			// Use the actual pinned ADK converter as an independent oracle,
			// without using its shared session on the passthrough path.
			clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
			session, err := server.Connect(t.Context(), serverTransport, nil)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, session.Close()) })
			ordinary, err := mcptoolset.New(mcptoolset.Config{Transport: clientTransport})
			require.NoError(t, err)
			ordinaryTools, err := ordinary.Tools(testReadonlyContext{Context: ctx})
			require.NoError(t, err)
			require.Len(t, ordinaryTools, 1)
			want, err := ordinaryTools[0].(functionTool).Run(callCtx, map[string]any{})
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}
}

type unsupportedPassthroughContent struct{ mcpsdk.Content }

func TestFormatPassthroughUnavailableContent(t *testing.T) {
	got := formatPassthroughContent([]mcpsdk.Content{
		nil, (*mcpsdk.TextContent)(nil), (*mcpsdk.EmbeddedResource)(nil),
		&mcpsdk.EmbeddedResource{}, (*mcpsdk.ResourceLink)(nil),
		(*mcpsdk.ImageContent)(nil), (*mcpsdk.AudioContent)(nil), &unsupportedPassthroughContent{},
	})
	require.Equal(t, "[MCP content: unavailable]\n[MCP text content: unavailable]\n"+
		"[MCP embedded resource: unavailable]\n[MCP embedded resource: unavailable]\n"+
		"[MCP resource link: unavailable]\n[MCP image: unavailable]\n"+
		"[MCP audio: unavailable]\n[MCP content: unsupported]", got)
}

func TestPassthroughRunCredentialSafeErrors(t *testing.T) {
	const token = "test-sensitive-caller-token"
	for _, failure := range []string{"connect", "call", "result"} {
		t.Run(failure, func(t *testing.T) {
			server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
			server.AddTool(&mcpsdk.Tool{Name: "fail", InputSchema: map[string]any{"type": "object"}},
				func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
					if failure == "call" {
						return nil, fmt.Errorf("upstream echoed %s", token)
					}
					return &mcpsdk.CallToolResult{
						IsError: true, StructuredContent: map[string]any{"token": token},
						Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: token}},
					}, nil
				})
			handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if failure == "connect" {
					http.Error(w, token, http.StatusUnauthorized)
					return
				}
				handler.ServeHTTP(w, r)
			}))
			t.Cleanup(srv.Close)
			p := &passthroughTool{
				set: &passthroughToolset{params: mcpServerParams{
					URL: srv.URL, ServerType: "http", BearerTokenPassthrough: true,
				}},
				declaration: &genai.FunctionDeclaration{Name: "fail"},
			}
			ctx := context.WithValue(t.Context(), models.BearerTokenKey, token)
			got, err := p.Run(toolContext{ctx: ctx}, map[string]any{})
			require.Nil(t, got)
			switch failure {
			case "connect":
				require.EqualError(t, err, "failed to connect passthrough MCP server")
			case "call":
				require.EqualError(t, err, "failed to call passthrough MCP tool")
			case "result":
				require.EqualError(t, err, "passthrough MCP tool execution failed")
			}
			require.NotContains(t, err.Error(), token)
		})
	}
}

func TestPassthroughMCPInvocationIsolation(t *testing.T) {
	for _, protocol := range []string{"http", "sse"} {
		t.Run(protocol, func(t *testing.T) {
			server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
			server.AddTool(&mcpsdk.Tool{Name: "echo", InputSchema: map[string]any{"type": "object"}},
				func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
					return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "ok"}}}, nil
				})
			server.AddTool(&mcpsdk.Tool{Name: "hidden", InputSchema: map[string]any{"type": "object"},
				Meta: mcpsdk.Meta{"ui": map[string]any{"visibility": []any{"app"}}}},
				func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
					t.Error("app-only tool must not be exposed")
					return nil, nil
				})
			getServer := func(*http.Request) *mcpsdk.Server { return server }
			var handler http.Handler = mcpsdk.NewStreamableHTTPHandler(getServer, nil)
			if protocol == "sse" {
				handler = mcpsdk.NewSSEHandler(getServer, nil)
			}
			var mu sync.Mutex
			var received []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				received = append(received, r.Header.Get("Authorization"))
				mu.Unlock()
				handler.ServeHTTP(w, r)
			}))
			defer srv.Close()
			ts := &passthroughToolset{params: mcpServerParams{URL: srv.URL, ServerType: protocol,
				BearerTokenPassthrough: true, PropagateToken: true,
				Headers:        map[string]string{"authorization": "discovery-only"},
				HeaderProvider: func(context.Context) map[string]string { panic("must not consult generic token provider") },
			}}
			missing := a2aCtx(map[string][]string{"Authorization": {"Bearer runtime-service-token"}})
			_, err := ts.Tools(testReadonlyContext{Context: missing})
			require.ErrorContains(t, err, "requires an authenticated invocation")
			require.Empty(t, received, "no initialize or tools/list before token validation")

			alice := context.WithValue(t.Context(), models.BearerTokenKey, "alice")
			tools, err := ts.Tools(testReadonlyContext{Context: alice})
			require.NoError(t, err)
			require.Len(t, tools, 1)
			require.Equal(t, "echo", tools[0].Name())
			mu.Lock()
			for _, auth := range received {
				require.Equal(t, "Bearer alice", auth)
			}
			received = nil
			mu.Unlock()
			ts.filter = map[string]bool{"not-exposed": true}
			filtered, err := ts.Tools(testReadonlyContext{Context: alice})
			require.NoError(t, err)
			require.Empty(t, filtered)
			mu.Lock()
			received = nil
			mu.Unlock()

			// A tool obtained by Alice must use Bob's current invocation when a
			// deferred/resumed call executes, not Alice's old session context.
			bob := context.WithValue(t.Context(), models.BearerTokenKey, "bob")
			output, err := tools[0].(functionTool).Run(toolContext{ctx: bob}, map[string]any{})
			require.NoError(t, err)
			require.Equal(t, map[string]any{"output": "ok"}, output)
			mu.Lock()
			require.NotEmpty(t, received)
			for _, auth := range received {
				require.Equal(t, "Bearer bob", auth)
			}
			before := len(received)
			mu.Unlock()
			_, err = tools[0].(functionTool).Run(toolContext{ctx: missing}, nil)
			require.Error(t, err)
			mu.Lock()
			require.Len(t, received, before)
			mu.Unlock()
		})
	}
}

func TestPassthroughToolsetsDoNotDiscoverAtStartup(t *testing.T) {
	toolsets := CreateToolsets(t.Context(),
		[]*configv1.ADKHTTPMCPServerConfig{{Params: &configv1.ADKHTTPConnectionParams{Url: "http://invalid.invalid"},
			BearerTokenPassthrough: true}}, nil, nil, false, nil)
	require.Len(t, toolsets, 1)
	_, err := toolsets[0].Tools(testReadonlyContext{Context: t.Context()})
	require.ErrorContains(t, err, "requires an authenticated invocation")
}

func TestPassthroughRejectsRedirectAndForeignSSEEndpoint(t *testing.T) {
	var foreignRequests int
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { foreignRequests++ }))
	defer foreign.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	ctx := context.WithValue(t.Context(), models.BearerTokenKey, "caller")
	ts := &passthroughToolset{params: mcpServerParams{URL: origin.URL, ServerType: "http", BearerTokenPassthrough: true}}
	_, err := ts.Tools(testReadonlyContext{Context: ctx})
	require.Error(t, err)
	require.Zero(t, foreignRequests)
	rt := &headerRoundTripper{base: http.DefaultTransport, bearerTokenPassthrough: true, endpoint: origin.URL}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, foreign.URL, nil)
	require.NoError(t, err)
	_, err = rt.RoundTrip(req)
	require.ErrorContains(t, err, "cannot change destination")
	require.Zero(t, foreignRequests)
}
