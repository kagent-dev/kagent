package models

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testRoundTripper struct {
	lastRequest *http.Request
}

func (t *testRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	t.lastRequest = req
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       http.NoBody,
		Header:     make(http.Header),
	}, nil
}

func TestPassthroughAuthTransport_SetsHeaderFromContext(t *testing.T) {
	base := &testRoundTripper{}
	transport := &passthroughAuthTransport{base: base}

	token := "test-token-123"
	ctx := context.WithValue(context.Background(), BearerTokenKey, token)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req = req.WithContext(ctx)

	_, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that the Authorization header was set
	authHeader := base.lastRequest.Header.Get("Authorization")
	expected := "Bearer " + token
	if authHeader != expected {
		t.Errorf("expected Authorization header to be %q, got %q", expected, authHeader)
	}
}

func TestPassthroughAuthTransport_NoTokenInContext_NoOp(t *testing.T) {
	base := &testRoundTripper{}
	transport := &passthroughAuthTransport{base: base}

	// No token in context
	ctx := context.Background()

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req = req.WithContext(ctx)

	// Set an existing Authorization header to verify it wasn't modified
	req.Header.Set("Authorization", "existing-auth")

	_, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that the Authorization header was not modified (passthrough no-ops)
	authHeader := base.lastRequest.Header.Get("Authorization")
	// Since there's no token in context, the passthrough should not modify the request
	// The request is cloned, but the passthrough no-ops when no token
	// So the original header should be preserved (or if no header, no header)
	if authHeader != "existing-auth" {
		t.Errorf("expected Authorization header to remain %q, got %q", "existing-auth", authHeader)
	}
}

func TestPassthroughAuthTransport_EmptyToken_NoOp(t *testing.T) {
	base := &testRoundTripper{}
	transport := &passthroughAuthTransport{base: base}

	// Empty token in context
	ctx := context.WithValue(context.Background(), BearerTokenKey, "")

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req = req.WithContext(ctx)

	_, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that no Authorization header was set (empty token means no-op)
	authHeader := base.lastRequest.Header.Get("Authorization")
	if authHeader != "" {
		t.Errorf("expected no Authorization header for empty token, got %q", authHeader)
	}
}

func TestPassthroughAuthTransport_ClonesRequest(t *testing.T) {
	base := &testRoundTripper{}
	transport := &passthroughAuthTransport{base: base}

	token := "test-token"
	ctx := context.WithValue(context.Background(), BearerTokenKey, token)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req = req.WithContext(ctx)

	originalAuth := req.Header.Get("Authorization")

	_, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the original request was not modified (it was cloned)
	if req.Header.Get("Authorization") != originalAuth {
		t.Error("original request should not be modified by passthroughAuthTransport")
	}
}

func TestPassthroughAuthTransport_PreservesOtherHeaders(t *testing.T) {
	base := &testRoundTripper{}
	transport := &passthroughAuthTransport{base: base}

	token := "test-token"
	ctx := context.WithValue(context.Background(), BearerTokenKey, token)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req = req.WithContext(ctx)
	req.Header.Set("X-Custom-Header", "custom-value")
	req.Header.Set("Content-Type", "application/json")

	_, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify custom headers are preserved
	if base.lastRequest.Header.Get("X-Custom-Header") != "custom-value" {
		t.Error("custom header should be preserved")
	}
	if base.lastRequest.Header.Get("Content-Type") != "application/json" {
		t.Error("Content-Type header should be preserved")
	}

	// Verify Authorization was added
	if base.lastRequest.Header.Get("Authorization") != "Bearer "+token {
		t.Error("Authorization header should be set")
	}
}

func TestPassthroughAuthTransport_WrongContextKeyType(t *testing.T) {
	base := &testRoundTripper{}
	transport := &passthroughAuthTransport{base: base}

	// Use a different type for the context key
	type wrongKey struct{}
	ctx := context.WithValue(context.Background(), wrongKey{}, "some-token")

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req = req.WithContext(ctx)

	_, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Authorization header should not be set (wrong key type)
	authHeader := base.lastRequest.Header.Get("Authorization")
	if authHeader != "" {
		t.Errorf("expected no Authorization header with wrong key type, got %q", authHeader)
	}
}

func TestPassthroughAuthTransport_NonStringToken(t *testing.T) {
	base := &testRoundTripper{}
	transport := &passthroughAuthTransport{base: base}

	// Store non-string type in context
	ctx := context.WithValue(context.Background(), BearerTokenKey, 12345)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req = req.WithContext(ctx)

	_, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Authorization header should not be set (non-string token)
	authHeader := base.lastRequest.Header.Get("Authorization")
	if authHeader != "" {
		t.Errorf("expected no Authorization header with non-string token, got %q", authHeader)
	}
}
