package auth_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestExternalBearerAuthenticatorRFC7662RequestShapeAndClaimPreservation(t *testing.T) {
	var sawRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest = true
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer validation-token" {
			t.Errorf("Authorization = %q, want exact configured value", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			t.Errorf("body is not form encoded: %v", err)
		}
		if got := form.Get("token"); got != "inbound-token" {
			t.Errorf("token = %q, want inbound-token", got)
		}
		if got := form.Get("token_type_hint"); got != "access_token" {
			t.Errorf("token_type_hint = %q, want access_token", got)
		}

		writeJSON(t, w, http.StatusOK, map[string]any{
			"active":     true,
			"username":   "alice@example.com",
			"sub":        "subject-fallback",
			"client_id":  "web-client",
			"scope":      "kagent:a2a read",
			"aud":        []string{"kagent", "agents"},
			"iss":        "https://issuer.example.com",
			"grant_type": "authorization_code",
			"exp":        time.Now().Add(time.Hour).Unix(),
			"custom":     "preserved",
		})
	}))
	defer server.Close()

	authn := newExternalBearerForTest(t, authimpl.ExternalBearerAuthenticatorConfig{
		URL:                     server.URL,
		ValidationAuthorization: "Bearer validation-token",
		UserIDClaim:             "username",
	})
	headers := http.Header{}
	headers.Set("Authorization", "Bearer inbound-token")

	session, err := authn.Authenticate(context.Background(), headers, url.Values{})
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if !sawRequest {
		t.Fatal("introspection endpoint was not called")
	}
	principal := session.Principal()
	if principal.User.ID != "alice@example.com" {
		t.Fatalf("User.ID = %q, want alice@example.com", principal.User.ID)
	}
	assertClaimString(t, principal.Claims, "client_id", "web-client")
	assertClaimString(t, principal.Claims, "scope", "kagent:a2a read")
	assertClaimString(t, principal.Claims, "iss", "https://issuer.example.com")
	assertClaimString(t, principal.Claims, "grant_type", "authorization_code")
	assertClaimString(t, principal.Claims, "custom", "preserved")
	aud, ok := principal.Claims["aud"].([]any)
	if !ok || len(aud) != 2 || aud[0] != "kagent" || aud[1] != "agents" {
		t.Fatalf("Claims[aud] = %#v, want [kagent agents]", principal.Claims["aud"])
	}
}

func TestExternalBearerAuthenticatorEndpointAuthConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     authimpl.ExternalBearerAuthenticatorConfig
		wantErr string
	}{
		{
			name: "validation authorization conflicts with client credentials",
			cfg: authimpl.ExternalBearerAuthenticatorConfig{
				URL:                     "https://auth.example.com/introspect",
				ValidationAuthorization: "Bearer validation-token",
				ClientID:                "client",
				ClientSecret:            "secret",
			},
			wantErr: "cannot set ValidationAuthorization",
		},
		{
			name: "partial basic config rejects missing secret",
			cfg: authimpl.ExternalBearerAuthenticatorConfig{
				URL:      "https://auth.example.com/introspect",
				ClientID: "client",
			},
			wantErr: "requires both ClientID and ClientSecret",
		},
		{
			name: "partial basic config rejects missing client id",
			cfg: authimpl.ExternalBearerAuthenticatorConfig{
				URL:          "https://auth.example.com/introspect",
				ClientSecret: "secret",
			},
			wantErr: "requires both ClientID and ClientSecret",
		},
		{
			name: "unauthenticated introspection rejected by default",
			cfg: authimpl.ExternalBearerAuthenticatorConfig{
				URL: "https://auth.example.com/introspect",
			},
			wantErr: "requires introspection endpoint authentication",
		},
		{
			name: "http non-localhost rejected",
			cfg: authimpl.ExternalBearerAuthenticatorConfig{
				URL:                               "http://auth.example.com/introspect",
				AllowUnauthenticatedIntrospection: true,
			},
			wantErr: "must use https for non-localhost",
		},
		{
			name: "missing URL rejected",
			cfg: authimpl.ExternalBearerAuthenticatorConfig{
				ValidationAuthorization: "Bearer validation-token",
			},
			wantErr: "AUTH_EXTERNAL_BEARER_URL",
		},
		{
			name: "https with validation auth accepted",
			cfg: authimpl.ExternalBearerAuthenticatorConfig{
				URL:                     "https://auth.example.com/introspect",
				ValidationAuthorization: "Bearer validation-token",
			},
		},
		{
			name: "remote https with unauthenticated opt-in rejected",
			cfg: authimpl.ExternalBearerAuthenticatorConfig{
				URL:                               "https://auth.example.com/introspect",
				AllowUnauthenticatedIntrospection: true,
			},
			wantErr: "only allowed for localhost/loopback",
		},
		{
			name: "localhost http with explicit unauthenticated opt-in accepted",
			cfg: authimpl.ExternalBearerAuthenticatorConfig{
				URL:                               "http://localhost:8080/introspect",
				AllowUnauthenticatedIntrospection: true,
			},
		},
		{
			name: "loopback https with explicit unauthenticated opt-in accepted",
			cfg: authimpl.ExternalBearerAuthenticatorConfig{
				URL:                               "https://127.0.0.1:8443/introspect",
				AllowUnauthenticatedIntrospection: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := authimpl.NewExternalBearerAuthenticator(tt.cfg)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want to contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestExternalBearerAuthenticatorRejectsServiceTokenClaimsInThisSlice(t *testing.T) {
	// Clear service-token claims are rejected before they can receive human
	// X-User-Id propagation via username/sub fallbacks.
	tests := []struct {
		name   string
		claims map[string]any
	}{
		{
			name: "grant_type client_credentials",
			claims: map[string]any{
				"active":     true,
				"grant_type": "client_credentials",
				"username":   "service-looking-user",
				"sub":        "service-subject",
			},
		},
		{
			name: "token_class service",
			claims: map[string]any{
				"active":      true,
				"token_class": "service",
				"username":    "service-looking-user",
				"sub":         "service-subject",
			},
		},
		{
			name: "token_use client_credentials",
			claims: map[string]any{
				"active":    true,
				"token_use": "client_credentials",
				"username":  "service-looking-user",
				"sub":       "service-subject",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusOK, tt.claims)
			}))
			defer server.Close()

			authn := newExternalBearerForTest(t, authimpl.ExternalBearerAuthenticatorConfig{
				URL:                               server.URL,
				AllowUnauthenticatedIntrospection: true,
			})
			headers := http.Header{"Authorization": []string{"Bearer service-token"}}
			if _, err := authn.Authenticate(context.Background(), headers, url.Values{}); err == nil {
				t.Fatal("expected service-token claims to be rejected, got nil error")
			}
		})
	}
}

