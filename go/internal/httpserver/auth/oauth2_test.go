/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/pkg/auth"
)

// testOIDCServer creates a mock OIDC server for testing
type testOIDCServer struct {
	server     *httptest.Server
	privateKey *rsa.PrivateKey
	keyID      string
}

func newTestOIDCServer(t *testing.T) *testOIDCServer {
	t.Helper()

	// Generate RSA key pair for signing
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	keyID := "test-key-1"

	// Create JWK from public key
	publicJWK, err := jwk.FromRaw(privateKey.PublicKey)
	require.NoError(t, err)
	err = publicJWK.Set(jwk.KeyIDKey, keyID)
	require.NoError(t, err)
	err = publicJWK.Set(jwk.AlgorithmKey, jwa.RS256)
	require.NoError(t, err)
	err = publicJWK.Set(jwk.KeyUsageKey, "sig")
	require.NoError(t, err)

	keySet := jwk.NewSet()
	err = keySet.AddKey(publicJWK)
	require.NoError(t, err)

	ts := &testOIDCServer{
		privateKey: privateKey,
		keyID:      keyID,
	}

	// Create test server
	mux := http.NewServeMux()

	// OIDC discovery endpoint
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		discovery := map[string]interface{}{
			"issuer":                 ts.server.URL,
			"authorization_endpoint": ts.server.URL + "/authorize",
			"token_endpoint":         ts.server.URL + "/token",
			"jwks_uri":               ts.server.URL + "/jwks",
			"userinfo_endpoint":      ts.server.URL + "/userinfo",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(discovery)
	})

	// JWKS endpoint
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(keySet)
	})

	ts.server = httptest.NewServer(mux)

	return ts
}

func (ts *testOIDCServer) Close() {
	ts.server.Close()
}

func (ts *testOIDCServer) URL() string {
	return ts.server.URL
}

func (ts *testOIDCServer) createToken(t *testing.T, claims map[string]interface{}, expiry time.Duration) string {
	t.Helper()

	builder := jwt.NewBuilder()

	// Set standard claims
	builder.Issuer(ts.server.URL)
	builder.IssuedAt(time.Now())
	builder.Expiration(time.Now().Add(expiry))

	// Set custom claims
	for k, v := range claims {
		builder.Claim(k, v)
	}

	token, err := builder.Build()
	require.NoError(t, err)

	// Create signing key
	privateJWK, err := jwk.FromRaw(ts.privateKey)
	require.NoError(t, err)
	err = privateJWK.Set(jwk.KeyIDKey, ts.keyID)
	require.NoError(t, err)
	err = privateJWK.Set(jwk.AlgorithmKey, jwa.RS256)
	require.NoError(t, err)

	// Sign the token
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.RS256, privateJWK))
	require.NoError(t, err)

	return string(signed)
}

func TestNewOAuth2Authenticator(t *testing.T) {
	tests := []struct {
		name        string
		config      OAuth2Config
		expectError bool
	}{
		{
			name: "valid config",
			config: OAuth2Config{
				IssuerURL: "https://example.com",
				ClientID:  "test-client",
			},
			expectError: false,
		},
		{
			name: "missing issuer URL",
			config: OAuth2Config{
				ClientID: "test-client",
			},
			expectError: true,
		},
		{
			name: "default claims are set",
			config: OAuth2Config{
				IssuerURL: "https://example.com",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth, err := NewOAuth2Authenticator(tt.config)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, auth)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, auth)
			}
		})
	}
}

