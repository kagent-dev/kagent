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
	"strings"
	"testing"
	"time"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
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
			name: "localhost http with explicit unauthenticated opt-in accepted",
			cfg: authimpl.ExternalBearerAuthenticatorConfig{
				URL:                               "http://localhost:8080/introspect",
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"active":     true,
			"grant_type": "client_credentials",
			"username":   "service-looking-user",
			"sub":        "service-subject",
		})
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
			name:        "username wins before configured claim",
			userIDClaim: "email",
			claims:      map[string]any{"active": true, "username": "username-user", "email": "email-user", "sub": "sub-user"},
			wantUserID:  "username-user",
		},
		{
			name:        "configured claim used after username",
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

func TestExternalBearerAuthenticatorUpstreamAuth(t *testing.T) {
	for _, propagate := range []bool{false, true} {
		t.Run(fmt.Sprintf("propagate=%t", propagate), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusOK, map[string]any{"active": true, "sub": "user123"})
			}))
			defer server.Close()

			authn := newExternalBearerForTest(t, authimpl.ExternalBearerAuthenticatorConfig{
				URL:                               server.URL,
				PropagateToken:                    propagate,
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
			req.Header.Set("Authorization", "Bearer stale-preexisting-token")
			if err := authn.UpstreamAuth(req, session, session.Principal()); err != nil {
				t.Fatalf("UpstreamAuth() error = %v", err)
			}
			if got := req.Header.Get("X-User-Id"); got != "user123" {
				t.Fatalf("X-User-Id = %q, want user123", got)
			}
			wantAuth := ""
			if propagate {
				wantAuth = "Bearer inbound-token"
			}
			if got := req.Header.Get("Authorization"); got != wantAuth {
				t.Fatalf("Authorization = %q, want %q", got, wantAuth)
			}
		})
	}
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