func TestExternalBearerAuthenticatorContentTypeValidation(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		wantErr     bool
	}{
		{name: "application json", contentType: "application/json"},
		{name: "application json with charset", contentType: "application/json; charset=utf-8"},
		{name: "structured json", contentType: "application/token-introspection+jwt+json"},
		{name: "missing content type", contentType: "", wantErr: true},
		{name: "text plain", contentType: "text/plain", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.contentType != "" {
					w.Header().Set("Content-Type", tt.contentType)
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"active":true,"sub":"user123"}`))
			}))
			defer server.Close()

			authn := newExternalBearerForTest(t, authimpl.ExternalBearerAuthenticatorConfig{
				URL:                               server.URL,
				AllowUnauthenticatedIntrospection: true,
			})
			headers := http.Header{"Authorization": []string{"Bearer inbound-token"}}
			_, err := authn.Authenticate(context.Background(), headers, url.Values{})
			if tt.wantErr && err == nil {
				t.Fatal("expected authentication error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected authentication error: %v", err)
			}
		})
	}
}

func TestExternalBearerAuthenticatorUsesBasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("client:secret"))
		if got := r.Header.Get("Authorization"); got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"active": true, "sub": "user123"})
	}))
	defer server.Close()

	authn := newExternalBearerForTest(t, authimpl.ExternalBearerAuthenticatorConfig{
		URL:          server.URL,
		ClientID:     "client",
		ClientSecret: "secret",
	})
	headers := http.Header{"Authorization": []string{"Bearer inbound-token"}}
	if _, err := authn.Authenticate(context.Background(), headers, url.Values{}); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
}

func TestExternalBearerAuthenticatorFailClosed(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{name: "non 2xx", statusCode: http.StatusUnauthorized, body: `{"active":true,"sub":"user123"}`},
		{name: "malformed json", statusCode: http.StatusOK, body: `{not-json`},
		{name: "trailing json garbage", statusCode: http.StatusOK, body: `{"active":true,"sub":"user123"} trailing`},
		{name: "missing active", statusCode: http.StatusOK, body: `{"sub":"user123"}`},
		{name: "false active", statusCode: http.StatusOK, body: `{"active":false,"sub":"user123"}`},
		{name: "missing identity", statusCode: http.StatusOK, body: `{"active":true}`},
		{name: "expired exp", statusCode: http.StatusOK, body: fmt.Sprintf(`{"active":true,"sub":"user123","exp":%d}`, time.Now().Add(-time.Hour).Unix())},
		{name: "oversized response", statusCode: http.StatusOK, body: strings.Repeat(" ", 64*1024+1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			authn := newExternalBearerForTest(t, authimpl.ExternalBearerAuthenticatorConfig{
				URL:                               server.URL,
				AllowUnauthenticatedIntrospection: true,
			})
			headers := http.Header{"Authorization": []string{"Bearer inbound-token"}}
			if _, err := authn.Authenticate(context.Background(), headers, url.Values{}); err == nil {
				t.Fatal("expected authentication error, got nil")
			}
		})
	}
}

func TestExternalBearerAuthenticatorRedirectFailsClosed(t *testing.T) {
	redirected := false
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected = true
		writeJSON(t, w, http.StatusOK, map[string]any{"active": true, "sub": "user123"})
	}))
	defer redirectTarget.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	authn := newExternalBearerForTest(t, authimpl.ExternalBearerAuthenticatorConfig{
		URL:                               server.URL,
		AllowUnauthenticatedIntrospection: true,
	})
	headers := http.Header{"Authorization": []string{"Bearer inbound-token"}}
	if _, err := authn.Authenticate(context.Background(), headers, url.Values{}); err == nil {
		t.Fatal("expected redirect authentication error, got nil")
	}
	if redirected {
		t.Fatal("introspection client followed redirect")
	}
}

func TestExternalBearerAuthenticatorTimeoutFailsClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"active":true,"sub":"user123"}`))
	}))
	defer server.Close()

	authn := newExternalBearerForTest(t, authimpl.ExternalBearerAuthenticatorConfig{
		URL:                               server.URL,
		Timeout:                           10 * time.Millisecond,
		AllowUnauthenticatedIntrospection: true,
	})
	headers := http.Header{"Authorization": []string{"Bearer inbound-token"}}
	if _, err := authn.Authenticate(context.Background(), headers, url.Values{}); err == nil {
		t.Fatal("expected timeout authentication error, got nil")
	}
}

