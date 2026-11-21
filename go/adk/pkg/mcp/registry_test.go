package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	adkplugin "google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/kagent-dev/kagent/go/adk/pkg/auth"
	"github.com/kagent-dev/kagent/go/adk/pkg/constants"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	kagentsession "github.com/kagent-dev/kagent/go/adk/pkg/session"
	"github.com/kagent-dev/kagent/go/adk/pkg/sts"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func TestSTSDelegatedTokenReachesMCP(t *testing.T) {
	for _, transport := range []string{"http", "sse"} {
		t.Run("Given STS over "+transport+", when callers share MCP tools, assert that each request carries its delegated identity", func(t *testing.T) {
			checkSTSDelegatedTokenReachesMCP(t, transport)
		})
	}
}

func checkSTSDelegatedTokenReachesMCP(t *testing.T, transport string) {
	t.Helper()
	// Given an STS exchange and an actor-local session whose native ID differs
	// from the public ID, when the runner lists and invokes MCP tools, assert
	// that HTTP requests carry the delegated identity through transport wrappers.
	const caller = "caller-token"
	const delegated = "delegated-token"
	const otherCaller = "other-caller-token"
	const otherDelegated = "other-delegated-token"
	const appName = "sts-test"
	const toolName = "identity"
	const discoverMethod = "server/discover"
	const initializeMethod = "initialize"
	const listMethod = "tools/list"
	const callMethod = "tools/call"
	delegatedTokens := map[string]string{caller: delegated, otherCaller: otherDelegated}
	var tokenServer *httptest.Server
	tokenServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "invalid exchange", http.StatusBadRequest)
				return
			}
			token, ok := delegatedTokens[r.Form.Get("subject_token")]
			if !ok {
				http.Error(w, "unexpected subject", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token, "issued_token_type": string(sts.TokenTypeJWT), "token_type": "Bearer", "expires_in": 3600})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": tokenServer.URL, "token_endpoint": tokenServer.URL + "/token"})
	}))
	t.Cleanup(tokenServer.Close)
	integration, err := sts.NewSTSIntegration(tokenServer.URL, "", func(context.Context) (string, error) { return "actor-token", nil }, nil, 5, true, false)
	require.NoError(t, err)
	plugin := sts.NewTokenPropagationPlugin(integration, slog.New(slog.DiscardHandler), nil, nil)
	adkPlugin, err := plugin.ADKPlugin()
	require.NoError(t, err)

	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: appName}, &mcpsdk.ServerOptions{SupportedProtocolVersions: []string{"2025-11-25"}})
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: toolName}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		return nil, map[string]any{"identity": "delegated"}, nil
	})
	var handler http.Handler = mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
	if transport == "sse" {
		handler = mcpsdk.NewSSEHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
	}
	type observedRequest struct {
		method        string
		authorization string
		caller        string
	}
	var mu sync.Mutex
	var requests []observedRequest
	streamOpened := make(chan struct{}, 1)
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := observedRequest{method: r.Method, authorization: r.Header.Get(constants.AuthorizationHeader)}
		if r.Method == http.MethodPost {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var rpc struct {
				Method string
				Params struct{ Arguments struct{ Caller string } }
			}
			if err := json.Unmarshal(body, &rpc); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			request.method, request.caller = rpc.Method, rpc.Params.Arguments.Caller
		}
		mu.Lock()
		requests = append(requests, request)
		mu.Unlock()
		if r.Method == http.MethodGet {
			select {
			case streamOpened <- struct{}{}:
			default:
			}
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(mcpServer.Close)
	// Make the HTTP client's deadline earlier than the run's deadline, ensuring
	// net/http itself wraps the context before header injection.
	var httpTools []*configv1.ADKHTTPMCPServerConfig
	var sseTools []*configv1.ADKSSEMCPServerConfig
	if transport == "sse" {
		sseTools = []*configv1.ADKSSEMCPServerConfig{{Params: &configv1.ADKSSEConnectionParams{Url: mcpServer.URL, Timeout: durationpb.New(5 * time.Second)}}}
	} else {
		httpTools = []*configv1.ADKHTTPMCPServerConfig{{Params: &configv1.ADKHTTPConnectionParams{Url: mcpServer.URL, Timeout: durationpb.New(5 * time.Second)}}}
	}
	toolsets := CreateToolsets(t.Context(), httpTools, sseTools, nil, false, plugin.HeaderProvider)
	require.Len(t, toolsets, 1)
	inner := toolsets[0].(*mcpAppToolset).inner.(*scopedToolset).Toolset
	closer := inner.(io.Closer)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	var called bool
	agent, err := adkagent.New(adkagent.Config{Name: appName, Run: func(ctx adkagent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			toolCtx := adkagent.NewToolContext(ctx, "call-1", nil, nil)
			tools, err := toolsets[0].Tools(toolCtx)
			if err != nil {
				yield(nil, err)
				return
			}
			if len(tools) != 1 {
				yield(nil, fmt.Errorf("discovered %d tools, want one", len(tools)))
				return
			}
			_, err = tools[0].(functionTool).Run(toolCtx, map[string]any{"caller": models.BearerTokenFromContext(ctx)})
			if err != nil {
				yield(nil, err)
				return
			}
			called = true
		}
	}})
	require.NoError(t, err)
	sessions := &kagentsession.LocalSessionService{Service: session.InMemoryService()}
	_, err = sessions.Create(t.Context(), &session.CreateRequest{AppName: appName, UserID: "user", SessionID: "public-session"})
	require.NoError(t, err)
	run, err := runner.New(runner.Config{AppName: appName, Agent: agent, SessionService: sessions, PluginConfig: runner.PluginConfig{Plugins: []*adkplugin.Plugin{adkPlugin}}})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	invoke := func(bearer string) {
		t.Helper()
		called = false
		callCtx := context.WithValue(ctx, models.BearerTokenKey, bearer)
		for _, err := range run.Run(callCtx, "user", "public-session", genai.NewContentFromText("invoke", genai.RoleUser), adkagent.RunConfig{}) {
			require.NoError(t, err)
		}
		require.True(t, called, "the MCP tool must be invoked")
	}
	invoke(caller)
	select {
	case <-streamOpened:
	case <-ctx.Done():
		t.Fatal("MCP event stream did not open")
	}
	mu.Lock()
	firstRequests := append([]observedRequest(nil), requests...)
	mu.Unlock()
	methods := make(map[string]bool)
	for _, request := range firstRequests {
		methods[request.method] = true
		require.Equal(t, "Bearer "+delegated, request.authorization, "method %s", request.method)
	}
	for _, method := range []string{discoverMethod, initializeMethod, listMethod, callMethod, http.MethodGet} {
		require.True(t, methods[method], "must exercise %s", method)
	}
	// A reused connection must resolve the current call's credential. A call
	// without a bearer must not inherit either of the cached delegated tokens.
	invoke(otherCaller)
	invoke("")
	invoke(caller)
	// Close the SDK session before the test servers, including its detached SSE stream.
	require.NoError(t, closer.Close())
	mu.Lock()
	defer mu.Unlock()
	var calls []observedRequest
	for _, request := range requests {
		if request.method == callMethod {
			calls = append(calls, request)
		}
	}
	require.Equal(t, []observedRequest{
		{method: callMethod, authorization: "Bearer " + delegated, caller: caller},
		{method: callMethod, authorization: "Bearer " + otherDelegated, caller: otherCaller},
		{method: callMethod},
		{method: callMethod, authorization: "Bearer " + delegated, caller: caller},
	}, calls)
}

