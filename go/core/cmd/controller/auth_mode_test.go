package main

import (
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authenticator := getAuthenticator(tt.authCfg)
			gotType := getTypeName(authenticator)
			if gotType != tt.wantType {
				t.Errorf("getAuthenticator() = %s, want %s", gotType, tt.wantType)
			}
		})
	}
}

func TestGetAuthenticatorPanicsOnExternalBearerPlaceholder(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for external-bearer placeholder, got none")
		}
		if got, want := r.(string), "auth mode external-bearer is recognized but not implemented in this slice"; got != want {
			t.Fatalf("panic = %q, want %q", got, want)
		}
	}()
	getAuthenticator(app.AuthConfig{Mode: "external-bearer"})
}

func TestGetAuthenticatorPanicsOnUnknownMode(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for unknown auth mode, got none")
		}
		if got, want := r.(string), "unknown auth mode: proxy (valid modes: unsecure, trusted-proxy, external-bearer)"; got != want {
			t.Fatalf("panic = %q, want %q", got, want)
		}
	}()
	getAuthenticator(app.AuthConfig{Mode: "proxy"})
}

func getTypeName(v auth.AuthProvider) string {
	switch v.(type) {
	case *authimpl.UnsecureAuthenticator:
		return "*auth.UnsecureAuthenticator"
	case *authimpl.ProxyAuthenticator:
		return "*auth.ProxyAuthenticator"
	default:
		return "unknown"
	}
}
