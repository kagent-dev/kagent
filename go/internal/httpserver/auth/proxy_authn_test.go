package auth_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	authimpl "github.com/kagent-dev/kagent/go/internal/httpserver/auth"
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
			if principal.User.ID != tt.wantUserID {
				t.Errorf("User.ID = %q, want %q", principal.User.ID, tt.wantUserID)
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

func TestProxyAuthenticator_JWTWithAgentHeader(t *testing.T) {
	tests := []struct {
		name        string
		claims      map[string]any
		agentName   string
		wantUserID  string
		wantAgentID string
	}{
		{
			name: "extracts agent identity from header when JWT is present",
			claims: map[string]any{
				"sub": "system:serviceaccount:kagent:kebab-agent",
				"iss": "https://kubernetes.default.svc.cluster.local",
				"aud": []any{"kagent"},
			},
			agentName:   "kagent__NS__kebab_agent",
			wantUserID:  "system:serviceaccount:kagent:kebab-agent",
			wantAgentID: "kagent__NS__kebab_agent",
		},
		{
			name: "works with OIDC JWT and agent header",
			claims: map[string]any{
				"sub":   "user123",
				"email": "user@example.com",
			},
			agentName:   "kagent__NS__my_agent",
			wantUserID:  "user123",
			wantAgentID: "kagent__NS__my_agent",
		},
		{
			name: "handles JWT without agent header",
			claims: map[string]any{
				"sub": "user123",
			},
			agentName:   "",
			wantUserID:  "user123",
			wantAgentID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := authimpl.NewProxyAuthenticator("")

			headers := http.Header{}
			token := createTestJWT(tt.claims)
			headers.Set("Authorization", "Bearer "+token)
			if tt.agentName != "" {
				headers.Set("X-Agent-Name", tt.agentName)
			}

			session, err := auth.Authenticate(context.Background(), headers, url.Values{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			principal := session.Principal()
			if principal.User.ID != tt.wantUserID {
				t.Errorf("User.ID = %q, want %q", principal.User.ID, tt.wantUserID)
			}
			if principal.Agent.ID != tt.wantAgentID {
				t.Errorf("Agent.ID = %q, want %q", principal.Agent.ID, tt.wantAgentID)
			}
		})
	}
}

func TestProxyAuthenticator_ServiceAccountFallback(t *testing.T) {
	tests := []struct {
		name        string
		headers     map[string]string
		queryParams map[string]string
		wantUserID  string
		wantAgentID string
		wantErr     bool
	}{
		{
			name: "authenticates via user_id query param with agent name",
			queryParams: map[string]string{
				"user_id": "system:serviceaccount:kagent:kebab-agent",
			},
			headers: map[string]string{
				"X-Agent-Name": "kagent/kebab-agent",
			},
			wantUserID:  "system:serviceaccount:kagent:kebab-agent",
			wantAgentID: "kagent/kebab-agent",
			wantErr:     false,
		},
		{
			name: "authenticates via X-User-Id header with agent name",
			headers: map[string]string{
				"X-User-Id":    "system:serviceaccount:kagent:test-agent",
				"X-Agent-Name": "kagent/test-agent",
			},
			wantUserID:  "system:serviceaccount:kagent:test-agent",
			wantAgentID: "kagent/test-agent",
			wantErr:     false,
		},
		{
			name:    "returns error when no auth method available",
			wantErr: true,
		},
		{
			name: "returns error when no X-Agent-Name header for fallback",
			queryParams: map[string]string{
				"user_id": "system:serviceaccount:kagent:kebab-agent",
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
			if principal.User.ID != tt.wantUserID {
				t.Errorf("User.ID = %q, want %q", principal.User.ID, tt.wantUserID)
			}
			if principal.Agent.ID != tt.wantAgentID {
				t.Errorf("Agent.ID = %q, want %q", principal.Agent.ID, tt.wantAgentID)
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

	err = auth.UpstreamAuth(req, session, session.Principal())
	if err != nil {
		t.Errorf("UpstreamAuth returned error: %v", err)
	}

	// Verify the Authorization header was forwarded
	if got := req.Header.Get("Authorization"); got != authHeader {
		t.Errorf("Authorization header = %q, want %q", got, authHeader)
	}
}
