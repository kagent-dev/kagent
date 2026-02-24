package main

import (
	"testing"

	authimpl "github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/pkg/auth"
)

func TestGetAuthenticator(t *testing.T) {
	tests := []struct {
		name     string
		authCfg  struct{ Mode, UserIDClaim string }
		wantType string
	}{
		{
			name:     "defaults to UnsecureAuthenticator",
			authCfg:  struct{ Mode, UserIDClaim string }{"", ""},
			wantType: "*auth.UnsecureAuthenticator",
		},
		{
			name:     "unsecure mode uses UnsecureAuthenticator",
			authCfg:  struct{ Mode, UserIDClaim string }{"unsecure", ""},
			wantType: "*auth.UnsecureAuthenticator",
		},
		{
			name:     "proxy mode uses ProxyAuthenticator",
			authCfg:  struct{ Mode, UserIDClaim string }{"proxy", ""},
			wantType: "*auth.ProxyAuthenticator",
		},
		{
			name:     "proxy mode with custom claim",
			authCfg:  struct{ Mode, UserIDClaim string }{"proxy", "user_id"},
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
