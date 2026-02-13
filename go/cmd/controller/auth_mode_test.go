package main

import (
	"os"
	"testing"

	authimpl "github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/pkg/auth"
)

func TestGetAuthenticator(t *testing.T) {
	tests := []struct {
		name     string
		authMode string
		wantType string
	}{
		{
			name:     "defaults to UnsecureAuthenticator",
			authMode: "",
			wantType: "*auth.UnsecureAuthenticator",
		},
		{
			name:     "unsecure mode uses UnsecureAuthenticator",
			authMode: "unsecure",
			wantType: "*auth.UnsecureAuthenticator",
		},
		{
			name:     "proxy mode uses ProxyAuthenticator",
			authMode: "proxy",
			wantType: "*auth.ProxyAuthenticator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.authMode != "" {
				os.Setenv("AUTH_MODE", tt.authMode)
				defer os.Unsetenv("AUTH_MODE")
			} else {
				os.Unsetenv("AUTH_MODE")
			}

			authenticator := getAuthenticator()
			gotType := getTypeName(authenticator)
			if gotType != tt.wantType {
				t.Errorf("getAuthenticator() = %s, want %s", gotType, tt.wantType)
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
