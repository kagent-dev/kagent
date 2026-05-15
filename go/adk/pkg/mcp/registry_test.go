package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/a2asrv"
)

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
		base:           http.DefaultTransport,
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
		base:           http.DefaultTransport,
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
		base:           http.DefaultTransport,
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
		base:           http.DefaultTransport,
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
		base:           http.DefaultTransport,
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
		base:           http.DefaultTransport,
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