func TestOAuth2Authenticator_Authenticate(t *testing.T) {
	ts := newTestOIDCServer(t)
	defer ts.Close()

	authenticator, err := NewOAuth2Authenticator(OAuth2Config{
		IssuerURL:   ts.URL(),
		UserIDClaim: "sub",
		RolesClaim:  "roles",
	})
	require.NoError(t, err)

	tests := []struct {
		name           string
		setupHeaders   func() http.Header
		setupQuery     func() url.Values
		expectedUserID string
		expectedRoles  []string
		expectError    bool
		errorContains  string
	}{
		{
			name: "valid token in Authorization header",
			setupHeaders: func() http.Header {
				token := ts.createToken(t, map[string]interface{}{
					"sub":   "user123",
					"roles": []interface{}{"admin", "user"},
				}, time.Hour)
				h := http.Header{}
				h.Set("Authorization", "Bearer "+token)
				return h
			},
			setupQuery:     func() url.Values { return url.Values{} },
			expectedUserID: "user123",
			expectedRoles:  []string{"admin", "user"},
			expectError:    false,
		},
		{
			name: "valid token in query parameter",
			setupHeaders: func() http.Header {
				return http.Header{}
			},
			setupQuery: func() url.Values {
				token := ts.createToken(t, map[string]interface{}{
					"sub": "user456",
				}, time.Hour)
				q := url.Values{}
				q.Set("access_token", token)
				return q
			},
			expectedUserID: "user456",
			expectError:    false,
		},
		{
			name: "missing token",
			setupHeaders: func() http.Header {
				return http.Header{}
			},
			setupQuery:    func() url.Values { return url.Values{} },
			expectError:   true,
			errorContains: "missing bearer token",
		},
		{
			name: "expired token",
			setupHeaders: func() http.Header {
				token := ts.createToken(t, map[string]interface{}{
					"sub": "user789",
				}, -time.Hour) // Already expired
				h := http.Header{}
				h.Set("Authorization", "Bearer "+token)
				return h
			},
			setupQuery:    func() url.Values { return url.Values{} },
			expectError:   true,
			errorContains: "invalid or expired token",
		},
		{
			name: "invalid token format",
			setupHeaders: func() http.Header {
				h := http.Header{}
				h.Set("Authorization", "Bearer invalid-token")
				return h
			},
			setupQuery:    func() url.Values { return url.Values{} },
			expectError:   true,
			errorContains: "invalid or expired token",
		},
		{
			name: "bearer prefix case insensitive",
			setupHeaders: func() http.Header {
				token := ts.createToken(t, map[string]interface{}{
					"sub": "user-case",
				}, time.Hour)
				h := http.Header{}
				h.Set("Authorization", "BEARER "+token)
				return h
			},
			setupQuery:     func() url.Values { return url.Values{} },
			expectedUserID: "user-case",
			expectError:    false,
		},
		{
			name: "roles as space-separated string",
			setupHeaders: func() http.Header {
				token := ts.createToken(t, map[string]interface{}{
					"sub":   "user-roles",
					"roles": "role1 role2 role3",
				}, time.Hour)
				h := http.Header{}
				h.Set("Authorization", "Bearer "+token)
				return h
			},
			setupQuery:     func() url.Values { return url.Values{} },
			expectedUserID: "user-roles",
			expectedRoles:  []string{"role1", "role2", "role3"},
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			session, err := authenticator.Authenticate(ctx, tt.setupHeaders(), tt.setupQuery())

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, session)

			principal := session.Principal()
			assert.Equal(t, tt.expectedUserID, principal.User.ID)
			if len(tt.expectedRoles) > 0 {
				assert.Equal(t, tt.expectedRoles, principal.User.Roles)
			}

			// Verify it's an OAuth2Session
			oauth2Session, ok := session.(*OAuth2Session)
			require.True(t, ok)
			assert.NotEmpty(t, oauth2Session.AccessToken())
			assert.NotNil(t, oauth2Session.Claims())
		})
	}
}

