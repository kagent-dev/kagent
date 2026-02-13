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
		claimsConfig authimpl.ClaimsConfig
		wantUserID   string
		wantEmail    string
		wantName     string
		wantGroups   []string
		wantErr      bool
		noToken      bool
		invalidToken bool
	}{
		{
			name: "extracts standard claims successfully",
			claims: map[string]any{
				"sub":    "user123",
				"email":  "user@example.com",
				"name":   "Test User",
				"groups": []any{"admin", "developers"},
			},
			wantUserID: "user123",
			wantEmail:  "user@example.com",
			wantName:   "Test User",
			wantGroups: []string{"admin", "developers"},
			wantErr:    false,
		},
		{
			name: "uses preferred_username as fallback for name",
			claims: map[string]any{
				"sub":                "user123",
				"email":              "user@example.com",
				"preferred_username": "testuser",
			},
			wantUserID: "user123",
			wantEmail:  "user@example.com",
			wantName:   "testuser",
			wantGroups: []string{},
			wantErr:    false,
		},
		{
			name: "handles cognito:groups claim",
			claims: map[string]any{
				"sub":            "user123",
				"cognito:groups": []any{"admins", "users"},
			},
			wantUserID: "user123",
			wantGroups: []string{"admins", "users"},
			wantErr:    false,
		},
		{
			name: "handles roles claim",
			claims: map[string]any{
				"sub":   "user123",
				"roles": []any{"admin", "editor"},
			},
			wantUserID: "user123",
			wantGroups: []string{"admin", "editor"},
			wantErr:    false,
		},
		{
			name: "handles comma-separated groups string",
			claims: map[string]any{
				"sub":    "user123",
				"groups": "admin, developers, users",
			},
			wantUserID: "user123",
			wantGroups: []string{"admin", "developers", "users"},
			wantErr:    false,
		},
		{
			name: "uses custom claim names from config",
			claims: map[string]any{
				"user_id":      "custom-user-123",
				"mail":         "custom@example.com",
				"display_name": "Custom Name",
				"team_groups":  []any{"team-a", "team-b"},
			},
			claimsConfig: authimpl.ClaimsConfig{
				UserID: "user_id",
				Email:  "mail",
				Name:   "display_name",
				Groups: "team_groups",
			},
			wantUserID: "custom-user-123",
			wantEmail:  "custom@example.com",
			wantName:   "Custom Name",
			wantGroups: []string{"team-a", "team-b"},
			wantErr:    false,
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
			name: "handles empty claims gracefully",
			claims: map[string]any{
				"sub": "user123",
			},
			wantUserID: "user123",
			wantEmail:  "",
			wantName:   "",
			wantGroups: []string{},
			wantErr:    false,
		},
		{
			name: "handles single group in array",
			claims: map[string]any{
				"sub":    "user123",
				"groups": []any{"admin"},
			},
			wantUserID: "user123",
			wantGroups: []string{"admin"},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := authimpl.NewProxyAuthenticator(tt.claimsConfig)

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
			if principal.User.Email != tt.wantEmail {
				t.Errorf("User.Email = %q, want %q", principal.User.Email, tt.wantEmail)
			}
			if principal.User.Name != tt.wantName {
				t.Errorf("User.Name = %q, want %q", principal.User.Name, tt.wantName)
			}
			if len(principal.Groups) != len(tt.wantGroups) {
				t.Errorf("Groups length = %d, want %d (got %v)", len(principal.Groups), len(tt.wantGroups), principal.Groups)
			}
			for i, g := range principal.Groups {
				if i < len(tt.wantGroups) && g != tt.wantGroups[i] {
					t.Errorf("Groups[%d] = %q, want %q", i, g, tt.wantGroups[i])
				}
			}
		})
	}
}

func TestProxyAuthenticator_JWTWithAgentHeader(t *testing.T) {
	// This test verifies that X-Agent-Name header is extracted even when
	// authenticating via JWT. This is important for agent-to-controller
	// calls where the agent sends both a service account JWT and X-Agent-Name.
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
			auth := authimpl.NewProxyAuthenticator(authimpl.ClaimsConfig{})

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
			name: "authenticates via user_id query param",
			queryParams: map[string]string{
				"user_id": "system:serviceaccount:kagent:kebab-agent",
			},
			wantUserID: "system:serviceaccount:kagent:kebab-agent",
			wantErr:    false,
		},
		{
			name: "authenticates via X-User-Id header",
			headers: map[string]string{
				"X-User-Id": "system:serviceaccount:kagent:test-agent",
			},
			wantUserID: "system:serviceaccount:kagent:test-agent",
			wantErr:    false,
		},
		{
			name: "extracts agent identity from X-Agent-Name header",
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
			name:    "returns error when no auth method available",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := authimpl.NewProxyAuthenticator(authimpl.ClaimsConfig{})

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
	auth := authimpl.NewProxyAuthenticator(authimpl.ClaimsConfig{})

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
