package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
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
			require.Equal(t, ordinaryTools[0].(functionTool).Declaration(), tools[0].(functionTool).Declaration())
			want, err := ordinaryTools[0].(functionTool).Run(callCtx, map[string]any{})
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}
}

func TestPassthroughRunCredentialSafeErrors(t *testing.T) {
	const token = "test-sensitive-caller-token"
	for _, failure := range []string{"connect", "call", "result", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(t.Context(), models.BearerTokenKey, token))
			defer cancel()
			server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
			server.AddTool(&mcpsdk.Tool{Name: "fail", InputSchema: map[string]any{"type": "object"}},
				func(ctx context.Context, _ *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
					if failure == "call" {
						return nil, fmt.Errorf("upstream echoed %s", token)
					}
					if failure == "cancel" {
						cancel()
						<-ctx.Done()
						return nil, ctx.Err()
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
			got, err := p.Run(passthroughResultContext{toolContext{ctx: ctx}}, map[string]any{})
			require.Nil(t, got)
			if failure == "cancel" {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.EqualError(t, err, "passthrough MCP operation failed")
			}
			require.NotContains(t, err.Error(), token)
			require.Eventually(t, func() bool {
				return len(slices.Collect(server.Sessions())) == 0
			}, time.Second, time.Millisecond)
		})
	}
}

func TestPassthroughDiscoveryFailureCleanup(t *testing.T) {
	for _, protocol := range []string{"http", "sse"} {
		for _, failure := range []string{"initialize", "list"} {
			t.Run(protocol+"/"+failure, func(t *testing.T) {
				server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
				server.AddReceivingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
					return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
						if method == "tools/list" && failure == "list" {
							return nil, fmt.Errorf("echoed test-sensitive-token")
						}
						result, err := next(ctx, method, req)
						if method == "initialize" && err == nil && failure == "initialize" {
							// SDK v1.5.0 returns without closing on unsupported
							// protocol versions; the transport owner must close it.
							result.(*mcpsdk.InitializeResult).ProtocolVersion = "unsupported"
						}
						return result, err
					}
				})
				getServer := func(*http.Request) *mcpsdk.Server { return server }
				var handler http.Handler = mcpsdk.NewStreamableHTTPHandler(getServer, nil)
				if protocol == "sse" {
					handler = mcpsdk.NewSSEHandler(getServer, nil)
				}
				srv := httptest.NewServer(handler)
				t.Cleanup(srv.Close)
				ts := &passthroughToolset{params: mcpServerParams{
					URL: srv.URL, ServerType: protocol, BearerTokenPassthrough: true,
				}}
				ctx := context.WithValue(t.Context(), models.BearerTokenKey, "test-sensitive-token")
				_, err := ts.Tools(testReadonlyContext{Context: ctx})
				require.EqualError(t, err, "passthrough MCP operation failed")
				require.Eventually(t, func() bool {
					return len(slices.Collect(server.Sessions())) == 0
				}, time.Second, time.Millisecond)
			})
		}
	}
}