func TestCreateTransportBuildsStdioCommand(t *testing.T) {
	dir := t.TempDir()
	transport, err := createTransport(t.Context(), mcpServerParams{
		ServerType: "stdio", Command: "server", Args: []string{"--stdio"}, Env: map[string]string{"PLUGIN_ROOT": dir}, Dir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	stdio, ok := transport.(*mcpsdk.CommandTransport)
	if !ok || filepath.Base(stdio.Command.Path) != "server" || len(stdio.Command.Args) != 2 || stdio.Command.Dir != dir {
		t.Fatalf("stdio transport = %#v", transport)
	}
}

// newTestTransport returns a transport private to the test. These parallel
// tests must not share newTestTransport(t): httptest.Server.Close (deferred
// in each test) also closes the default transport's idle connections, which
// can break another test's request with "http: CloseIdleConnections called".
func newTestTransport(t *testing.T) http.RoundTripper {
	t.Helper()
	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	return tr
}

// a2aCtx builds a context that carries an A2A CallContext with the given headers.
// Keys are stored case-insensitively by NewServiceParams, matching the behaviour
// of a real A2A server.
func a2aCtx(headers map[string][]string) context.Context {
	ctx, _ := a2asrv.NewCallContext(context.Background(), a2asrv.NewServiceParams(headers))
	return ctx
}

// TestAllowedRequestHeaders_ForwardsMatchingHeaders verifies that headers listed
// in allowedHeaders are forwarded when they are present in the A2A CallContext.
func TestAllowedRequestHeaders_ForwardsMatchingHeaders(t *testing.T) {
	t.Parallel()
	var capturedAuth, capturedCustom, capturedStatic string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedCustom = r.Header.Get("X-Custom")
		capturedStatic = r.Header.Get("X-Static")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx := a2aCtx(map[string][]string{
		"Authorization": {"Bearer token123"},
		"X-Custom":      {"custom-value"},
		"X-Ignored":     {"should-not-appear"},
	})

	rt := &headerRoundTripper{
		base:           newTestTransport(t),
		headers:        map[string]string{"X-Static": "static-value"},
		allowedHeaders: []string{"Authorization", "X-Custom"},
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	resp.Body.Close()

	if capturedAuth != "Bearer token123" {
		t.Errorf("Authorization: got %q, want %q", capturedAuth, "Bearer token123")
	}
	if capturedCustom != "custom-value" {
		t.Errorf("X-Custom: got %q, want %q", capturedCustom, "custom-value")
	}
	if capturedStatic != "static-value" {
		t.Errorf("X-Static: got %q, want %q", capturedStatic, "static-value")
	}
}

// TestAllowedRequestHeaders_StaticOverridesDynamic verifies that a statically
// configured header wins over the same header forwarded from the A2A request.
func TestAllowedRequestHeaders_StaticOverridesDynamic(t *testing.T) {
	t.Parallel()
	var capturedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx := a2aCtx(map[string][]string{
		"Authorization": {"Bearer incoming"},
	})

	rt := &headerRoundTripper{
		base:           newTestTransport(t),
		headers:        map[string]string{"Authorization": "Bearer static"},
		allowedHeaders: []string{"Authorization"},
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	resp.Body.Close()

	if capturedAuth != "Bearer static" {
		t.Errorf("Authorization: got %q, want %q", capturedAuth, "Bearer static")
	}
}

// TestAllowedRequestHeaders_NoA2AContext verifies that no headers are forwarded
// when the context does not carry an A2A CallContext.
func TestAllowedRequestHeaders_NoA2AContext(t *testing.T) {
	t.Parallel()
	var capturedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := &headerRoundTripper{
		base:           newTestTransport(t),
		allowedHeaders: []string{"Authorization"},
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	resp.Body.Close()

	if capturedAuth != "" {
		t.Errorf("Authorization should be empty without A2A context, got %q", capturedAuth)
	}
}

// TestAllowedRequestHeaders_IgnoresNonAllowed verifies that headers not listed
// in allowedHeaders are not forwarded even if they appear in the A2A request.
func TestAllowedRequestHeaders_IgnoresNonAllowed(t *testing.T) {
	t.Parallel()
	var capturedIgnored string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedIgnored = r.Header.Get("X-Ignored")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx := a2aCtx(map[string][]string{
		"X-Ignored": {"should-not-appear"},
	})

	rt := &headerRoundTripper{
		base:           newTestTransport(t),
		allowedHeaders: []string{"Authorization"},
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	resp.Body.Close()

	if capturedIgnored != "" {
		t.Errorf("X-Ignored should not be forwarded, got %q", capturedIgnored)
	}
}

// TestAllowedRequestHeaders_EmptyAllowedList verifies that allowedRequestHeaders
// returns nil immediately when the allowed list is empty.
func TestAllowedRequestHeaders_EmptyAllowedList(t *testing.T) {
	t.Parallel()
	ctx := a2aCtx(map[string][]string{
		"Authorization": {"Bearer token"},
	})

	got := allowedRequestHeaders(ctx, nil)
	if got != nil {
		t.Errorf("expected nil for empty allowed list, got %v", got)
	}

	got = allowedRequestHeaders(ctx, []string{})
	if got != nil {
		t.Errorf("expected nil for empty allowed list, got %v", got)
	}
}

func TestMCPAppToolNamesFromToolsets(t *testing.T) {
	t.Parallel()

	inner := &stubToolset{name: "mcp-server"}
	toolsets := []tool.Toolset{
		&mcpAppToolset{inner: inner, discovery: &mcpAppDiscovery{appToolNames: map[string]bool{"show_board": true}}},
		&mcpAppToolset{inner: inner, discovery: &mcpAppDiscovery{appToolNames: map[string]bool{"refresh": true}}},
		inner,
	}

	got := MCPAppToolNamesFromToolsets(t.Context(), toolsets)
	if len(got) != 2 || !got["show_board"] || !got["refresh"] {
		t.Fatalf("MCPAppToolNamesFromToolsets() = %#v, want show_board and refresh", got)
	}
}

func TestInitializeToolSetRecoversWhenServerStartsAfterInitialization(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	serverURL := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("listener.Close() error = %v", err)
	}

	toolset, err := initializeToolSet(t.Context(), mcpServerParams{
		URL:        serverURL,
		ServerType: "http",
	}, map[string]bool{"getWeather": true, "disabledTool": false})
	if err != nil {
		t.Fatalf("initializeToolSet() error = %v, want lazy fallback", err)
	}

	mcpServer := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "weather-test", Version: "1.0.0"}, nil)
	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{Name: "getWeather"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		return nil, map[string]any{"weather": "sunny"}, nil
	})
	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{Name: "disabledTool"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		return nil, nil, nil
	})
	listener, err = net.Listen("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("net.Listen() after initialization error = %v", err)
	}
	httpServer := &http.Server{Handler: mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return mcpServer
	}, nil)}
	t.Cleanup(func() {
		_ = httpServer.Close()
	})
	go func() {
		_ = httpServer.Serve(listener)
	}()

	tools, err := toolset.Tools(testReadonlyContext{Context: t.Context()})
	if err != nil {
		t.Fatalf("toolset.Tools() error = %v, want recovery after server startup", err)
	}
	if len(tools) != 1 || tools[0].Name() != "getWeather" {
		t.Fatalf("toolset.Tools() = %#v, want getWeather", tools)
	}
}

