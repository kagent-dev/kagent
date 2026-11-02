package main

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestCheckConfigAllowsUnsignedPush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "controller.yaml")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "database-url"), []byte("postgres://localhost/kagent"), 0o600))
	const database = "database: {owner: {urlFile: database-url, role: kagent_owner}, readWrite: {urlFile: database-url, role: kagent_readwrite}}\n"
	require.NoError(t, os.WriteFile(path, []byte(database), 0o600))
	require.NoError(t, run(path, true, ""))
	require.NoError(t, os.WriteFile(path, []byte(database+"push: {signingPrivateKeyFile: missing.pem}\n"), 0o600))
	require.ErrorContains(t, run(path, true, ""), filepath.Join(dir, "missing.pem"))
}

func TestCheckConfigSkipsOwnerFileWhenSkippingMigrations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "controller.yaml")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readwrite-url"), []byte("postgres://localhost/kagent"), 0o600))
	const database = "database: {owner: {urlFile: missing-owner-url, role: kagent_owner}, readWrite: {urlFile: readwrite-url, role: kagent_readwrite}"
	require.NoError(t, os.WriteFile(path, []byte(database+", skipMigrations: true}\n"), 0o600))
	require.NoError(t, run(path, true, ""))
	require.NoError(t, os.WriteFile(path, []byte(database+"}\n"), 0o600))
	require.ErrorContains(t, run(path, true, ""), "missing-owner-url")
}

func TestControllerAuthenticator(t *testing.T) {
	for _, tt := range []struct {
		name, mode, claim, payload, wantUser string
		wantConfigError, wantAuthError       bool
	}{
		{name: "default", mode: "insecure", wantUser: "admin@kagent.dev"},
		{name: "subject", mode: "trusted-proxy", payload: `{"sub":"subject","email":"user@example.com"}`, wantUser: "subject"},
		{name: "email", mode: "trusted-proxy", claim: "email", payload: `{"sub":"subject","email":"user@example.com"}`, wantUser: "user@example.com"},
		{name: "fallback", mode: "trusted-proxy", claim: "email", payload: `{"sub":"subject"}`, wantUser: "subject"},
		{name: "empty custom claim", mode: "trusted-proxy", claim: "email", payload: `{"sub":"subject","email":""}`, wantUser: "subject"},
		{name: "non-string custom claim", mode: "trusted-proxy", claim: "email", payload: `{"sub":"subject","email":42}`, wantUser: "subject"},
		{name: "no identity", mode: "trusted-proxy", claim: "email", payload: `{}`, wantAuthError: true},
		{name: "non-string subject", mode: "trusted-proxy", payload: `{"sub":42}`, wantAuthError: true},
		{name: "missing credentials", mode: "trusted-proxy", wantAuthError: true},
		{name: "malformed credentials", mode: "trusted-proxy", payload: "invalid", wantAuthError: true},
		{name: "unsupported mode", mode: "oidc", wantConfigError: true},
		{name: "empty mode", mode: "", wantConfigError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			authenticator, err := controllerAuthenticator(tt.mode, tt.claim)
			if tt.wantConfigError {
				require.ErrorContains(t, err, "server.auth.mode")
				require.Nil(t, authenticator)
				return
			}
			require.NoError(t, err)
			headers := http.Header{}
			if tt.payload != "" {
				header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
				headers.Set("Authorization", "Bearer "+header+"."+base64.RawURLEncoding.EncodeToString([]byte(tt.payload))+"."+base64.RawURLEncoding.EncodeToString([]byte("signature")))
			}
			session, err := authenticator.Authenticate(context.Background(), headers, url.Values{})
			if tt.wantAuthError {
				require.ErrorIs(t, err, authimpl.ErrUnauthenticated)
				return
			}
			require.NoError(t, err)
			if tt.mode == "trusted-proxy" {
				tt.wantUser = auth.PrincipalID("trusted-proxy:", tt.wantUser)
			}
			require.Equal(t, tt.wantUser, session.Principal().ID)
		})
	}
}
