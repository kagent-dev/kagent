package mcp

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/a2asrv"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"
)

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
// Keys are stored case-insensitively by NewRequestMeta, matching the behaviour
// of a real A2A server.
func a2aCtx(headers map[string][]string) context.Context {
	meta := a2asrv.NewRequestMeta(headers)
	ctx, _ := a2asrv.WithCallContext(context.Background(), meta)
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
		&mcpAppToolset{inner: inner, appToolNames: map[string]bool{"show_board": true}},
		&mcpAppToolset{inner: inner, appToolNames: map[string]bool{"refresh": true}},
		inner,
	}

	got := MCPAppToolNamesFromToolsets(toolsets)
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