func TestExternalBearerAuthenticatorInboundBearerParsing(t *testing.T) {
	var gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			t.Errorf("body is not form encoded: %v", err)
		}
		gotToken = form.Get("token")
		writeJSON(t, w, http.StatusOK, map[string]any{"active": true, "sub": "user123"})
	}))
	defer server.Close()

	authn := newExternalBearerForTest(t, authimpl.ExternalBearerAuthenticatorConfig{
		URL:                               server.URL,
		AllowUnauthenticatedIntrospection: true,
	})

	accepted := []struct {
		name       string
		authHeader string
		wantToken  string
	}{
		{name: "standard Bearer", authHeader: "Bearer inbound-token", wantToken: "inbound-token"},
		{name: "lowercase bearer", authHeader: "bearer lower-token", wantToken: "lower-token"},
		{name: "uppercase bearer with token whitespace", authHeader: "BEARER   spaced-token  ", wantToken: "spaced-token"},
		{name: "tab whitespace", authHeader: "Bearer\ttab-token", wantToken: "tab-token"},
	}
	for _, tt := range accepted {
		t.Run(tt.name, func(t *testing.T) {
			gotToken = ""
			headers := http.Header{"Authorization": []string{tt.authHeader}}
			if _, err := authn.Authenticate(context.Background(), headers, url.Values{}); err != nil {
				t.Fatalf("unexpected authentication error: %v", err)
			}
			if gotToken != tt.wantToken {
				t.Fatalf("introspection token = %q, want %q", gotToken, tt.wantToken)
			}
		})
	}

	for _, authHeader := range []string{"", "Basic abc", "Bearer ", "bearer    ", "Bearer token extra"} {
		t.Run("reject "+authHeader, func(t *testing.T) {
			headers := http.Header{}
			if authHeader != "" {
				headers.Set("Authorization", authHeader)
			}
			if _, err := authn.Authenticate(context.Background(), headers, url.Values{}); err == nil {
				t.Fatal("expected authentication error, got nil")
			}
		})
	}
}

func TestExternalBearerAuthenticatorIdentityFallback(t *testing.T) {
	tests := []struct {
		name        string
		userIDClaim string
		claims      map[string]any
		wantUserID  string
	}{
		{
			name:        "configured claim wins before username",
			userIDClaim: "email",
			claims:      map[string]any{"active": true, "username": "username-user", "email": "email-user", "sub": "sub-user"},
			wantUserID:  "email-user",
		},
		{
			name:        "username fallback after missing configured claim",
			userIDClaim: "email",
			claims:      map[string]any{"active": true, "username": "username-user", "sub": "sub-user"},
			wantUserID:  "username-user",
		},
		{
			name:        "configured claim used before sub fallback",
			userIDClaim: "email",
			claims:      map[string]any{"active": true, "email": "email-user", "sub": "sub-user"},
			wantUserID:  "email-user",
		},
		{
			name:        "sub fallback",
			userIDClaim: "email",
			claims:      map[string]any{"active": true, "sub": "sub-user"},
			wantUserID:  "sub-user",
		},
		{
			name:        "subject fallback",
			userIDClaim: "email",
			claims:      map[string]any{"active": true, "subject": "subject-user"},
			wantUserID:  "subject-user",
		},
		{
			name:        "default sub claim wins before username fallback",
			userIDClaim: "",
			claims:      map[string]any{"active": true, "username": "username-user", "sub": "sub-user"},
			wantUserID:  "sub-user",
		},
		{
			name:        "configured subject claim wins before username fallback",
			userIDClaim: "subject",
			claims:      map[string]any{"active": true, "username": "username-user", "sub": "sub-user", "subject": "subject-user"},
			wantUserID:  "subject-user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusOK, tt.claims)
			}))
			defer server.Close()

			authn := newExternalBearerForTest(t, authimpl.ExternalBearerAuthenticatorConfig{
				URL:                               server.URL,
				AllowUnauthenticatedIntrospection: true,
				UserIDClaim:                       tt.userIDClaim,
			})
			headers := http.Header{"Authorization": []string{"Bearer inbound-token"}}
			session, err := authn.Authenticate(context.Background(), headers, url.Values{})
			if err != nil {
				t.Fatalf("Authenticate() error = %v", err)
			}
			if got := session.Principal().User.ID; got != tt.wantUserID {
				t.Fatalf("User.ID = %q, want %q", got, tt.wantUserID)
			}
		})
	}
}

