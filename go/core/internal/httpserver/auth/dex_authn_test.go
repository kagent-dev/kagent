package auth_test

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

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/stretchr/testify/require"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
)

// oidcTestFixture holds a running HTTP server that simulates Dex's OIDC and
// JWKS endpoints, and the RSA key used for signing/verifying tokens.
type oidcTestFixture struct {
	server   *httptest.Server
	privKey  *rsa.PrivateKey
	keyID    string
	issuer   string
	clientID string
}

func newOIDCTestFixture(t *testing.T) *oidcTestFixture {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	f := &oidcTestFixture{
		privKey:  privKey,
		keyID:    "test-key-id",
		clientID: "kagent-a2a",
	}

	mux := http.NewServeMux()
	f.server = httptest.NewServer(mux)
	f.issuer = f.server.URL

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"issuer":   f.issuer,
			"jwks_uri": f.server.URL + "/keys",
		})
	})

	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		key, err := jwk.FromRaw(&privKey.PublicKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := key.Set(jwk.AlgorithmKey, jwa.RS256); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := key.Set(jwk.KeyIDKey, f.keyID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		key.Set(jwk.KeyUsageKey, "sig") //nolint:errcheck

		set := jwk.NewSet()
		set.AddKey(key) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(set)
	})

	return f
}

func (f *oidcTestFixture) signToken(t *testing.T, claims gojwt.MapClaims) string {
	t.Helper()
	token := gojwt.NewWithClaims(gojwt.SigningMethodRS256, claims)
	token.Header["kid"] = f.keyID
	signed, err := token.SignedString(f.privKey)
	require.NoError(t, err)
	return signed
}

func (f *oidcTestFixture) newAuthenticator(t *testing.T) *authimpl.DexAuthenticator {
	t.Helper()
	ctx := t.Context()
	a, err := authimpl.NewDexAuthenticator(ctx, authimpl.DexAuthenticatorConfig{
		Issuer:      f.issuer,
		ClientID:    f.clientID,
		UserIDClaim: "email",
		// RefreshInterval: zero means use default (15m), which satisfies the
		// httprc minimum-window constraint in tests.
	})
	require.NoError(t, err)
	return a
}

func TestDexAuthenticator_Authenticate(t *testing.T) {
	f := newOIDCTestFixture(t)
	defer f.server.Close()

	a := f.newAuthenticator(t)
	now := time.Now()

	tests := []struct {
		name        string
		buildHeader func() string
		wantErr     bool
		wantUserID  string
		wantGroups  []string
		wantEmail   string
	}{
		{
			name: "valid token with email and groups",
			buildHeader: func() string {
				return "Bearer " + f.signToken(t, gojwt.MapClaims{
					"iss":    f.issuer,
					"aud":    f.clientID,
					"sub":    "CiQ1MjQ4NzQ3MC1hMDJlLTQ5MGMtOTM0Yy0zNjg1N2U5YTA5YzQSBWxvY2Fs",
					"email":  "user@giantswarm.io",
					"groups": []string{"customer:devops", "customer:admin"},
					"exp":    now.Add(time.Hour).Unix(),
					"iat":    now.Unix(),
				})
			},
			wantUserID: "user@giantswarm.io",
			wantEmail:  "user@giantswarm.io",
			wantGroups: []string{"customer:devops", "customer:admin"},
		},
		{
			name: "valid token without groups claim",
			buildHeader: func() string {
				return "Bearer " + f.signToken(t, gojwt.MapClaims{
					"iss":   f.issuer,
					"aud":   f.clientID,
					"sub":   "user-sub",
					"email": "nogroups@giantswarm.io",
					"exp":   now.Add(time.Hour).Unix(),
					"iat":   now.Unix(),
				})
			},
			wantUserID: "nogroups@giantswarm.io",
		},
		{
			name: "token missing email falls back to sub",
			buildHeader: func() string {
				return "Bearer " + f.signToken(t, gojwt.MapClaims{
					"iss": f.issuer,
					"aud": f.clientID,
					"sub": "fallback-sub",
					"exp": now.Add(time.Hour).Unix(),
					"iat": now.Unix(),
				})
			},
			wantUserID: "fallback-sub",
		},
		{
			name:        "missing Authorization header",
			buildHeader: func() string { return "" },
			wantErr:     true,
		},
		{
			name:        "non-Bearer Authorization scheme",
			buildHeader: func() string { return "Basic dXNlcjpwYXNz" },
			wantErr:     true,
		},
		{
			name: "expired token",
			buildHeader: func() string {
				return "Bearer " + f.signToken(t, gojwt.MapClaims{
					"iss":   f.issuer,
					"aud":   f.clientID,
					"sub":   "expired-sub",
					"email": "expired@giantswarm.io",
					"exp":   now.Add(-2 * time.Hour).Unix(),
					"iat":   now.Add(-3 * time.Hour).Unix(),
				})
			},
			wantErr: true,
		},
		{
			name: "wrong issuer",
			buildHeader: func() string {
				return "Bearer " + f.signToken(t, gojwt.MapClaims{
					"iss":   "https://evil.example.com",
					"aud":   f.clientID,
					"sub":   "sub",
					"email": "evil@example.com",
					"exp":   now.Add(time.Hour).Unix(),
					"iat":   now.Unix(),
				})
			},
			wantErr: true,
		},
		{
			name: "wrong audience",
			buildHeader: func() string {
				return "Bearer " + f.signToken(t, gojwt.MapClaims{
					"iss":   f.issuer,
					"aud":   "some-other-client",
					"sub":   "sub",
					"email": "user@example.com",
					"exp":   now.Add(time.Hour).Unix(),
					"iat":   now.Unix(),
				})
			},
			wantErr: true,
		},
		{
			name:        "garbage token",
			buildHeader: func() string { return "Bearer not.a.jwt" },
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{}
			if h := tt.buildHeader(); h != "" {
				headers.Set("Authorization", h)
			}

			session, err := a.Authenticate(t.Context(), headers, url.Values{})

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, session)

			principal := session.Principal()
			require.Equal(t, tt.wantUserID, principal.User.ID)

			if tt.wantEmail != "" {
				require.Equal(t, tt.wantEmail, principal.Claims["email"])
			}
			if len(tt.wantGroups) > 0 {
				groups, ok := principal.Claims["groups"]
				require.True(t, ok, "claims should contain groups")
				// JSON numbers decode as []interface{}, JWT lib may return []string or []interface{}
				switch g := groups.(type) {
				case []string:
					require.Equal(t, tt.wantGroups, g)
				case []any:
					var got []string
					for _, v := range g {
						got = append(got, v.(string))
					}
					require.Equal(t, tt.wantGroups, got)
				default:
					t.Fatalf("unexpected groups type %T", groups)
				}
			}
		})
	}
}

