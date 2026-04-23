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