func TestExternalBearerAuthenticatorDoesNotTrustInboundIdentityHeadersOrQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"active": true})
	}))
	defer server.Close()

	authn := newExternalBearerForTest(t, authimpl.ExternalBearerAuthenticatorConfig{
		URL:                               server.URL,
		AllowUnauthenticatedIntrospection: true,
	})
	headers := http.Header{
		"Authorization": []string{"Bearer inbound-token"},
		"X-User-Id":     []string{"header-user"},
		"X-Agent-Name":  []string{"kagent/agent"},
	}
	query := url.Values{"user_id": []string{"query-user"}}
	if _, err := authn.Authenticate(context.Background(), headers, query); err == nil {
		t.Fatal("expected authentication error without identity in introspection claims, got nil")
	}
}

func TestExternalBearerAuthenticatorClaimsDoNotExposeRawBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"active":           true,
			"sub":              "user123",
			"token":            "inbound-token",
			"access_token":     "inbound-token",
			"auth_header_echo": "Bearer inbound-token",
			"debug":            "prefix inbound-token suffix",
			"safe_claim":       "safe inbound claim",
			"nested": map[string]any{
				"token":      "inbound-token",
				"authHeader": "Bearer inbound-token",
				"safe":       "preserved",
			},
			"token_list": []string{"safe", "inbound-token", "Bearer inbound-token"},
		})
	}))
	defer server.Close()

	authn := newExternalBearerForTest(t, authimpl.ExternalBearerAuthenticatorConfig{
		URL:                               server.URL,
		AllowUnauthenticatedIntrospection: true,
	})
	headers := http.Header{"Authorization": []string{"Bearer inbound-token"}}
	session, err := authn.Authenticate(context.Background(), headers, url.Values{})
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	claims := session.Principal().Claims
	if claimContainsString(claims, "inbound-token") {
		t.Fatalf("Principal.Claims exposes raw inbound bearer token: %#v", claims)
	}
	assertClaimString(t, claims, "safe_claim", "safe inbound claim")
	nested, ok := claims["nested"].(map[string]any)
	if !ok {
		t.Fatalf("Claims[nested] = %#v, want map", claims["nested"])
	}
	assertClaimString(t, nested, "safe", "preserved")
	tokenList, ok := claims["token_list"].([]any)
	if !ok {
		t.Fatalf("Claims[token_list] = %#v, want list", claims["token_list"])
	}
	if len(tokenList) != 1 || tokenList[0] != "safe" {
		t.Fatalf("Claims[token_list] = %#v, want only safe entry", tokenList)
	}
}

func TestExternalBearerAuthenticatorAudStringAndListAreAcceptedAndPreserved(t *testing.T) {
	tests := []struct {
		name string
		aud  any
	}{
		{name: "string aud", aud: "kagent"},
		{name: "list aud", aud: []string{"kagent", "agents"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusOK, map[string]any{"active": true, "sub": "user123", "aud": tt.aud})
			}))
			defer server.Close()

			authn := newExternalBearerForTest(t, authimpl.ExternalBearerAuthenticatorConfig{
				URL:                               server.URL,
				AllowUnauthenticatedIntrospection: true,
			})
			headers := http.Header{"Authorization": []string{"Bearer inbound-token"}}
			session, err := authn.Authenticate(context.Background(), headers, url.Values{})
			if err != nil {
				t.Fatalf("Authenticate() error = %v", err)
			}
			if _, ok := session.Principal().Claims["aud"]; !ok {
				t.Fatal("Claims[aud] missing")
			}
		})
	}
}

