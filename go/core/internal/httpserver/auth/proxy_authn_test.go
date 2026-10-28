package auth_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// createTestJWT creates a minimal JWT token with the given claims
func createTestJWT(claims map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	signature := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))
	return header + "." + payloadB64 + "." + signature
}

func TestProxyAuthenticator_Authenticate(t *testing.T) {
	tests := []struct {
		name         string
		claims       map[string]any
		userIDClaim  string
		wantUserID   string
		wantClaims   map[string]any
		wantErr      bool
		noToken      bool
		invalidToken bool
	}{
		{
			name: "extracts standard claims and passes through raw claims",
			claims: map[string]any{
				"sub":    "user123",
				"email":  "user@example.com",
				"name":   "Test User",
				"groups": []any{"admin", "developers"},
			},
			wantUserID: "user123",
			wantClaims: map[string]any{
				"sub":   "user123",
				"email": "user@example.com",
				"name":  "Test User",
			},
			wantErr: false,
		},
		{
			name: "uses custom user ID claim",
			claims: map[string]any{
				"user_id": "custom-user-123",
				"email":   "custom@example.com",
				"sub":     "fallback-sub",
			},
			userIDClaim: "user_id",
			wantUserID:  "custom-user-123",
			wantClaims: map[string]any{
				"user_id": "custom-user-123",
				"email":   "custom@example.com",
				"sub":     "fallback-sub",
			},
			wantErr: false,
		},
		{
			name: "falls back to sub when custom claim is missing",
			claims: map[string]any{
				"sub":   "fallback-user",
				"email": "user@example.com",
			},
			userIDClaim: "user_id",
			wantUserID:  "fallback-user",
			wantErr:     false,
		},
		{
			name:    "returns error when Authorization header missing",
			noToken: true,
			wantErr: true,
		},
		{
			name:         "returns error for invalid JWT format",
			invalidToken: true,
			wantErr:      true,
		},
		{
			name: "handles minimal claims",
			claims: map[string]any{
				"sub": "user123",
			},
			wantUserID: "user123",
			wantClaims: map[string]any{
				"sub": "user123",
			},
			wantErr: false,
		},
		{
			name: "returns error when JWT has empty sub claim",
			claims: map[string]any{
				"email": "user@example.com",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := authimpl.NewProxyAuthenticator(tt.userIDClaim)

			headers := http.Header{}
			if !tt.noToken {
				if tt.invalidToken {
					headers.Set("Authorization", "Bearer invalid-token")
				} else {
					token := createTestJWT(tt.claims)
					headers.Set("Authorization", "Bearer "+token)
				}
			}

			session, err := auth.Authenticate(context.Background(), headers, url.Values{})

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			principal := session.Principal()
			if principal.ID != pkgauth.PrincipalID("trusted-proxy:", tt.wantUserID) {
				t.Errorf("User.ID = %q, want %q", principal.ID, tt.wantUserID)
			}

			// Verify raw claims are passed through
			if tt.wantClaims != nil {
				if principal.Claims == nil {
					t.Fatal("expected Claims to be non-nil")
				}
				for k, wantV := range tt.wantClaims {
					gotV, ok := principal.Claims[k]
					if !ok {
						t.Errorf("Claims[%q] missing", k)
						continue
					}
					// Compare as strings for simple values
					if wantStr, ok := wantV.(string); ok {
						if gotStr, ok := gotV.(string); !ok || gotStr != wantStr {
							t.Errorf("Claims[%q] = %v, want %q", k, gotV, wantStr)
						}
					}
				}
			}
		})
	}
}

