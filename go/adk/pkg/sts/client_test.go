package sts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newMockSTSClientServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":         srv.URL,
				"token_endpoint": srv.URL + "/token",
			})
			return
		}
		handler(w, r)
	}))
	return srv
}

func TestSTSClientImpersonateSuccess(t *testing.T) {
	t.Parallel()
	srv := newMockSTSClientServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		if got := r.FormValue("subject_token"); got != "subject" {
			t.Fatalf("subject_token = %q, want %q", got, "subject")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":      "access-token",
			"issued_token_type": string(TokenTypeJWT),
			"token_type":        "Bearer",
			"expires_in":        3600,
		})
	})
	defer srv.Close()

	client := NewSTSClient(STSConfig{
		WellKnownURI: srv.URL + "/.well-known/oauth-authorization-server",
		Timeout:      2,
	})
	resp, err := client.Impersonate(context.Background(), "subject", TokenTypeJWT, nil, nil, "", "", nil)
	if err != nil {
		t.Fatalf("Impersonate() error = %v", err)
	}
	if resp.AccessToken != "access-token" {
		t.Fatalf("AccessToken = %q, want %q", resp.AccessToken, "access-token")
	}
}

func TestSTSClientDelegateBuildsRequestData(t *testing.T) {
	t.Parallel()
	srv := newMockSTSClientServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		assertFormValue(t, r.Form, "grant_type", string(GrantTypeTokenExchange))
		assertFormValue(t, r.Form, "subject_token", "subject-token")
		assertFormValue(t, r.Form, "subject_token_type", string(TokenTypeJWT))
		assertFormValue(t, r.Form, "actor_token", "actor-token")
		assertFormValue(t, r.Form, "actor_token_type", string(TokenTypeJWT))
		assertFormValue(t, r.Form, "audience", "https://api.example.com")
		assertFormValue(t, r.Form, "scope", "read write")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":      "delegated-token",
			"issued_token_type": string(TokenTypeJWT),
			"token_type":        "Bearer",
		})
	})
	defer srv.Close()

	client := NewSTSClient(STSConfig{
		WellKnownURI: srv.URL + "/.well-known/oauth-authorization-server",
		Timeout:      2,
	})
	_, err := client.Delegate(
		context.Background(),
		"subject-token",
		TokenTypeJWT,
		"actor-token",
		TokenTypeJWT,
		nil,
		"https://api.example.com",
		"read write",
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("Delegate() error = %v", err)
	}
}

func TestSTSClientExchangeTokenErrorResponse(t *testing.T) {
	t.Parallel()
	srv := newMockSTSClientServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_request",
			"error_description": "missing required parameter",
		})
	})
	defer srv.Close()

	client := NewSTSClient(STSConfig{
		WellKnownURI: srv.URL + "/.well-known/oauth-authorization-server",
		Timeout:      2,
	})
	_, err := client.Impersonate(context.Background(), "subject", TokenTypeJWT, nil, nil, "", "", nil)
	if err == nil {
		t.Fatalf("Impersonate() error = nil, want non-nil")
	}
	exchangeErr, ok := err.(*TokenExchangeError)
	if !ok {
		t.Fatalf("error type = %T, want *TokenExchangeError", err)
	}
	if exchangeErr.ErrorCode != "invalid_request" {
		t.Fatalf("ErrorCode = %q, want %q", exchangeErr.ErrorCode, "invalid_request")
	}
}

func TestSTSClientNetworkError(t *testing.T) {
	t.Parallel()
	// Use a closed local listener address to trigger a network failure quickly.
	client := NewSTSClient(STSConfig{
		WellKnownURI: "http://127.0.0.1:1/.well-known/oauth-authorization-server",
		Timeout:      1,
	})
	_, err := client.Impersonate(context.Background(), "subject", TokenTypeJWT, nil, nil, "", "", nil)
	if err == nil {
		t.Fatalf("Impersonate() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "failed to fetch well-known configuration") &&
		!strings.Contains(err.Error(), "network error") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewSTSClientAppliesSecureDefaults(t *testing.T) {
	t.Parallel()
	client := NewSTSClient(STSConfig{
		WellKnownURI: "http://example.com/.well-known/oauth-authorization-server",
	})

	if client.config.Timeout != 5 {
		t.Fatalf("Timeout = %d, want 5", client.config.Timeout)
	}
	if client.config.VerifySSL == nil || !*client.config.VerifySSL {
		t.Fatalf("VerifySSL = %v, want true", client.config.VerifySSL)
	}
}

func TestSTSClientInitializeRetriesAfterDiscoveryFailure(t *testing.T) {
	t.Parallel()
	wellKnownCalls := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			wellKnownCalls++
			if wellKnownCalls == 1 {
				http.Error(w, "temporary failure", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":         srv.URL,
				"token_endpoint": srv.URL + "/token",
			})
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":      "access-token",
				"issued_token_type": string(TokenTypeJWT),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewSTSClient(STSConfig{
		WellKnownURI: srv.URL + "/.well-known/oauth-authorization-server",
		Timeout:      2,
	})

	if _, err := client.Impersonate(context.Background(), "subject", TokenTypeJWT, nil, nil, "", "", nil); err == nil {
		t.Fatalf("first Impersonate() error = nil, want discovery error")
	}
	resp, err := client.Impersonate(context.Background(), "subject", TokenTypeJWT, nil, nil, "", "", nil)
	if err != nil {
		t.Fatalf("second Impersonate() error = %v", err)
	}
	if resp.AccessToken != "access-token" {
		t.Fatalf("AccessToken = %q, want %q", resp.AccessToken, "access-token")
	}
	if wellKnownCalls != 2 {
		t.Fatalf("well-known calls = %d, want 2", wellKnownCalls)
	}
}

func TestTransportWithTLSVerificationClonesDefaultTransport(t *testing.T) {
	t.Parallel()
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Skip("http.DefaultTransport is not *http.Transport")
	}

	transport := transportWithTLSVerification(false)
	if transport == defaultTransport {
		t.Fatal("transportWithTLSVerification returned http.DefaultTransport directly")
	}
	if transport.TLSHandshakeTimeout != defaultTransport.TLSHandshakeTimeout {
		t.Fatalf("TLSHandshakeTimeout = %v, want %v", transport.TLSHandshakeTimeout, defaultTransport.TLSHandshakeTimeout)
	}
	if transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLSClientConfig.InsecureSkipVerify = false, want true")
	}
}

func TestSTSClientDelegateWithoutSubjectToken(t *testing.T) {
	t.Parallel()
	client := NewSTSClient(STSConfig{
		WellKnownURI: "http://unused",
		Timeout:      1,
	})
	_, err := client.Delegate(context.Background(), "", TokenTypeJWT, "actor", TokenTypeJWT, nil, nil, "", "", nil)
	if err == nil {
		t.Fatalf("Delegate() error = nil, want non-nil")
	}
	if _, ok := err.(*AuthenticationError); !ok {
		t.Fatalf("error type = %T, want *AuthenticationError", err)
	}
}

func assertFormValue(t *testing.T, form url.Values, key, want string) {
	t.Helper()
	if got := form.Get(key); got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}
