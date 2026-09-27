package main

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"testing"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/require"
)

func TestControllerAuthenticator(t *testing.T) {
	for _, tt := range []struct {
		name, mode, claim, payload, wantUser string
		wantConfigError, wantAuthError       bool
	}{
		{name: "default", mode: env.AuthMode.DefaultValue(), wantUser: "admin@kagent.dev"},
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
				require.ErrorContains(t, err, env.AuthMode.Name())
				require.Nil(t, authenticator)
				return
			}
			require.NoError(t, err)
			headers := http.Header{}
			if tt.payload != "" {
				headers.Set("Authorization", "Bearer e30."+base64.RawURLEncoding.EncodeToString([]byte(tt.payload))+".signature")
			}
			session, err := authenticator.Authenticate(context.Background(), headers, url.Values{})
			if tt.wantAuthError {
				require.ErrorIs(t, err, authimpl.ErrUnauthenticated)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantUser, session.Principal().User.ID)
		})
	}
}