func TestProxyAuthenticator_AgentCalls(t *testing.T) {
	tests := []struct {
		name        string
		headers     map[string]string
		queryParams map[string]string
		wantUserID  string
		wantErr     bool
	}{
		{
			name: "agent with SA Bearer token and X-User-Id header ignores header identity",
			headers: map[string]string{
				"Authorization": "Bearer " + createTestJWT(map[string]any{"sub": "system:serviceaccount:kagent:test-agent"}),
				"X-Agent-Name":  "kagent/test-agent",
				"X-User-Id":     "user@example.com",
			},
			wantUserID: "system:serviceaccount:kagent:test-agent",
		},
		{
			name: "agent with SA Bearer token and user_id query param ignores query identity",
			headers: map[string]string{
				"Authorization": "Bearer " + createTestJWT(map[string]any{"sub": "system:serviceaccount:kagent:test-agent"}),
				"X-Agent-Name":  "kagent/test-agent",
			},
			queryParams: map[string]string{
				"user_id": "user@example.com",
			},
			wantUserID: "system:serviceaccount:kagent:test-agent",
		},
		{
			name: "agent with no X-User-Id falls back to SA sub claim",
			headers: map[string]string{
				"Authorization": "Bearer " + createTestJWT(map[string]any{"sub": "system:serviceaccount:kagent:test-agent"}),
				"X-Agent-Name":  "kagent/test-agent",
			},
			wantUserID: "system:serviceaccount:kagent:test-agent",
		},
		// Error cases.
		{
			name: "agent without Bearer token is rejected",
			headers: map[string]string{
				"X-Agent-Name": "kagent/test-agent",
				"X-User-Id":    "user@example.com",
			},
			wantErr: true,
		},
		{
			name:    "no token and no X-Agent-Name is rejected",
			wantErr: true,
		},
		{
			name: "user_id without X-Agent-Name is rejected",
			queryParams: map[string]string{
				"user_id": "user@example.com",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := authimpl.NewProxyAuthenticator("")

			headers := http.Header{}
			for k, v := range tt.headers {
				headers.Set(k, v)
			}

			query := url.Values{}
			for k, v := range tt.queryParams {
				query.Set(k, v)
			}

			session, err := auth.Authenticate(context.Background(), headers, query)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			principal := session.Principal()
			if principal.ID != pkgauth.PrincipalID("trusted-proxy:", tt.wantUserID) {
				t.Errorf("User.ID = %q, want %q", principal.ID, tt.wantUserID)
			}
		})
	}
}

func TestProxyAuthenticator_UpstreamAuth(t *testing.T) {
	auth := authimpl.NewProxyAuthenticator("")

	claims := map[string]any{
		"sub":   "user123",
		"email": "user@example.com",
	}
	token := createTestJWT(claims)
	authHeader := "Bearer " + token

	headers := http.Header{}
	headers.Set("Authorization", authHeader)

	session, err := auth.Authenticate(context.Background(), headers, url.Values{})
	if err != nil {
		t.Fatalf("failed to authenticate: %v", err)
	}

	// Create a new request to test UpstreamAuth
	req, _ := http.NewRequest("GET", "http://example.com", nil)

	err = auth.UpstreamAuth(req, session, "")
	if err != nil {
		t.Errorf("UpstreamAuth returned error: %v", err)
	}

	// Verify the Authorization header was forwarded
	if got := req.Header.Get("Authorization"); got != authHeader {
		t.Errorf("Authorization header = %q, want %q", got, authHeader)
	}

	// Verify X-User-Id is forwarded so downstream A2A runtimes receive the real user identity
	if got := req.Header.Get("X-User-Id"); got != pkgauth.PrincipalID("trusted-proxy:", "user123") {
		t.Errorf("X-User-Id header = %q, want %q", got, "user123")
	}
}

func TestProxyRejectsExpiredAndAmbiguousCredentials(t *testing.T) {
	provider := authimpl.NewProxyAuthenticator("")
	expired := createTestJWT(map[string]any{"sub": "alice", "exp": time.Now().Add(-time.Minute).Unix()})
	_, err := provider.Authenticate(t.Context(), http.Header{"Authorization": {"Bearer " + expired}}, nil)
	if err == nil {
		t.Fatal("expired token accepted")
	}
	token := createTestJWT(map[string]any{"sub": "alice"})
	_, err = provider.Authenticate(t.Context(), http.Header{"Authorization": {"Bearer " + token, "Bearer " + token}}, nil)
	if err == nil {
		t.Fatal("multiple credentials accepted")
	}
}

func TestProxyRejectsMalformedJWTHeaders(t *testing.T) {
	_, payload, _ := strings.Cut(createTestJWT(map[string]any{"sub": "alice"}), ".")
	for _, header := range []string{"not-json", `{}`, `{"alg":"unknown"}`} {
		t.Run(header, func(t *testing.T) {
			token := base64.RawURLEncoding.EncodeToString([]byte(header)) + "." + payload
			session, err := authimpl.NewProxyAuthenticator("").Authenticate(t.Context(), http.Header{"Authorization": {"Bearer " + token}}, nil)
			if err == nil || session != nil {
				t.Fatal("malformed JWT header accepted")
			}
		})
	}
}