func TestDexAuthenticator_UpstreamAuth(t *testing.T) {
	f := newOIDCTestFixture(t)
	defer f.server.Close()

	a := f.newAuthenticator(t)
	now := time.Now()

	authHeader := "Bearer " + f.signToken(t, gojwt.MapClaims{
		"iss":   f.issuer,
		"aud":   f.clientID,
		"sub":   "user-sub",
		"email": "user@giantswarm.io",
		"exp":   now.Add(time.Hour).Unix(),
		"iat":   now.Unix(),
	})

	headers := http.Header{}
	headers.Set("Authorization", authHeader)

	session, err := a.Authenticate(t.Context(), headers, url.Values{})
	require.NoError(t, err)

	upstream, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://agent.example.com", nil)
	err = a.UpstreamAuth(upstream, session, session.Principal())
	require.NoError(t, err)

	require.Equal(t, authHeader, upstream.Header.Get("Authorization"), "token must be forwarded")
	require.Equal(t, "user@giantswarm.io", upstream.Header.Get("X-User-Id"), "user ID must be forwarded")
}

func TestDexAuthenticator_Discovery(t *testing.T) {
	t.Run("invalid issuer URL", func(t *testing.T) {
		_, err := authimpl.NewDexAuthenticator(t.Context(), authimpl.DexAuthenticatorConfig{
			Issuer:   "http://127.0.0.1:1",
			ClientID: "kagent",
		})
		require.Error(t, err)
	})

	t.Run("discovery returns no jwks_uri", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]string{"issuer": "http://bad"})
		}))
		defer srv.Close()

		_, err := authimpl.NewDexAuthenticator(t.Context(), authimpl.DexAuthenticatorConfig{
			Issuer:   srv.URL,
			ClientID: "kagent",
		})
		require.Error(t, err)
	})

	t.Run("JWKS fetch fails", func(t *testing.T) {
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/.well-known/openid-configuration" {
				json.NewEncoder(w).Encode(map[string]string{
					"issuer":   srv.URL,
					"jwks_uri": srv.URL + "/keys",
				})
				return
			}
			// /keys returns 500
			http.Error(w, "internal error", http.StatusInternalServerError)
		}))
		defer srv.Close()

		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		_, err := authimpl.NewDexAuthenticator(ctx, authimpl.DexAuthenticatorConfig{
			Issuer:   srv.URL,
			ClientID: "kagent",
		})
		require.Error(t, err)
	})
}