func TestPassthroughRetryCleanupAndVisibility(t *testing.T) {
	for _, tc := range []struct {
		name      string
		method    string
		revoke    bool
		failAgain bool
	}{
		{name: "list", method: "tools/list"},
		{name: "call", method: "tools/call"},
		{name: "revoked on call retry", method: "tools/call", revoke: true},
		{name: "list retry fails", method: "tools/list", failAgain: true},
		{name: "call retry fails", method: "tools/call", failAgain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
			var calls, initialized atomic.Int32
			server.AddTool(&mcpsdk.Tool{Name: "echo", InputSchema: map[string]any{"type": "object"}},
				func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
					calls.Add(1)
					return &mcpsdk.CallToolResult{}, nil
				})
			server.AddReceivingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
				return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
					if method == "initialize" {
						initialized.Add(1)
					}
					result, err := next(ctx, method, req)
					if method == "tools/list" && err == nil && tc.revoke && initialized.Load() > 1 {
						// The same name is no longer model-visible after reconnect.
						result.(*mcpsdk.ListToolsResult).Tools = []*mcpsdk.Tool{{
							Name: "echo", InputSchema: map[string]any{"type": "object"},
							Meta: mcpsdk.Meta{"ui": map[string]any{"visibility": []any{"app"}}},
						}}
					}
					return result, err
				}
			})
			handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
			var failures atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
						return
					}
					r.Body = io.NopCloser(bytes.NewReader(body))
					var request struct{ Method string }
					if err := json.Unmarshal(body, &request); err != nil {
						t.Error(err)
						return
					}
					if request.Method == tc.method && (failures.Add(1) == 1 || tc.failAgain) {
						// Expire the session at the actual HTTP boundary so ADK
						// must reconnect, not reuse a single-use SDK transport.
						for ss := range server.Sessions() {
							if ss.ID() == r.Header.Get("Mcp-Session-Id") {
								if err := ss.Close(); err != nil {
									t.Error(err)
								}
							}
						}
						http.Error(w, "expired", http.StatusNotFound)
						return
					}
				}
				handler.ServeHTTP(w, r)
			}))
			t.Cleanup(srv.Close)
			ts := &passthroughToolset{params: mcpServerParams{
				URL: srv.URL, ServerType: "http", BearerTokenPassthrough: true,
			}}
			ctx := context.WithValue(t.Context(), models.BearerTokenKey, "caller")
			var err error
			if tc.method == "tools/list" {
				_, err = ts.Tools(testReadonlyContext{Context: ctx})
			} else {
				p := &passthroughTool{set: ts, declaration: &genai.FunctionDeclaration{Name: "echo"}}
				_, err = p.Run(passthroughResultContext{toolContext{ctx: ctx}}, map[string]any{})
			}
			if tc.revoke || tc.failAgain {
				require.EqualError(t, err, "passthrough MCP operation failed")
			} else {
				require.NoError(t, err)
			}
			require.EqualValues(t, 2, initialized.Load())
			if tc.method == "tools/call" && !tc.revoke && !tc.failAgain {
				require.EqualValues(t, 1, calls.Load())
			} else {
				require.Zero(t, calls.Load())
			}
			require.Eventually(t, func() bool {
				return len(slices.Collect(server.Sessions())) == 0
			}, time.Second, time.Millisecond)
		})
	}
}

type passthroughApprovalContext struct {
	passthroughResultContext
	confirmation *toolconfirmation.ToolConfirmation
	actions      session.EventActions
}

func (p *passthroughApprovalContext) ToolConfirmation() *toolconfirmation.ToolConfirmation {
	return p.confirmation
}
func (*passthroughApprovalContext) RequestConfirmation(string, any) error { return nil }
func (p *passthroughApprovalContext) Actions() *session.EventActions      { return &p.actions }

func TestPassthroughApprovalAndVisibility(t *testing.T) {
	var requests, calls atomic.Int32
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
	definition := &mcpsdk.Tool{Name: "echo", InputSchema: map[string]any{"type": "object"}}
	run := func(ctx context.Context, _ *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		calls.Add(1)
		return &mcpsdk.CallToolResult{}, nil
	}
	server.AddTool(definition, run)
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	sets := CreateToolsets(t.Context(), []*configv1.ADKHTTPMCPServerConfig{{
		Params: &configv1.ADKHTTPConnectionParams{Url: srv.URL}, BearerTokenPassthrough: true,
		RequireApproval: true, Tools: []string{"echo"},
	}}, nil, nil, false, nil)
	require.Len(t, sets, 1)
	ctx := context.WithValue(t.Context(), models.BearerTokenKey, "alice")
	tools, err := sets[0].Tools(testReadonlyContext{Context: ctx})
	require.NoError(t, err)
	require.Len(t, tools, 1)
	fn := tools[0].(functionTool)
	callCtx := &passthroughApprovalContext{passthroughResultContext: passthroughResultContext{toolContext{ctx: ctx}}}
	req := &model.LLMRequest{}
	require.NoError(t, fn.ProcessRequest(callCtx, req))
	require.Same(t, fn, req.Tools["echo"], "ADK must execute the approval/lifecycle wrapper")
	before := requests.Load()
	_, err = fn.Run(callCtx, nil)
	require.ErrorIs(t, err, tool.ErrConfirmationRequired)
	require.True(t, callCtx.actions.SkipSummarization)
	callCtx.confirmation = &toolconfirmation.ToolConfirmation{Confirmed: false}
	_, err = fn.Run(callCtx, nil)
	require.ErrorIs(t, err, tool.ErrConfirmationRejected)
	require.Equal(t, before, requests.Load(), "pending/rejected approval must not connect")
	callCtx.confirmation.Confirmed = true
	callCtx.ctx = context.WithValue(t.Context(), models.BearerTokenKey, "bob")
	_, err = fn.Run(callCtx, map[string]any{})
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())

	hidden := *definition
	hidden.Meta = mcpsdk.Meta{"ui": map[string]any{"visibility": []any{"app"}}}
	server.AddTool(&hidden, run)
	_, err = fn.Run(callCtx, nil)
	require.ErrorContains(t, err, "no longer available")
	require.EqualValues(t, 1, calls.Load(), "approval cannot bypass changed visibility")
}