func TestExternalBearerAuthenticatorPolicyValidation(t *testing.T) {
	tests := []struct {
		name    string
		policy  string
		wantErr string
	}{
		{name: "invalid JSON policy rejected", policy: `{`, wantErr: "invalid external-bearer policy file"},
		{name: "trailing JSON policy rejected", policy: `{} {}`, wantErr: "trailing JSON after policy object"},
		{name: "unknown top-level field rejected", policy: `{"allowedAudience":["kagent"]}`, wantErr: "unknown field"},
		{
			name: "empty allOf rejected",
			policy: `{
				"serviceActors": {
					"svc": {
						"match": {"allOf": []},
						"allowedA2A": [
							{"namespace": "kagent", "name": "agent", "workloadType": "agent"}
						]
					}
				}
			}`,
			wantErr: "match.allOf must contain",
		},
		{
			name: "predicate with both value and contains rejected",
			policy: `{
				"serviceActors": {
					"svc": {
						"match": {"allOf": [
							{"claim": "client_id", "value": "svc", "contains": "svc"},
							{"claim": "grant_type", "value": "client_credentials"}
						]},
						"allowedA2A": [
							{"namespace": "kagent", "name": "agent", "workloadType": "agent"}
						]
					}
				}
			}`,
			wantErr: "exactly one operator",
		},
		{
			name: "empty predicate value rejected",
			policy: `{
				"serviceActors": {
					"svc": {
						"match": {"allOf": [
							{"claim": "client_id", "value": "svc"},
							{"claim": "grant_type", "value": ""}
						]},
						"allowedA2A": [
							{"namespace": "kagent", "name": "agent", "workloadType": "agent"}
						]
					}
				}
			}`,
			wantErr: "predicate value must not be empty",
		},
		{
			name: "empty predicate contains rejected",
			policy: `{
				"serviceActors": {
					"svc": {
						"match": {"allOf": [
							{"claim": "client_id", "value": "svc"},
							{"claim": "grant_type", "value": "client_credentials"},
							{"claim": "scope", "contains": ""}
						]},
						"allowedA2A": [
							{"namespace": "kagent", "name": "agent", "workloadType": "agent"}
						]
					}
				}
			}`,
			wantErr: "predicate contains value must not be empty",
		},
		{
			name: "unknown predicate operator rejected",
			policy: `{
				"serviceActors": {
					"svc": {
						"match": {"allOf": [
							{"claim": "client_id", "equals": "svc"},
							{"claim": "grant_type", "value": "client_credentials"}
						]},
						"allowedA2A": [
							{"namespace": "kagent", "name": "agent", "workloadType": "agent"}
						]
					}
				}
			}`,
			wantErr: "unknown external-bearer policy predicate",
		},
		{
			name: "unknown workloadType rejected",
			policy: `{
				"serviceActors": {
					"svc": {
						"match": {"allOf": [
							{"claim": "client_id", "value": "svc"},
							{"claim": "grant_type", "value": "client_credentials"}
						]},
						"allowedA2A": [
							{"namespace": "kagent", "name": "agent", "workloadType": "deployment"}
						]
					}
				}
			}`,
			wantErr: "unknown workloadType",
		},
		{
			name: "partial wildcard rejected",
			policy: `{
				"serviceActors": {
					"svc": {
						"match": {"allOf": [
							{"claim": "client_id", "value": "svc"},
							{"claim": "grant_type", "value": "client_credentials"}
						]},
						"allowedA2A": [
							{"namespace": "kagent*", "name": "agent", "workloadType": "agent"}
						]
					}
				}
			}`,
			wantErr: "partial wildcard",
		},
		{
			name: "client_id-only service actor rejected",
			policy: `{
				"serviceActors": {
					"svc": {
						"match": {"allOf": [
							{"claim": "client_id", "value": "svc"}
						]},
						"allowedA2A": [
							{"namespace": "kagent", "name": "agent", "workloadType": "agent"}
						]
					}
				}
			}`,
			wantErr: "client_id alone",
		},
		{
			name: "single non-client_id predicate rejected",
			policy: `{
				"serviceActors": {
					"svc": {
						"match": {"allOf": [
							{"claim": "scope", "contains": "kagent:a2a"}
						]},
						"allowedA2A": [
							{"namespace": "kagent", "name": "agent", "workloadType": "agent"}
						]
					}
				}
			}`,
			wantErr: "at least two predicates",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := authimpl.NewExternalBearerAuthenticator(authimpl.ExternalBearerAuthenticatorConfig{
				URL:                               "http://localhost/introspect",
				AllowUnauthenticatedIntrospection: true,
				PolicyFile:                        writePolicyFile(t, tt.policy),
			})
			if err == nil {
				t.Fatal("expected construction error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestExternalBearerAuthenticatorPolicyTopLevelChecks(t *testing.T) {
	tests := []struct {
		name    string
		policy  string
		claims  map[string]any
		wantErr bool
	}{
		{name: "requiredScopes pass exact token", policy: `{"requiredScopes":["kagent:a2a"]}`, claims: map[string]any{"active": true, "sub": "user123", "scope": "read kagent:a2a"}},
		{name: "requiredScopes fail exact token", policy: `{"requiredScopes":["kagent:a2a"]}`, claims: map[string]any{"active": true, "sub": "user123", "scope": "kagent:a2a-extra"}, wantErr: true},
		{name: "requiredScopes pass array form", policy: `{"requiredScopes":["kagent:a2a"]}`, claims: map[string]any{"active": true, "sub": "user123", "scope": []string{"read", "kagent:a2a"}}},
		{name: "allowedAudiences pass string", policy: `{"allowedAudiences":["kagent"]}`, claims: map[string]any{"active": true, "sub": "user123", "aud": "kagent"}},
		{name: "allowedAudiences pass list", policy: `{"allowedAudiences":["kagent"]}`, claims: map[string]any{"active": true, "sub": "user123", "aud": []string{"other", "kagent"}}},
		{name: "allowedAudiences missing aud fails", policy: `{"allowedAudiences":["kagent"]}`, claims: map[string]any{"active": true, "sub": "user123"}, wantErr: true},
		{name: "allowedIssuers pass", policy: `{"allowedIssuers":["https://issuer.example.com"]}`, claims: map[string]any{"active": true, "sub": "user123", "iss": "https://issuer.example.com"}},
		{name: "allowedIssuers fail", policy: `{"allowedIssuers":["https://issuer.example.com"]}`, claims: map[string]any{"active": true, "sub": "user123", "iss": "https://evil.example.com"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authn, err := newExternalBearerForClaims(t, tt.claims, authimpl.ExternalBearerAuthenticatorConfig{PolicyFile: writePolicyFile(t, tt.policy)})
			if err != nil {
				t.Fatalf("NewExternalBearerAuthenticator() error = %v", err)
			}
			_, err = authn.Authenticate(context.Background(), http.Header{"Authorization": []string{"Bearer inbound-token"}}, url.Values{})
			if tt.wantErr && err == nil {
				t.Fatal("expected authentication error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected authentication error: %v", err)
			}
		})
	}
}

func TestExternalBearerAuthenticatorServiceActorClassificationAndA2AAccess(t *testing.T) {
	policy := `{
		"serviceActors": {
			"svc-a": {
				"match": {"allOf": [
					{"claim": "client_id", "value": "svc-client"},
					{"claim": "grant_type", "value": "client_credentials"},
					{"claim": "scope", "contains": "kagent:a2a"}
				]},
				"allowedA2A": [
					{"namespace": "kagent", "name": "example-agent", "workloadType": "agent"},
					{"namespace": "observability", "name": "*", "workloadType": "*"}
				]
			}
		}
	}`
	claims := map[string]any{
		"active": true, "sub": "service-subject", "client_id": "svc-client",
		"grant_type": "client_credentials", "scope": "read kagent:a2a",
	}
	authn, err := newExternalBearerForClaims(t, claims, authimpl.ExternalBearerAuthenticatorConfig{PolicyFile: writePolicyFile(t, policy)})
	if err != nil {
		t.Fatalf("NewExternalBearerAuthenticator() error = %v", err)
	}
	session, err := authn.Authenticate(context.Background(), http.Header{"Authorization": []string{"Bearer inbound-token"}}, url.Values{})
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if got := session.Principal().User.ID; got != "" {
		t.Fatalf("service actor User.ID = %q, want empty", got)
	}

	accessTests := []struct {
		name    string
		target  auth.A2ATarget
		wantErr bool
	}{
		{name: "allows matching target", target: auth.A2ATarget{Namespace: "kagent", Name: "example-agent", WorkloadType: auth.A2AWorkloadAgent}},
		{name: "denies non-matching namespace", target: auth.A2ATarget{Namespace: "other", Name: "example-agent", WorkloadType: auth.A2AWorkloadAgent}, wantErr: true},
		{name: "denies non-matching name", target: auth.A2ATarget{Namespace: "kagent", Name: "other-agent", WorkloadType: auth.A2AWorkloadAgent}, wantErr: true},
		{name: "denies non-matching workloadType", target: auth.A2ATarget{Namespace: "kagent", Name: "example-agent", WorkloadType: auth.A2AWorkloadSandbox}, wantErr: true},
		{name: "supports whole-field wildcard", target: auth.A2ATarget{Namespace: "observability", Name: "any-agent", WorkloadType: auth.A2AWorkloadSandbox}},
	}
	for _, tt := range accessTests {
		t.Run(tt.name, func(t *testing.T) {
			err := authn.CheckA2AAccess(context.Background(), session, tt.target)
			if tt.wantErr && err == nil {
				t.Fatal("expected access error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected access error: %v", err)
			}
		})
	}

	userAuthn, err := newExternalBearerForClaims(t, map[string]any{"active": true, "sub": "user123"}, authimpl.ExternalBearerAuthenticatorConfig{PolicyFile: writePolicyFile(t, policy)})
	if err != nil {
		t.Fatalf("NewExternalBearerAuthenticator() for user error = %v", err)
	}
	userSession, err := userAuthn.Authenticate(context.Background(), http.Header{"Authorization": []string{"Bearer user-token"}}, url.Values{})
	if err != nil {
		t.Fatalf("Authenticate() user error = %v", err)
	}
	if err := userAuthn.CheckA2AAccess(context.Background(), userSession, auth.A2ATarget{Namespace: "other", Name: "agent", WorkloadType: auth.A2AWorkloadAgent}); err != nil {
		t.Fatalf("user actor CheckA2AAccess() error = %v", err)
	}
	if err := userAuthn.CheckA2AAccess(context.Background(), nil, auth.A2ATarget{Namespace: "other", Name: "agent", WorkloadType: auth.A2AWorkloadAgent}); err == nil {
		t.Fatal("expected nil session to be denied")
	}
	if err := userAuthn.CheckA2AAccess(context.Background(), staticSession{principal: auth.Principal{User: auth.User{ID: "foreign-user"}}}, auth.A2ATarget{Namespace: "other", Name: "agent", WorkloadType: auth.A2AWorkloadAgent}); err == nil {
		t.Fatal("expected foreign session to be denied")
	}
}

func TestExternalBearerAuthenticatorRejectsAmbiguousServiceActorMatch(t *testing.T) {
	policy := `{
		"serviceActors": {
			"svc-a": {
				"match": {"allOf": [
					{"claim": "client_id", "value": "svc-client"},
					{"claim": "grant_type", "value": "client_credentials"}
				]},
				"allowedA2A": [
					{"namespace": "kagent", "name": "agent-a", "workloadType": "agent"}
				]
			},
			"svc-b": {
				"match": {"allOf": [
					{"claim": "client_id", "value": "svc-client"},
					{"claim": "grant_type", "value": "client_credentials"}
				]},
				"allowedA2A": [
					{"namespace": "kagent", "name": "agent-b", "workloadType": "agent"}
				]
			}
		}
	}`
	authn, err := newExternalBearerForClaims(t, map[string]any{"active": true, "client_id": "svc-client", "grant_type": "client_credentials"}, authimpl.ExternalBearerAuthenticatorConfig{PolicyFile: writePolicyFile(t, policy)})
	if err != nil {
		t.Fatalf("NewExternalBearerAuthenticator() error = %v", err)
	}
	if _, err := authn.Authenticate(context.Background(), http.Header{"Authorization": []string{"Bearer service-token"}}, url.Values{}); err == nil {
		t.Fatal("expected ambiguous service actor match to be rejected")
	}
}

func TestExternalBearerAuthenticatorCustomServiceTokenIndicatorFallthrough(t *testing.T) {
	policy := `{
		"serviceActors": {
			"svc": {
				"match": {"allOf": [
					{"claim": "client_id", "value": "svc-client"},
					{"claim": "token_class", "value": "service"},
					{"claim": "scope", "contains": "kagent:a2a"}
				]},
				"allowedA2A": [
					{"namespace": "kagent", "name": "agent", "workloadType": "agent"}
				]
			}
		}
	}`
	authn, err := newExternalBearerForClaims(t, map[string]any{"active": true, "sub": "service-subject", "username": "service-looking-user", "client_id": "other-client", "token_class": "service", "scope": "kagent:a2a"}, authimpl.ExternalBearerAuthenticatorConfig{PolicyFile: writePolicyFile(t, policy)})
	if err != nil {
		t.Fatalf("NewExternalBearerAuthenticator() error = %v", err)
	}
	if _, err := authn.Authenticate(context.Background(), http.Header{"Authorization": []string{"Bearer service-token"}}, url.Values{}); err == nil {
		t.Fatal("expected token matching configured service-token indicator but not full service actor policy to be rejected")
	}

	userAuthn, err := newExternalBearerForClaims(t, map[string]any{"active": true, "username": "alice@example.com", "sub": "alice-sub", "client_id": "web-client", "scope": "kagent:a2a"}, authimpl.ExternalBearerAuthenticatorConfig{PolicyFile: writePolicyFile(t, policy), UserIDClaim: "username"})
	if err != nil {
		t.Fatalf("NewExternalBearerAuthenticator() for user error = %v", err)
	}
	session, err := userAuthn.Authenticate(context.Background(), http.Header{"Authorization": []string{"Bearer user-token"}}, url.Values{})
	if err != nil {
		t.Fatalf("ordinary user token without service-token indicator should be accepted: %v", err)
	}
	if got := session.Principal().User.ID; got != "alice@example.com" {
		t.Fatalf("User.ID = %q, want alice@example.com", got)
	}
}

func TestExternalBearerAuthenticatorServiceActorFallthrough(t *testing.T) {
	policy := `{
		"serviceActors": {
			"svc": {
				"match": {"allOf": [
					{"claim": "client_id", "value": "svc-client"},
					{"claim": "grant_type", "value": "client_credentials"},
					{"claim": "scope", "contains": "kagent:a2a"}
				]},
				"allowedA2A": [
					{"namespace": "kagent", "name": "agent", "workloadType": "agent"}
				]
			}
		}
	}`

	authn, err := newExternalBearerForClaims(t, map[string]any{"active": true, "sub": "service-subject", "client_id": "svc-client", "grant_type": "client_credentials", "scope": "read"}, authimpl.ExternalBearerAuthenticatorConfig{PolicyFile: writePolicyFile(t, policy)})
	if err != nil {
		t.Fatalf("NewExternalBearerAuthenticator() error = %v", err)
	}
	if _, err := authn.Authenticate(context.Background(), http.Header{"Authorization": []string{"Bearer service-token"}}, url.Values{}); err == nil {
		t.Fatal("expected unmatched client_credentials token with sub to be rejected")
	}

	userAuthn, err := newExternalBearerForClaims(t, map[string]any{"active": true, "username": "alice@example.com", "client_id": "web-client", "grant_type": "authorization_code"}, authimpl.ExternalBearerAuthenticatorConfig{PolicyFile: writePolicyFile(t, policy), UserIDClaim: "username"})
	if err != nil {
		t.Fatalf("NewExternalBearerAuthenticator() for user error = %v", err)
	}
	session, err := userAuthn.Authenticate(context.Background(), http.Header{"Authorization": []string{"Bearer user-token"}}, url.Values{})
	if err != nil {
		t.Fatalf("user token without service actor match should be accepted: %v", err)
	}
	if got := session.Principal().User.ID; got != "alice@example.com" {
		t.Fatalf("User.ID = %q, want alice@example.com", got)
	}
}

func TestExternalBearerAuthenticatorUpstreamAuth(t *testing.T) {
	tests := []struct {
		name        string
		propagate   bool
		wantAuthz   string
		wantUserID  string
		preexisting string
	}{
		{
			name:        "user session with PropagateToken=false sets X-User-Id only",
			propagate:   false,
			wantUserID:  "user123",
			preexisting: "Bearer stale-preexisting-token",
		},
		{
			name:        "user session with PropagateToken=true sets X-User-Id and forwards bearer",
			propagate:   true,
			wantAuthz:   "Bearer inbound-token",
			wantUserID:  "user123",
			preexisting: "Bearer stale-preexisting-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusOK, map[string]any{"active": true, "sub": "user123"})
			}))
			defer server.Close()

			authn := newExternalBearerForTest(t, authimpl.ExternalBearerAuthenticatorConfig{
				URL:                               server.URL,
				PropagateToken:                    tt.propagate,
				AllowUnauthenticatedIntrospection: true,
			})
			headers := http.Header{"Authorization": []string{"Bearer inbound-token"}}
			session, err := authn.Authenticate(context.Background(), headers, url.Values{})
			if err != nil {
				t.Fatalf("Authenticate() error = %v", err)
			}

			req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			if tt.preexisting != "" {
				req.Header.Set("Authorization", tt.preexisting)
			}
			if err := authn.UpstreamAuth(req, session, session.Principal()); err != nil {
				t.Fatalf("UpstreamAuth() error = %v", err)
			}
			if got := req.Header.Get("X-User-Id"); got != tt.wantUserID {
				t.Fatalf("X-User-Id = %q, want %q", got, tt.wantUserID)
			}
			if got := req.Header.Get("Authorization"); got != tt.wantAuthz {
				t.Fatalf("Authorization = %q, want %q", got, tt.wantAuthz)
			}
		})
	}
}

