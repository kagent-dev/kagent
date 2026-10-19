package auth_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		required     bool
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
			name: "refuses a token without the custom claim when it is required",
			claims: map[string]any{
				"sub":   "fallback-user",
				"email": "user@example.com",
			},
			userIDClaim: "user_id",
			required:    true,
			wantErr:     true,
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
			var options []authimpl.ProxyAuthenticatorOption
			if tt.required {
				options = append(options, authimpl.WithUserIDClaimRequired())
			}
			auth := authimpl.NewProxyAuthenticator(tt.userIDClaim, options...)

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

func TestProxyAuthenticator_AgentCalls(t *testing.T) {
	tests := []struct {
		name        string
		headers     map[string]string
		queryParams map[string]string
		wantUserID  string
		wantAgentID string
		wantErr     bool
	}{
		{
			name: "agent with SA Bearer token and X-User-Id header uses header identity",
			headers: map[string]string{
				"Authorization": "Bearer " + createTestJWT(map[string]any{"sub": "system:serviceaccount:kagent:test-agent"}),
				"X-Agent-Name":  "kagent/test-agent",
				"X-User-Id":     "user@example.com",
			},
			wantUserID:  "user@example.com",
			wantAgentID: "kagent/test-agent",
		},
		{
			name: "agent with SA Bearer token and user_id query param uses query identity",
			headers: map[string]string{
				"Authorization": "Bearer " + createTestJWT(map[string]any{"sub": "system:serviceaccount:kagent:test-agent"}),
				"X-Agent-Name":  "kagent/test-agent",
			},
			queryParams: map[string]string{
				"user_id": "user@example.com",
			},
			wantUserID:  "user@example.com",
			wantAgentID: "kagent/test-agent",
		},
		{
			name: "agent with no X-User-Id falls back to SA sub claim",
			headers: map[string]string{
				"Authorization": "Bearer " + createTestJWT(map[string]any{"sub": "system:serviceaccount:kagent:test-agent"}),
				"X-Agent-Name":  "kagent/test-agent",
			},
			wantUserID:  "system:serviceaccount:kagent:test-agent",
			wantAgentID: "kagent/test-agent",
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

	// Verify X-User-Id is forwarded so downstream A2A runtimes receive the real user identity
	if got := req.Header.Get("X-User-Id"); got != "user123" {
		t.Errorf("X-User-Id header = %q, want %q", got, "user123")
	}
}

func TestProxyAuthenticator_Groups(t *testing.T) {
	serviceAccountToken := createTestJWT(map[string]any{"sub": "system:serviceaccount:kagent:assistant"})
	tests := []struct {
		name       string
		claims     map[string]any
		headers    map[string]string
		wantUserID string
		wantGroups []string
		wantErr    bool
	}{
		{name: "a list claim", claims: map[string]any{"sub": "alice", "groups": []any{"team-a", "cn=admins,ou=groups"}},
			wantUserID: "alice", wantGroups: []string{"team-a", "cn=admins,ou=groups"}},
		{name: "a string claim", claims: map[string]any{"sub": "alice", "groups": "team-a"}, wantUserID: "alice", wantGroups: []string{"team-a"}},
		{name: "no claim", claims: map[string]any{"sub": "alice"}, wantUserID: "alice"},
		{name: "a group that is not a string", claims: map[string]any{"sub": "alice", "groups": []any{"team-a", 7}}, wantErr: true},
		{name: "a claim of another shape", claims: map[string]any{"sub": "alice", "groups": 7}, wantErr: true},
		{name: "a direct call's header is ignored", claims: map[string]any{"sub": "alice"},
			headers: map[string]string{"X-User-Groups": "system%3Amasters"}, wantUserID: "alice"},
		{name: "an agent's call carries its user's groups",
			headers:    map[string]string{"Authorization": "Bearer " + serviceAccountToken, "X-Agent-Name": "kagent/assistant", "X-User-Id": "alice", "X-User-Groups": "team-a,cn%3Dadmins%2Cou%3Dgroups"},
			wantUserID: "alice", wantGroups: []string{"team-a", "cn=admins,ou=groups"}},
		{name: "an agent's call without a user names no groups",
			headers:    map[string]string{"Authorization": "Bearer " + serviceAccountToken, "X-Agent-Name": "kagent/assistant", "X-User-Groups": "team-a"},
			wantUserID: "system:serviceaccount:kagent:assistant"},
		{name: "an agent's call with malformed groups",
			headers: map[string]string{"Authorization": "Bearer " + serviceAccountToken, "X-Agent-Name": "kagent/assistant", "X-User-Id": "alice", "X-User-Groups": "team-a,,admins"},
			wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authenticator := authimpl.NewProxyAuthenticator("", authimpl.WithGroupsClaim("groups"))
			headers := http.Header{}
			if tt.claims != nil {
				headers.Set("Authorization", "Bearer "+createTestJWT(tt.claims))
			}
			for k, v := range tt.headers {
				headers.Set(k, v)
			}
			session, err := authenticator.Authenticate(context.Background(), headers, url.Values{})
			if tt.wantErr {
				require.ErrorIs(t, err, authimpl.ErrUnauthenticated)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantUserID, session.Principal().User.ID)
			require.Equal(t, tt.wantGroups, session.Principal().User.Groups)
		})
	}
}

func TestProxyAuthenticator_UpstreamAuthForwardsTheGroups(t *testing.T) {
	authenticator := authimpl.NewProxyAuthenticator("", authimpl.WithGroupsClaim("groups"))
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+createTestJWT(map[string]any{"sub": "alice", "groups": []any{"team-a", "cn=admins,ou=groups"}}))
	session, err := authenticator.Authenticate(context.Background(), headers, url.Values{})
	require.NoError(t, err)

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("X-User-Groups", "stale")
	require.NoError(t, authenticator.UpstreamAuth(req, session, session.Principal()))
	require.Equal(t, "team-a,cn=admins%2Cou=groups", req.Header.Get("X-User-Groups"))

	// A user without groups forwards none, whatever the request held.
	headers.Set("Authorization", "Bearer "+createTestJWT(map[string]any{"sub": "bob"}))
	session, err = authenticator.Authenticate(context.Background(), headers, url.Values{})
	require.NoError(t, err)
	require.NoError(t, authenticator.UpstreamAuth(req, session, session.Principal()))
	require.Empty(t, req.Header.Values("X-User-Groups"))
}