func TestOAuth2Authenticator_RequiredScopes(t *testing.T) {
	ts := newTestOIDCServer(t)
	defer ts.Close()

	authenticator, err := NewOAuth2Authenticator(OAuth2Config{
		IssuerURL:      ts.URL(),
		RequiredScopes: []string{"read", "write"},
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		scopes      interface{}
		expectError bool
	}{
		{
			name:        "has all required scopes as string",
			scopes:      "read write delete",
			expectError: false,
		},
		{
			name:        "has all required scopes as array",
			scopes:      []interface{}{"read", "write", "admin"},
			expectError: false,
		},
		{
			name:        "missing required scope",
			scopes:      "read",
			expectError: true,
		},
		{
			name:        "no scopes",
			scopes:      nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := map[string]interface{}{
				"sub": "user123",
			}
			if tt.scopes != nil {
				claims["scope"] = tt.scopes
			}

			token := ts.createToken(t, claims, time.Hour)
			headers := http.Header{}
			headers.Set("Authorization", "Bearer "+token)

			ctx := context.Background()
			_, err := authenticator.Authenticate(ctx, headers, url.Values{})

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestOAuth2Authenticator_AudienceValidation(t *testing.T) {
	ts := newTestOIDCServer(t)
	defer ts.Close()

	authenticator, err := NewOAuth2Authenticator(OAuth2Config{
		IssuerURL: ts.URL(),
		Audience:  "my-api",
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		audience    interface{}
		expectError bool
	}{
		{
			name:        "matching audience",
			audience:    "my-api",
			expectError: false,
		},
		{
			name:        "audience in array",
			audience:    []interface{}{"other-api", "my-api"},
			expectError: false,
		},
		{
			name:        "wrong audience",
			audience:    "wrong-api",
			expectError: true,
		},
		{
			name:        "missing audience",
			audience:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := map[string]interface{}{
				"sub": "user123",
			}
			if tt.audience != nil {
				claims["aud"] = tt.audience
			}

			token := ts.createToken(t, claims, time.Hour)
			headers := http.Header{}
			headers.Set("Authorization", "Bearer "+token)

			ctx := context.Background()
			_, err := authenticator.Authenticate(ctx, headers, url.Values{})

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestOAuth2Authenticator_UpstreamAuth(t *testing.T) {
	authenticator, err := NewOAuth2Authenticator(OAuth2Config{
		IssuerURL: "https://example.com",
	})
	require.NoError(t, err)

	tests := []struct {
		name           string
		session        auth.Session
		expectedUserID string
		expectedAuth   string
	}{
		{
			name: "forwards user ID and token",
			session: &OAuth2Session{
				principal: auth.Principal{
					User: auth.User{
						ID: "user123",
					},
				},
				accessToken: "test-token",
			},
			expectedUserID: "user123",
			expectedAuth:   "Bearer test-token",
		},
		{
			name:           "nil session",
			session:        nil,
			expectedUserID: "",
			expectedAuth:   "",
		},
		{
			name: "session without token",
			session: &OAuth2Session{
				principal: auth.Principal{
					User: auth.User{
						ID: "user456",
					},
				},
			},
			expectedUserID: "user456",
			expectedAuth:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)

			err := authenticator.UpstreamAuth(req, tt.session, auth.Principal{})
			require.NoError(t, err)

			if tt.expectedUserID != "" {
				assert.Equal(t, tt.expectedUserID, req.Header.Get("X-User-Id"))
			} else {
				assert.Empty(t, req.Header.Get("X-User-Id"))
			}

			if tt.expectedAuth != "" {
				assert.Equal(t, tt.expectedAuth, req.Header.Get("Authorization"))
			} else {
				assert.Empty(t, req.Header.Get("Authorization"))
			}
		})
	}
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name          string
		authorization string
		expected      string
	}{
		{
			name:          "valid bearer token",
			authorization: "Bearer abc123",
			expected:      "abc123",
		},
		{
			name:          "bearer lowercase",
			authorization: "bearer xyz789",
			expected:      "xyz789",
		},
		{
			name:          "BEARER uppercase",
			authorization: "BEARER TOKEN123",
			expected:      "TOKEN123",
		},
		{
			name:          "empty header",
			authorization: "",
			expected:      "",
		},
		{
			name:          "basic auth",
			authorization: "Basic dXNlcjpwYXNz",
			expected:      "",
		},
		{
			name:          "no token after bearer",
			authorization: "Bearer",
			expected:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{}
			if tt.authorization != "" {
				headers.Set("Authorization", tt.authorization)
			}
			result := extractBearerToken(headers)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOAuth2Session(t *testing.T) {
	session := &OAuth2Session{
		principal: auth.Principal{
			User: auth.User{
				ID:    "test-user",
				Roles: []string{"admin"},
			},
			Agent: auth.Agent{
				ID: "agent-1",
			},
		},
		accessToken: "test-token",
		claims: map[string]interface{}{
			"sub":   "test-user",
			"email": "test@example.com",
		},
		expiresAt: time.Now().Add(time.Hour),
	}

	t.Run("Principal", func(t *testing.T) {
		p := session.Principal()
		assert.Equal(t, "test-user", p.User.ID)
		assert.Equal(t, []string{"admin"}, p.User.Roles)
		assert.Equal(t, "agent-1", p.Agent.ID)
	})

	t.Run("AccessToken", func(t *testing.T) {
		assert.Equal(t, "test-token", session.AccessToken())
	})

	t.Run("Claims", func(t *testing.T) {
		claims := session.Claims()
		assert.Equal(t, "test-user", claims["sub"])
		assert.Equal(t, "test@example.com", claims["email"])
	})

	t.Run("ExpiresAt", func(t *testing.T) {
		assert.False(t, session.ExpiresAt().IsZero())
		assert.True(t, session.ExpiresAt().After(time.Now()))
	})
}

func TestOAuth2Authenticator_CacheClearing(t *testing.T) {
	ts := newTestOIDCServer(t)
	defer ts.Close()

	authenticator, err := NewOAuth2Authenticator(OAuth2Config{
		IssuerURL:         ts.URL(),
		JWKSCacheDuration: time.Hour,
	})
	require.NoError(t, err)

	// Make a request to populate the cache
	token := ts.createToken(t, map[string]interface{}{
		"sub": "user123",
	}, time.Hour)
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)

	ctx := context.Background()
	_, err = authenticator.Authenticate(ctx, headers, url.Values{})
	require.NoError(t, err)

	// Clear the cache
	authenticator.ClearCache()

	// Verify cache is cleared by checking internal state
	authenticator.jwksMu.RLock()
	assert.Nil(t, authenticator.jwksCache)
	authenticator.jwksMu.RUnlock()

	authenticator.discoveryMu.RLock()
	assert.Nil(t, authenticator.discoveryCache)
	authenticator.discoveryMu.RUnlock()

	// Should still work after cache is cleared (will refetch)
	_, err = authenticator.Authenticate(ctx, headers, url.Values{})
	require.NoError(t, err)
}

func TestOAuth2Authenticator_SkipValidation(t *testing.T) {
	ts := newTestOIDCServer(t)
	defer ts.Close()

	t.Run("skip expiry validation", func(t *testing.T) {
		authenticator, err := NewOAuth2Authenticator(OAuth2Config{
			IssuerURL:            ts.URL(),
			SkipExpiryValidation: true,
		})
		require.NoError(t, err)

		// Create an expired token
		token := ts.createToken(t, map[string]interface{}{
			"sub": "user123",
		}, -time.Hour)

		headers := http.Header{}
		headers.Set("Authorization", "Bearer "+token)

		ctx := context.Background()
		session, err := authenticator.Authenticate(ctx, headers, url.Values{})
		require.NoError(t, err)
		assert.Equal(t, "user123", session.Principal().User.ID)
	})

	t.Run("skip issuer validation", func(t *testing.T) {
		authenticator, err := NewOAuth2Authenticator(OAuth2Config{
			IssuerURL:            ts.URL(),
			SkipIssuerValidation: true,
		})
		require.NoError(t, err)

		// The token issuer from ts.createToken matches ts.URL(),
		// so this should work regardless
		token := ts.createToken(t, map[string]interface{}{
			"sub": "user456",
		}, time.Hour)

		headers := http.Header{}
		headers.Set("Authorization", "Bearer "+token)

		ctx := context.Background()
		session, err := authenticator.Authenticate(ctx, headers, url.Values{})
		require.NoError(t, err)
		assert.Equal(t, "user456", session.Principal().User.ID)
	})
}

func TestOAuth2Authenticator_CustomClaims(t *testing.T) {
	ts := newTestOIDCServer(t)
	defer ts.Close()

	authenticator, err := NewOAuth2Authenticator(OAuth2Config{
		IssuerURL:   ts.URL(),
		UserIDClaim: "email",
		RolesClaim:  "groups",
	})
	require.NoError(t, err)

	token := ts.createToken(t, map[string]interface{}{
		"sub":    "user123",
		"email":  "test@example.com",
		"groups": []interface{}{"developers", "admins"},
	}, time.Hour)

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)

	ctx := context.Background()
	session, err := authenticator.Authenticate(ctx, headers, url.Values{})
	require.NoError(t, err)

	// Should use email as user ID instead of sub
	assert.Equal(t, "test@example.com", session.Principal().User.ID)
	// Should use groups as roles instead of roles
	assert.Equal(t, []string{"developers", "admins"}, session.Principal().User.Roles)
}

// Verify interface compliance
var _ auth.Session = (*OAuth2Session)(nil)
var _ auth.AuthProvider = (*OAuth2Authenticator)(nil)