func TestExternalBearerAuthenticatorUpstreamAuthNoopForNilAndEmptyPrincipal(t *testing.T) {
	authn := newExternalBearerForTest(t, authimpl.ExternalBearerAuthenticatorConfig{
		URL:                               "http://localhost:8080/introspect",
		AllowUnauthenticatedIntrospection: true,
	})

	for _, tt := range []struct {
		name    string
		session auth.Session
	}{
		{name: "nil session"},
		{name: "empty principal", session: staticSession{principal: auth.Principal{}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			if err := authn.UpstreamAuth(req, tt.session, auth.Principal{}); err != nil {
				t.Fatalf("UpstreamAuth() error = %v", err)
			}
			if got := req.Header.Get("X-User-Id"); got != "" {
				t.Fatalf("X-User-Id = %q, want empty", got)
			}
			if got := req.Header.Get("Authorization"); got != "" {
				t.Fatalf("Authorization = %q, want empty", got)
			}
		})
	}
}

func newExternalBearerForClaims(t *testing.T, claims map[string]any, cfg authimpl.ExternalBearerAuthenticatorConfig) (*authimpl.ExternalBearerAuthenticator, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, claims)
	}))
	t.Cleanup(server.Close)
	cfg.URL = server.URL
	cfg.AllowUnauthenticatedIntrospection = true
	return authimpl.NewExternalBearerAuthenticator(cfg)
}

