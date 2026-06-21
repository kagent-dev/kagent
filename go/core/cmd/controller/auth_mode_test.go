package main

import (
	"context"
	"strings"
	"testing"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type testAuthCfg struct {
	Mode        string
	UserIDClaim string
	DexIssuer   string
	DexClientID string
}

func TestGetAuthenticator(t *testing.T) {
	tests := []struct {
		name     string
		authCfg  testAuthCfg
		wantType string
	}{
		{
			name:     "unsecure mode uses UnsecureAuthenticator",
			authCfg:  testAuthCfg{Mode: "unsecure"},
			wantType: "*auth.UnsecureAuthenticator",
		},
		{
			name:     "trusted-proxy mode uses ProxyAuthenticator",
			authCfg:  testAuthCfg{Mode: "trusted-proxy"},
			wantType: "*auth.ProxyAuthenticator",
		},
		{
			name:     "trusted-proxy mode with custom claim",
			authCfg:  testAuthCfg{Mode: "trusted-proxy", UserIDClaim: "user_id"},
			wantType: "*auth.ProxyAuthenticator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authenticator, err := getAuthenticator(context.Background(), struct {
				Mode        string
				UserIDClaim string
				DexIssuer   string
				DexClientID string
			}{
				Mode:        tt.authCfg.Mode,
				UserIDClaim: tt.authCfg.UserIDClaim,
				DexIssuer:   tt.authCfg.DexIssuer,
				DexClientID: tt.authCfg.DexClientID,
			})
			if err != nil {
				t.Fatalf("getAuthenticator() unexpected error: %v", err)
			}
			gotType := getTypeName(authenticator)
			if gotType != tt.wantType {
				t.Errorf("getAuthenticator() = %s, want %s", gotType, tt.wantType)
			}
		})
	}
}

func TestGetAuthenticatorErrorsOnUnknownMode(t *testing.T) {
	const invalidMode = "proxy"
	authenticator, err := getAuthenticator(context.Background(), struct {
		Mode        string
		UserIDClaim string
		DexIssuer   string
		DexClientID string
	}{Mode: invalidMode})
	if err == nil {
		t.Fatal("expected error for unknown auth mode, got nil")
	}
	if authenticator != nil {
		t.Errorf("expected nil authenticator on error, got %T", authenticator)
	}
	// The error message must surface the invalid mode and the supported values.
	msg := err.Error()
	if !strings.Contains(msg, invalidMode) {
		t.Errorf("error message %q does not include the invalid mode %q", msg, invalidMode)
	}
	for _, valid := range []string{"unsecure", "trusted-proxy", "dex-oidc"} {
		if !strings.Contains(msg, valid) {
			t.Errorf("error message %q does not list supported mode %q", msg, valid)
		}
	}
}

func TestGetAuthenticatorDexOIDCValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     testAuthCfg
		wantErr string
	}{
		{
			name:    "missing issuer",
			cfg:     testAuthCfg{Mode: "dex-oidc", DexClientID: "kagent-a2a"},
			wantErr: "--auth-dex-issuer",
		},
		{
			name:    "missing client ID",
			cfg:     testAuthCfg{Mode: "dex-oidc", DexIssuer: "https://dex.example.com"},
			wantErr: "--auth-dex-client-id",
		},
		{
			name:    "unreachable issuer",
			cfg:     testAuthCfg{Mode: "dex-oidc", DexIssuer: "http://127.0.0.1:1", DexClientID: "kagent-a2a"},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := getAuthenticator(context.Background(), struct {
				Mode        string
				UserIDClaim string
				DexIssuer   string
				DexClientID string
			}{
				Mode:        tt.cfg.Mode,
				UserIDClaim: tt.cfg.UserIDClaim,
				DexIssuer:   tt.cfg.DexIssuer,
				DexClientID: tt.cfg.DexClientID,
			})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q should mention %q", err.Error(), tt.wantErr)
			}
		})
	}
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
