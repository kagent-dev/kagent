package main

import (
	"strings"
	"testing"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestGetAuthenticator(t *testing.T) {
	tests := []struct {
		name     string
		authCfg  struct{ Mode, UserIDClaim string }
		wantType string
	}{
		{
			name:     "unsecure mode uses UnsecureAuthenticator",
			authCfg:  struct{ Mode, UserIDClaim string }{"unsecure", ""},
			wantType: "*auth.UnsecureAuthenticator",
		},
		{
			name:     "trusted-proxy mode uses ProxyAuthenticator",
			authCfg:  struct{ Mode, UserIDClaim string }{"trusted-proxy", ""},
			wantType: "*auth.ProxyAuthenticator",
		},
		{
			name:     "trusted-proxy mode with custom claim",
			authCfg:  struct{ Mode, UserIDClaim string }{"trusted-proxy", "user_id"},
			wantType: "*auth.ProxyAuthenticator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authenticator, err := getAuthenticator(tt.authCfg)
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
	authenticator, err := getAuthenticator(struct{ Mode, UserIDClaim string }{invalidMode, ""})
	if err == nil {
		t.Fatal("expected error for unknown auth mode, got nil")
	}
	if authenticator != nil {
		t.Errorf("expected nil authenticator on error, got %T", authenticator)
	}
	// The error message must surface the invalid mode and the supported values
	// so misconfigured deployments get an actionable message rather than just a
	// generic failure.
	msg := err.Error()
	if !strings.Contains(msg, invalidMode) {
		t.Errorf("error message %q does not include the invalid mode %q", msg, invalidMode)
	}
	for _, valid := range []string{"unsecure", "trusted-proxy"} {
		if !strings.Contains(msg, valid) {
			t.Errorf("error message %q does not list supported mode %q", msg, valid)
		}
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