func writePolicyFile(t *testing.T, policy string) string {
	t.Helper()
	path := t.TempDir() + "/policy.json"
	if err := os.WriteFile(path, []byte(policy), 0o600); err != nil {
		t.Fatalf("writing policy file: %v", err)
	}
	return path
}

func newExternalBearerForTest(t *testing.T, cfg authimpl.ExternalBearerAuthenticatorConfig) *authimpl.ExternalBearerAuthenticator {
	t.Helper()
	authn, err := authimpl.NewExternalBearerAuthenticator(cfg)
	if err != nil {
		t.Fatalf("NewExternalBearerAuthenticator() error = %v", err)
	}
	return authn
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encoding response JSON: %v", err)
	}
}

func assertClaimString(t *testing.T, claims map[string]any, claim string, want string) {
	t.Helper()
	got, ok := claims[claim].(string)
	if !ok || got != want {
		t.Fatalf("Claims[%s] = %#v, want %q", claim, claims[claim], want)
	}
}

type staticSession struct {
	principal auth.Principal
}

func (s staticSession) Principal() auth.Principal {
	return s.principal
}

func claimContainsString(value any, want string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, want)
	case []any:
		for _, item := range typed {
			if claimContainsString(item, want) {
				return true
			}
		}
	case map[string]any:
		for _, item := range typed {
			if claimContainsString(item, want) {
				return true
			}
		}
	}
	return false
}