// fakeTokenReviewer answers every TokenReview with status, or fails with err,
// and records what it was asked.
type fakeTokenReviewer struct {
	status authenticationv1.TokenReviewStatus
	err    error
	asked  []authenticationv1.TokenReviewSpec
}

func (f *fakeTokenReviewer) Create(_ context.Context, review *authenticationv1.TokenReview, _ metav1.CreateOptions) (*authenticationv1.TokenReview, error) {
	f.asked = append(f.asked, review.Spec)
	if f.err != nil {
		return nil, f.err
	}
	result := review.DeepCopy()
	result.Status = f.status
	return result, nil
}

func TestProxyAuthenticator_ServiceAccountTokenReview(t *testing.T) {
	const serviceAccount = "system:serviceaccount:workspace-manager:workspace-manager"
	serviceAccountGroups := []string{"system:serviceaccounts", "system:serviceaccounts:workspace-manager", "system:authenticated"}
	serviceAccountToken := createTestJWT(map[string]any{"sub": serviceAccount, "aud": []any{"kagent"}, "kubernetes.io": map[string]any{"namespace": "workspace-manager"}})
	personToken := createTestJWT(map[string]any{"sub": "Cg1hbGljZQ", "email": "alice@example.com", "groups": []any{"team-a"}})
	authenticated := authenticationv1.TokenReviewStatus{Authenticated: true, Audiences: []string{"kagent"}, User: authenticationv1.UserInfo{Username: serviceAccount, Groups: serviceAccountGroups}}

	for _, tt := range []struct {
		name       string
		token      string
		agent      string
		audience   string
		reviewer   *fakeTokenReviewer
		wantUser   auth.User
		wantAsked  []authenticationv1.TokenReviewSpec
		wantRefuse bool
	}{
		{
			name: "a ServiceAccount token is the user and groups the API server authenticates", token: serviceAccountToken, audience: "kagent",
			reviewer:  &fakeTokenReviewer{status: authenticated},
			wantUser:  auth.User{ID: serviceAccount, Groups: serviceAccountGroups, Kubernetes: true},
			wantAsked: []authenticationv1.TokenReviewSpec{{Token: serviceAccountToken, Audiences: []string{"kagent"}}},
		},
		{
			// An authenticator that ignores audiences authenticates the token
			// for none: it is not shown to be meant for kagent.
			name: "a ServiceAccount token authenticated for no audience of kagent's is refused", token: serviceAccountToken, audience: "kagent",
			reviewer:   &fakeTokenReviewer{status: authenticationv1.TokenReviewStatus{Authenticated: true, User: authenticated.User}},
			wantAsked:  []authenticationv1.TokenReviewSpec{{Token: serviceAccountToken, Audiences: []string{"kagent"}}},
			wantRefuse: true,
		},
		{
			name: "a ServiceAccount token the API server does not authenticate is refused", token: serviceAccountToken, audience: "kagent",
			reviewer:   &fakeTokenReviewer{status: authenticationv1.TokenReviewStatus{Error: "token audiences [other] is invalid for the target audiences [kagent]"}},
			wantAsked:  []authenticationv1.TokenReviewSpec{{Token: serviceAccountToken, Audiences: []string{"kagent"}}},
			wantRefuse: true,
		},
		{
			name: "a ServiceAccount token that authenticates as someone else is refused", token: serviceAccountToken, audience: "kagent",
			reviewer:   &fakeTokenReviewer{status: authenticationv1.TokenReviewStatus{Authenticated: true, Audiences: []string{"kagent"}, User: authenticationv1.UserInfo{Username: "alice@example.com"}}},
			wantAsked:  []authenticationv1.TokenReviewSpec{{Token: serviceAccountToken, Audiences: []string{"kagent"}}},
			wantRefuse: true,
		},
		{
			name: "a ServiceAccount token is refused when the review fails", token: serviceAccountToken, audience: "kagent",
			reviewer:   &fakeTokenReviewer{err: errors.New("the API server is unreachable")},
			wantAsked:  []authenticationv1.TokenReviewSpec{{Token: serviceAccountToken, Audiences: []string{"kagent"}}},
			wantRefuse: true,
		},
		{
			name: "a person's token is read from its claims, unreviewed", token: personToken, audience: "kagent",
			reviewer: &fakeTokenReviewer{status: authenticated},
			wantUser: auth.User{ID: "alice@example.com", Groups: []string{"team-a"}},
		},
		{
			name: "an agent's call names its user, unreviewed", token: serviceAccountToken, agent: "assistant", audience: "kagent",
			reviewer: &fakeTokenReviewer{status: authenticated},
			wantUser: auth.User{ID: "alice@example.com"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			authenticator := authimpl.NewProxyAuthenticator("email", authimpl.WithGroupsClaim("groups"), authimpl.WithUserIDClaimRequired(),
				authimpl.WithServiceAccountTokenReview(tt.reviewer, tt.audience))
			headers := http.Header{}
			headers.Set("Authorization", "Bearer "+tt.token)
			if tt.agent != "" {
				headers.Set("X-Agent-Name", tt.agent)
				headers.Set("X-User-Id", "alice@example.com")
			}
			session, err := authenticator.Authenticate(context.Background(), headers, url.Values{})
			require.Equal(t, tt.wantAsked, tt.reviewer.asked)
			if tt.wantRefuse {
				require.ErrorIs(t, err, authimpl.ErrUnauthenticated)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantUser, session.Principal().User)
		})
	}
}