type testReadonlyContext struct {
	context.Context
}

func (testReadonlyContext) UserContent() *genai.Content          { return nil }
func (testReadonlyContext) InvocationID() string                 { return "test-invocation" }
func (testReadonlyContext) AgentName() string                    { return "test-agent" }
func (testReadonlyContext) ReadonlyState() session.ReadonlyState { return nil }
func (testReadonlyContext) UserID() string                       { return "test-user" }
func (testReadonlyContext) AppName() string                      { return "test-app" }
func (testReadonlyContext) SessionID() string                    { return "test-session" }
func (testReadonlyContext) Branch() string                       { return "" }

type stubToolset struct {
	name string
}

func (s *stubToolset) Name() string { return s.name }

func (s *stubToolset) Tools(ctx adkagent.ReadonlyContext) ([]tool.Tool, error) {
	_ = ctx
	return nil, nil
}

func TestMCPToolKindOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		meta mcpsdk.Meta
		want mcpToolKind
	}{
		{
			name: "app visibility list is app-only",
			meta: mcpsdk.Meta{"ui": map[string]any{"visibility": []any{"app"}}},
			want: mcpToolKindAppInternal,
		},
		{
			name: "app visibility string is app-only",
			meta: mcpsdk.Meta{"ui": map[string]any{"visibility": "app"}},
			want: mcpToolKindAppInternal,
		},
		{
			name: "app-only wins over a declared resource uri",
			meta: mcpsdk.Meta{"ui": map[string]any{"visibility": []any{"app"}, "resourceUri": "ui://forms/form.html"}},
			want: mcpToolKindAppInternal,
		},
		{
			name: "model and app visibility without resource is a plain model tool",
			meta: mcpsdk.Meta{"ui": map[string]any{"visibility": []any{"model", "app"}}},
			want: mcpToolKindModel,
		},
		{
			name: "model and app visibility with resource renders as app",
			meta: mcpsdk.Meta{"ui": map[string]any{"visibility": []any{"app", "model"}, "resourceUri": "ui://forms/form.html"}},
			want: mcpToolKindApp,
		},
		{
			name: "model only visibility is a plain model tool",
			meta: mcpsdk.Meta{"ui": map[string]any{"visibility": []any{"model"}}},
			want: mcpToolKindModel,
		},
		{
			name: "resource uri in ui object renders as app",
			meta: mcpsdk.Meta{"ui": map[string]any{"resourceUri": "ui://forms/form.html"}},
			want: mcpToolKindApp,
		},
		{
			name: "legacy resource uri key renders as app",
			meta: mcpsdk.Meta{"ui/resourceUri": "ui://forms/form.html"},
			want: mcpToolKindApp,
		},
		{
			name: "plain tool",
			meta: mcpsdk.Meta{},
			want: mcpToolKindModel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := mcpToolKindOf(tt.meta); got != tt.want {
				t.Fatalf("mcpToolKindOf() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestAllowedRequestHeaders_CaseInsensitiveLookup verifies that matching between
// the configured allowedHeaders and the incoming request headers is case-insensitive
// regardless of which side is lowercased or uppercased.
func TestAllowedRequestHeaders_CaseInsensitiveLookup(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		incoming map[string][]string
		allowed  []string
		wantKey  string
		wantVal  string
	}{
		{
			name:     "allowed lowercase, incoming capitalized",
			incoming: map[string][]string{"Authorization": {"Bearer x"}},
			allowed:  []string{"authorization"},
			wantKey:  "authorization",
			wantVal:  "Bearer x",
		},
		{
			name:     "allowed capitalized, incoming lowercase",
			incoming: map[string][]string{"authorization": {"Bearer y"}},
			allowed:  []string{"Authorization"},
			wantKey:  "Authorization",
			wantVal:  "Bearer y",
		},
		{
			name:     "mixed case both sides",
			incoming: map[string][]string{"X-Trace-Id": {"abc"}},
			allowed:  []string{"x-trace-id"},
			wantKey:  "x-trace-id",
			wantVal:  "abc",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := a2aCtx(tc.incoming)
			got := allowedRequestHeaders(ctx, tc.allowed)
			if got[tc.wantKey] != tc.wantVal {
				t.Errorf("got[%q] = %q, want %q (full map: %v)", tc.wantKey, got[tc.wantKey], tc.wantVal, got)
			}
		})
	}
}

// TestAllowedRequestHeaders_MultiValueFirstWins documents the behaviour for headers
// that arrive with multiple values: only the first one is forwarded. If a use case
// ever needs all values, the helper signature will have to change.
func TestAllowedRequestHeaders_MultiValueFirstWins(t *testing.T) {
	t.Parallel()
	ctx := a2aCtx(map[string][]string{
		"X-Forwarded-For": {"1.2.3.4", "5.6.7.8", "9.10.11.12"},
	})
	got := allowedRequestHeaders(ctx, []string{"X-Forwarded-For"})
	if got["X-Forwarded-For"] != "1.2.3.4" {
		t.Errorf("expected first value 1.2.3.4, got %q", got["X-Forwarded-For"])
	}
}

// TestPropagateToken_ForwardsAuthorizationToMCP verifies that when propagateToken
// is set on headerRoundTripper, the Authorization header from the incoming A2A
// CallContext is forwarded to the outbound MCP request independently of allowedHeaders.
func TestPropagateToken_ForwardsAuthorizationToMCP(t *testing.T) {
	t.Parallel()
	var capturedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx := a2aCtx(map[string][]string{
		"Authorization": {"Bearer propagated-token"},
	})

	rt := &headerRoundTripper{
		base:           newTestTransport(t),
		propagateToken: true,
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	resp.Body.Close()

	if capturedAuth != "Bearer propagated-token" {
		t.Errorf("Authorization: got %q, want %q", capturedAuth, "Bearer propagated-token")
	}
}

// TestPropagateToken_DoesNotForwardWhenDisabled verifies that when propagateToken
// is false, the Authorization header is not forwarded unless listed in allowedHeaders.
func TestPropagateToken_DoesNotForwardWhenDisabled(t *testing.T) {
	t.Parallel()
	var capturedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx := a2aCtx(map[string][]string{
		"Authorization": {"Bearer propagated-token"},
	})

	rt := &headerRoundTripper{
		base:           newTestTransport(t),
		propagateToken: false,
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	resp.Body.Close()

	if capturedAuth != "" {
		t.Errorf("Authorization should not be forwarded when propagateToken=false, got %q", capturedAuth)
	}
}

func TestPropagateTokenCarriesAuthenticatedOwner(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		ctx := auth.WithUserID(a2aCtx(map[string][]string{"X-User-Id": {"untrusted"}}), "alice")
		var got string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.Header.Get("X-User-Id")
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)
		rt := &headerRoundTripper{base: newTestTransport(t), propagateToken: enabled}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := rt.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		want := ""
		if enabled {
			want = "alice"
		}
		if got != want {
			t.Fatalf("propagateToken=%v: user = %q, want %q", enabled, got, want)
		}
	}
}

// TestAllowedRequestHeaders_ReturnsNilWhenNoMatches verifies that the helper returns
// nil rather than an empty map when the allowed list has entries but none of them
// appear in the request metadata.
func TestAllowedRequestHeaders_ReturnsNilWhenNoMatches(t *testing.T) {
	t.Parallel()
	ctx := a2aCtx(map[string][]string{
		"X-Something-Else": {"value"},
	})
	got := allowedRequestHeaders(ctx, []string{"Authorization", "X-Trace-Id"})
	if got != nil {
		t.Errorf("expected nil when no allowed headers are present, got %v", got)
	}
}

// TestDynamicHeaders_OverridePropagatedAndAllowedHeaders verifies dynamic headers
// take precedence over propagated and allowed request headers.
func TestDynamicHeaders_OverridePropagatedAndAllowedHeaders(t *testing.T) {
	t.Parallel()
	var capturedAuth, capturedCustom string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedCustom = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx := a2aCtx(map[string][]string{
		"Authorization": {"Bearer incoming"},
		"X-Custom":      {"custom-from-request"},
	})

	rt := &headerRoundTripper{
		base:           newTestTransport(t),
		propagateToken: true,
		allowedHeaders: []string{"Authorization", "X-Custom"},
		headerProvider: func(context.Context) map[string]string {
			return map[string]string{
				"Authorization": "Bearer sts-exchanged",
				"X-Custom":      "custom-from-dynamic",
			}
		},
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	resp.Body.Close()

	if capturedAuth != "Bearer sts-exchanged" {
		t.Errorf("Authorization: got %q, want %q", capturedAuth, "Bearer sts-exchanged")
	}
	if capturedCustom != "custom-from-dynamic" {
		t.Errorf("X-Custom: got %q, want %q", capturedCustom, "custom-from-dynamic")
	}
}

// TestStaticHeaders_OverrideDynamic verifies static configured headers remain
// the highest-precedence source.
func TestStaticHeaders_OverrideDynamic(t *testing.T) {
	t.Parallel()
	var capturedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := &headerRoundTripper{
		base:    newTestTransport(t),
		headers: map[string]string{"Authorization": "Bearer static"},
		headerProvider: func(context.Context) map[string]string {
			return map[string]string{"Authorization": "Bearer dynamic"}
		},
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	resp.Body.Close()

	if capturedAuth != "Bearer static" {
		t.Errorf("Authorization: got %q, want %q", capturedAuth, "Bearer static")
	}
}

func TestCreateTransportPreservesFractionalTimeout(t *testing.T) {
	for _, serverType := range []string{"http", "sse"} {
		t.Run(serverType, func(t *testing.T) {
			transport, err := createTransport(t.Context(), mcpServerParams{
				ServerType:     serverType,
				Timeout:        durationpb.New(250 * time.Millisecond),
				SseReadTimeout: durationpb.New(750 * time.Millisecond),
			})
			require.NoError(t, err)
			switch tr := transport.(type) {
			case *mcpsdk.StreamableClientTransport:
				require.Equal(t, 250*time.Millisecond, tr.HTTPClient.Timeout)
			case *mcpsdk.SSEClientTransport:
				require.Equal(t, 750*time.Millisecond, tr.HTTPClient.Timeout)
			default:
				t.Fatalf("unexpected transport %T", transport)
			}
		})
	}
}

func captureSessionHeaders(t *testing.T, rt *headerRoundTripper, ctx context.Context) http.Header {
	t.Helper()
	var captured http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt.base = newTestTransport(t)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext failed: %v", err)
	}
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	resp.Body.Close()
	return captured
}

func TestSessionIDHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		rt     *headerRoundTripper
		ctx    context.Context
		header string
		want   string
	}{
		{name: "sends the session ID when configured", rt: &headerRoundTripper{sessionIDHeader: "X-Kagent-Session-Id"}, ctx: WithPublicContextID(context.Background(), "sess-1"), header: "X-Kagent-Session-Id", want: "sess-1"},
		{name: "sends nothing when not configured", rt: &headerRoundTripper{headers: map[string]string{"X-Other": "v"}}, ctx: WithPublicContextID(context.Background(), "sess-1"), header: "X-Kagent-Session-Id", want: ""},
		{name: "omits the header when the context has no public context ID", rt: &headerRoundTripper{sessionIDHeader: "X-Kagent-Session-Id"}, ctx: context.Background(), header: "X-Kagent-Session-Id", want: ""},
		{name: "omits the header when the session ID is empty", rt: &headerRoundTripper{sessionIDHeader: "X-Kagent-Session-Id"}, ctx: WithPublicContextID(context.Background(), ""), header: "X-Kagent-Session-Id", want: ""},
		{name: "overrides a forwarded request header of the same name", rt: &headerRoundTripper{sessionIDHeader: "X-Kagent-Session-Id", allowedHeaders: []string{"X-Kagent-Session-Id"}}, ctx: WithPublicContextID(a2aCtx(map[string][]string{"X-Kagent-Session-Id": {"forged"}}), "sess-1"), header: "X-Kagent-Session-Id", want: "sess-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := captureSessionHeaders(t, tc.rt, tc.ctx)
			if v := got.Get(tc.header); v != tc.want {
				t.Errorf("%s: got %q, want %q", tc.header, v, tc.want)
			}
		})
	}
}

