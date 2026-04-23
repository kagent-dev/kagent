package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWithRequestHeaders_RoundTrip(t *testing.T) {
	var capturedAuth, capturedCustom, capturedStatic string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedCustom = r.Header.Get("X-Custom")
		capturedStatic = r.Header.Get("X-Static")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Simulate incoming request headers stored in context.
	ctx := WithRequestHeaders(context.Background(), map[string]string{
		"authorization": "Bearer token123",
		"x-custom":      "custom-value",
		"x-ignored":     "should-not-appear",
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

	if !strings.EqualFold(capturedAuth, "Bearer token123") {
		t.Errorf("Authorization: got %q, want %q", capturedAuth, "Bearer token123")
	}
	if !strings.EqualFold(capturedCustom, "custom-value") {
		t.Errorf("X-Custom: got %q, want %q", capturedCustom, "custom-value")
	}
	if !strings.EqualFold(capturedStatic, "static-value") {
		t.Errorf("X-Static: got %q, want %q", capturedStatic, "static-value")
	}
}

func TestWithRequestHeaders_StaticOverridesDynamic(t *testing.T) {
	var capturedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx := WithRequestHeaders(context.Background(), map[string]string{
		"authorization": "Bearer incoming",
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

	// Static header must win.
	if capturedAuth != "Bearer static" {
		t.Errorf("Authorization: got %q, want %q", capturedAuth, "Bearer static")
	}
}

func TestWithRequestHeaders_NoIncomingHeaders(t *testing.T) {
	var capturedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// No incoming headers stored in context.
	ctx := context.Background()

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

	if capturedAuth != "" {
		t.Errorf("Authorization should be empty without incoming headers, got %q", capturedAuth)
	}
}

func TestRequestHeadersFromContext_Empty(t *testing.T) {
	headers := requestHeadersFromContext(context.Background())
	if headers != nil {
		t.Errorf("expected nil for empty context, got %v", headers)
	}
}

func TestWithRequestHeaders_RoundTrip_IgnoresNonAllowed(t *testing.T) {
	var capturedIgnored string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedIgnored = r.Header.Get("X-Ignored")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx := WithRequestHeaders(context.Background(), map[string]string{
		"x-ignored": "should-not-appear",
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