func TestProxyAuthenticator_SessionReader(t *testing.T) {
	const reader = "system:serviceaccount:agent-platform:klaus-gateway"
	readerToken := createTestJWT(map[string]any{"sub": reader, "aud": []any{"kagent"}})
	otherToken := createTestJWT(map[string]any{"sub": "system:serviceaccount:workspace-manager:workspace-manager", "aud": []any{"kagent"}})
	// A person's token whose user ID claim names the reader: its claims are
	// not the API server's word.
	impostorToken := createTestJWT(map[string]any{"sub": "Cg1hbGljZQ", "email": reader})
	reviewed := func(user string) *fakeTokenReviewer {
		return &fakeTokenReviewer{status: authenticationv1.TokenReviewStatus{Authenticated: true, Audiences: []string{"kagent"}, User: authenticationv1.UserInfo{Username: user}}}
	}

	for _, tt := range []struct {
		name        string
		token       string
		agent       string
		reviewer    *fakeTokenReviewer
		admitted    bool
		wantReader  bool
		wantRefused bool
		wantUser    string
	}{
		{name: "the reader's token on an admitted call is the session reader", token: readerToken, reviewer: reviewed(reader), admitted: true, wantReader: true, wantUser: reader},
		{name: "the reader's token on any other call is refused", token: readerToken, reviewer: reviewed(reader), wantRefused: true},
		{name: "another ServiceAccount on an admitted call is an ordinary caller", token: otherToken, reviewer: reviewed("system:serviceaccount:workspace-manager:workspace-manager"), admitted: true, wantUser: "system:serviceaccount:workspace-manager:workspace-manager"},
		{name: "a person's token naming the reader is an ordinary caller", token: impostorToken, reviewer: reviewed(reader), admitted: true, wantUser: reader},
		{name: "an agent's call naming the reader is an ordinary caller", token: readerToken, agent: "assistant", reviewer: reviewed(reader), admitted: true, wantUser: reader},
	} {
		t.Run(tt.name, func(t *testing.T) {
			authenticator := authimpl.NewProxyAuthenticator("email", authimpl.WithUserIDClaimRequired(),
				authimpl.WithServiceAccountTokenReview(tt.reviewer, "kagent"), authimpl.WithSessionReader(reader))
			headers := http.Header{}
			headers.Set("Authorization", "Bearer "+tt.token)
			if tt.agent != "" {
				headers.Set("X-Agent-Name", tt.agent)
				headers.Set("X-User-Id", reader)
			}
			ctx := context.Background()
			if tt.admitted {
				ctx = auth.AdmitSessionReader(ctx)
			}
			session, err := authenticator.Authenticate(ctx, headers, url.Values{})
			if tt.wantRefused {
				require.ErrorIs(t, err, auth.ErrSessionReaderRefused)
				require.Nil(t, session)
				return
			}
			require.NoError(t, err)
			_, isReader := session.(auth.SessionReaderSession)
			require.Equal(t, tt.wantReader, isReader)
			require.Equal(t, tt.wantUser, session.Principal().User.ID)
		})
	}
}