func TestPassthroughErrorClassification(t *testing.T) {
	for _, safe := range []error{
		context.Canceled, context.DeadlineExceeded, tool.ErrConfirmationRequired, tool.ErrConfirmationRejected,
	} {
		err := passthroughError(fmt.Errorf("echoed test-sensitive-token: %w", safe))
		require.ErrorIs(t, err, safe)
		require.NotContains(t, err.Error(), "test-sensitive-token")
	}
	require.EqualError(t, passthroughError(errors.New("test-sensitive-token")), "passthrough MCP operation failed")
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
			owners := make(map[string]string)
			var mixedIdentity bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				received = append(received, r.Header.Get("Authorization"))
				mu.Unlock()
				handler.ServeHTTP(w, r)
				id := r.Header.Get("Mcp-Session-Id")
				if id == "" {
					id = r.URL.Query().Get("sessionid")
				}
				if id == "" {
					id = w.Header().Get("Mcp-Session-Id")
				}
				if id != "" {
					mu.Lock()
					auth := r.Header.Get("Authorization")
					if owner, ok := owners[id]; ok && owner != auth {
						mixedIdentity = true
					}
					owners[id] = auth
					mu.Unlock()
				}
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
			_, err = tools[0].(functionTool).Run(passthroughResultContext{toolContext{ctx: alice}}, nil)
			require.ErrorContains(t, err, "no longer available", "execution must reapply the selected-tool filter")
			ts.filter = nil
			mu.Lock()
			received = nil
			mu.Unlock()

			// A tool obtained by Alice must use Bob's current invocation when a
			// deferred/resumed call executes, not Alice's old session context.
			bob := context.WithValue(t.Context(), models.BearerTokenKey, "bob")
			output, err := tools[0].(functionTool).Run(passthroughResultContext{toolContext{ctx: bob}}, map[string]any{})
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

			// The same discovered descriptor is safe for simultaneous callers.
			results := make(chan error, 2)
			for _, ctx := range []context.Context{alice, bob} {
				go func() {
					output, err := tools[0].(functionTool).Run(passthroughResultContext{toolContext{ctx: ctx}}, map[string]any{})
					if err == nil && output["output"] != "ok" {
						err = fmt.Errorf("unexpected tool output")
					}
					results <- err
				}()
			}
			for range 2 {
				require.NoError(t, <-results)
			}
			require.Eventually(t, func() bool {
				return len(slices.Collect(server.Sessions())) == 0
			}, time.Second, time.Millisecond, "all HTTP/SSE sessions must close at operation boundaries")
			mu.Lock()
			require.False(t, mixedIdentity, "initialize/list/call/cleanup must keep one identity per session")
			require.Len(t, owners, 6, "each listing and call must create a separate session")
			counts := make(map[string]int)
			for _, owner := range owners {
				counts[owner]++
			}
			require.Equal(t, map[string]int{"Bearer alice": 4, "Bearer bob": 2}, counts)
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