func TestSessionIDHeader_DoesNotOverwriteAuthorization(t *testing.T) {
	t.Parallel()
	rt := &headerRoundTripper{
		sessionIDHeader: "X-Kagent-Session-Id",
		headerProvider:  func(context.Context) map[string]string { return map[string]string{"Authorization": "Bearer sts"} },
		headers:         map[string]string{"X-Tenant": "t"},
	}
	got := captureSessionHeaders(t, rt, WithPublicContextID(context.Background(), "sess-1"))
	if got.Get("Authorization") != "Bearer sts" || got.Get("X-Tenant") != "t" || got.Get("X-Kagent-Session-Id") != "sess-1" {
		t.Errorf("headers = %v", got)
	}
}

func TestCreateToolsetsSessionIDHeader(t *testing.T) {
	t.Parallel()
	const header = "X-Kagent-Session-Id"

	newFakeServer := func(t *testing.T, sse bool) (*httptest.Server, func() []string) {
		t.Helper()
		mcpServer := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "fake", Version: "0"}, nil)
		mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{Name: "echo"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
			return nil, map[string]any{}, nil
		})
		var inner http.Handler
		if sse {
			inner = mcpsdk.NewSSEHandler(func(*http.Request) *mcpsdk.Server { return mcpServer }, nil)
		} else {
			inner = mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return mcpServer }, nil)
		}
		var mu sync.Mutex
		var seen []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			seen = append(seen, r.Header.Get(header))
			mu.Unlock()
			inner.ServeHTTP(w, r)
		}))
		t.Cleanup(func() {
			srv.CloseClientConnections()
			srv.Close()
		})
		return srv, func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), seen...)
		}
	}

	build := func(t *testing.T, sse bool, url, sessionHeader string) []tool.Toolset {
		t.Helper()
		params := &configv1.ADKHTTPConnectionParams{Url: url}
		if sse {
			return CreateToolsets(t.Context(), nil, []*configv1.ADKSSEMCPServerConfig{{Params: &configv1.ADKSSEConnectionParams{Url: url}, SessionIdHeader: sessionHeader}}, nil, false, nil)
		}
		return CreateToolsets(t.Context(), []*configv1.ADKHTTPMCPServerConfig{{Params: params, SessionIdHeader: sessionHeader}}, nil, nil, false, nil)
	}

	for _, protocol := range []struct {
		name string
		sse  bool
	}{{name: "http"}, {name: "sse", sse: true}} {
		t.Run(protocol.name+" sends the session ID when configured", func(t *testing.T) {
			t.Parallel()
			srv, seen := newFakeServer(t, protocol.sse)
			sets := build(t, protocol.sse, srv.URL, header)
			if len(sets) != 1 {
				t.Fatalf("toolsets = %d, want 1", len(sets))
			}
			if _, err := sets[0].Tools(testReadonlyContext{Context: WithPublicContextID(WithDiscoveryScope(t.Context()), "test-session")}); err != nil {
				t.Fatalf("Tools() error = %v", err)
			}
			if !slices.Contains(seen(), "test-session") {
				t.Errorf("no request carried %s: test-session, saw %q", header, seen())
			}
		})
		t.Run(protocol.name+" sends no header when not configured", func(t *testing.T) {
			t.Parallel()
			srv, seen := newFakeServer(t, protocol.sse)
			sets := build(t, protocol.sse, srv.URL, "")
			if len(sets) != 1 {
				t.Fatalf("toolsets = %d, want 1", len(sets))
			}
			ctx := WithPublicContextID(WithDiscoveryScope(t.Context()), "test-session")
			if _, err := sets[0].Tools(testReadonlyContext{Context: ctx}); err != nil {
				t.Fatalf("Tools() error = %v", err)
			}
			for _, value := range seen() {
				if value != "" {
					t.Errorf("request carried %s = %q with the field unset", header, value)
				}
			}
		})
	}
}

