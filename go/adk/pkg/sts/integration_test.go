package sts

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewSTSIntegrationDefaultSubjectToken(t *testing.T) {
	t.Parallel()
	defaultConfig := DefaultSTSConfig("http://example.com/.well-known")
	i, err := NewSTSIntegration(
		defaultConfig.WellKnownURI,
		"",
		nil,
		nil,
		defaultConfig.Timeout,
		*defaultConfig.VerifySSL,
		defaultConfig.UseIssuerHost,
	)
	if err != nil {
		t.Fatalf("NewSTSIntegration() error = %v", err)
	}
	got := i.GetSubjectToken("bearer-token")
	if got != "bearer-token" {
		t.Fatalf("GetSubjectToken() = %q, want %q", got, "bearer-token")
	}
}

func TestSTSIntegrationDynamicActorTokenFetch(t *testing.T) {
	t.Parallel()
	calls := 0
	i, err := NewSTSIntegration(
		"http://example.com/.well-known",
		"",
		func(context.Context) (string, error) {
			calls++
			return "dynamic-actor", nil
		},
		nil,
		5,
		true,
		false,
	)
	if err != nil {
		t.Fatalf("NewSTSIntegration() error = %v", err)
	}
	got, err := i.getActorToken(context.Background())
	if err != nil {
		t.Fatalf("getActorToken() error = %v", err)
	}
	if got != "dynamic-actor" {
		t.Fatalf("getActorToken() = %q, want %q", got, "dynamic-actor")
	}
	if calls != 1 {
		t.Fatalf("fetchActorToken calls = %d, want 1", calls)
	}
}

func TestSTSIntegrationStaticActorTokenCached(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "actor-token")
	if err := os.WriteFile(tokenPath, []byte("static-token"), 0o600); err != nil {
		t.Fatalf("failed to write token file: %v", err)
	}

	i, err := NewSTSIntegration("http://example.com/.well-known", tokenPath, nil, nil, 5, true, false)
	if err != nil {
		t.Fatalf("NewSTSIntegration() error = %v", err)
	}

	got1, err := i.getActorToken(context.Background())
	if err != nil {
		t.Fatalf("first getActorToken() error = %v", err)
	}
	if got1 != "static-token" {
		t.Fatalf("first getActorToken() = %q, want %q", got1, "static-token")
	}

	// Change underlying file; cached static token should still be returned.
	if err := os.WriteFile(tokenPath, []byte("new-token"), 0o600); err != nil {
		t.Fatalf("failed to update token file: %v", err)
	}
	got2, err := i.getActorToken(context.Background())
	if err != nil {
		t.Fatalf("second getActorToken() error = %v", err)
	}
	if got2 != "static-token" {
		t.Fatalf("second getActorToken() = %q, want cached %q", got2, "static-token")
	}
}

func TestSTSIntegrationStaticActorTokenErrorPropagates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "empty-token")
	if err := os.WriteFile(tokenPath, []byte(" \n\t "), 0o600); err != nil {
		t.Fatalf("failed to write token file: %v", err)
	}

	i, err := NewSTSIntegration("http://example.com/.well-known", tokenPath, nil, nil, 5, true, false)
	if err != nil {
		t.Fatalf("NewSTSIntegration() error = %v", err)
	}

	_, err = i.actorTokenForExchange(context.Background())
	if err == nil {
		t.Fatalf("actorTokenForExchange() error = nil, want non-nil")
	}
}
