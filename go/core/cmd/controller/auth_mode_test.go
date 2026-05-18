package main

import (
	"strings"
	"testing"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/app"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestGetAuthenticator(t *testing.T) {
	tests := []struct {
		name     string
		authCfg  app.AuthConfig
		wantType string
	}{
		{
			name:     "unsecure mode uses UnsecureAuthenticator",
			authCfg:  app.AuthConfig{Mode: "unsecure"},
			wantType: "*auth.UnsecureAuthenticator",
		},
		{
			name:     "trusted-proxy mode uses ProxyAuthenticator",
			authCfg:  app.AuthConfig{Mode: "trusted-proxy"},
			wantType: "*auth.ProxyAuthenticator",
		},
		{
			name:     "trusted-proxy mode with custom claim",
			authCfg:  app.AuthConfig{Mode: "trusted-proxy", UserIDClaim: "user_id"},
			wantType: "*auth.ProxyAuthenticator",
		},
		{
			name: "external-bearer mode uses ExternalBearerAuthenticator",
			authCfg: app.AuthConfig{
				Mode: "external-bearer",
				ExternalBearer: app.ExternalBearerAuthConfig{
					URL:                               "http://localhost/introspect",
					AllowUnauthenticatedIntrospection: true,
				},
			},
			wantType: "*auth.ExternalBearerAuthenticator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authenticator, err := getAuthenticator(tt.authCfg)
			if err != nil {
				t.Fatalf("getAuthenticator() error = %v", err)
			}
			gotType := getTypeName(authenticator)
			if gotType != tt.wantType {
				t.Errorf("getAuthenticator() = %s, want %s", gotType, tt.wantType)
			}
		})
	}
}

func TestGetAuthenticatorExternalBearerConfigErrors(t *testing.T) {
	tests := []struct {
		name    string
		authCfg app.AuthConfig
		wantErr string
	}{
		{
			name:    "missing URL",
			authCfg: app.AuthConfig{Mode: "external-bearer"},
			wantErr: "AUTH_EXTERNAL_BEARER_URL",
		},
		{
			name: "invalid endpoint auth config",
			authCfg: app.AuthConfig{
				Mode: "external-bearer",
				ExternalBearer: app.ExternalBearerAuthConfig{
					URL:                     "http://localhost/introspect",
					ValidationAuthorization: "Bearer validation-token",
					ClientID:                "client",
					ClientSecret:            "secret",
				},
			},
			wantErr: "cannot set ValidationAuthorization",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := getAuthenticator(tt.authCfg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestGetAuthenticatorReturnsErrorOnUnknownMode(t *testing.T) {
	_, err := getAuthenticator(app.AuthConfig{Mode: "proxy"})
	if err == nil {
		t.Fatal("expected error for unknown auth mode, got nil")
	}
	if got, want := err.Error(), "unknown auth mode: proxy (valid modes: unsecure, trusted-proxy, external-bearer)"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func getTypeName(v auth.AuthProvider) string {
	switch v.(type) {
	case *authimpl.UnsecureAuthenticator:
		return "*auth.UnsecureAuthenticator"
	case *authimpl.ProxyAuthenticator:
		return "*auth.ProxyAuthenticator"
	case *authimpl.ExternalBearerAuthenticator:
		return "*auth.ExternalBearerAuthenticator"
	default:
		return "unknown"
	}
}