type concurrentToolContext struct {
	adkagent.Context
	ctx       context.Context
	sessionID string
}

func (c concurrentToolContext) Deadline() (time.Time, bool)                        { return c.ctx.Deadline() }
func (c concurrentToolContext) Done() <-chan struct{}                              { return c.ctx.Done() }
func (c concurrentToolContext) Err() error                                         { return c.ctx.Err() }
func (c concurrentToolContext) Value(key any) any                                  { return c.ctx.Value(key) }
func (c concurrentToolContext) SessionID() string                                  { return c.sessionID }
func (concurrentToolContext) ToolConfirmation() *toolconfirmation.ToolConfirmation { return nil }
func (c concurrentToolContext) FunctionCallID() string                             { return "call-" + c.sessionID }

type recordedMCPRequest struct {
	method  string
	session string
	tag     string
}

func TestCreateToolsetsSessionIDHeaderConcurrentSessions(t *testing.T) {
	t.Parallel()
	const header = "X-Kagent-Session-Id"

	given := func(t *testing.T) (*httptest.Server, func() []recordedMCPRequest) {
		t.Helper()
		mcpServer := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "fake", Version: "0"}, nil)
		mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{Name: "echo"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
			return nil, map[string]any{}, nil
		})
		inner := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return mcpServer }, nil)
		var mu sync.Mutex
		var seen []recordedMCPRequest
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := recordedMCPRequest{method: r.Method, session: r.Header.Get(header)}
			if r.Method == http.MethodPost {
				body, _ := io.ReadAll(r.Body)
				r.Body = io.NopCloser(bytes.NewReader(body))
				var msg struct {
					Method string `json:"method"`
					Params struct {
						Arguments struct {
							Who string `json:"who"`
						} `json:"arguments"`
					} `json:"params"`
				}
				_ = json.Unmarshal(body, &msg)
				rec.method, rec.tag = msg.Method, msg.Params.Arguments.Who
			}
			mu.Lock()
			seen = append(seen, rec)
			mu.Unlock()
			inner.ServeHTTP(w, r)
		}))
		t.Cleanup(func() {
			srv.CloseClientConnections()
			srv.Close()
		})
		return srv, func() []recordedMCPRequest {
			mu.Lock()
			defer mu.Unlock()
			return append([]recordedMCPRequest(nil), seen...)
		}
	}

	srv, seen := given(t)
	sets := CreateToolsets(t.Context(), []*configv1.ADKHTTPMCPServerConfig{{Params: &configv1.ADKHTTPConnectionParams{Url: srv.URL}, SessionIdHeader: header}}, nil, nil, false, nil)
	require.Len(t, sets, 1)
	tools, err := sets[0].Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())})
	require.NoError(t, err)
	require.Len(t, tools, 1)
	runnable, ok := tools[0].(functionTool)
	require.True(t, ok)

	sessions := []string{"session-a", "session-b"}
	var wg sync.WaitGroup
	for _, id := range sessions {
		for range 5 {
			wg.Go(func() {
				ctx := concurrentToolContext{ctx: WithPublicContextID(t.Context(), id), sessionID: "native-" + id}
				_, runErr := runnable.Run(ctx, map[string]any{"who": id})
				if runErr != nil {
					t.Errorf("Run(%s) error = %v", id, runErr)
				}
			})
		}
	}
	wg.Wait()

	calls := 0
	for _, rec := range seen() {
		if rec.method == "tools/call" {
			calls++
			if rec.session != rec.tag {
				t.Errorf("tools/call for %q carried %s = %q", rec.tag, header, rec.session)
			}
			continue
		}
		t.Logf("setup request %s %q carried %s = %q", rec.method, rec.tag, header, rec.session)
	}
	if calls != len(sessions)*5 {
		t.Errorf("tools/call requests = %d, want %d", calls, len(sessions)*5)
	}
}
