package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestExternalBearerAuthenticateRejectsMissingAuthorization(t *testing.T) {
	a := newTestExternalBearerAuthenticator(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("validation service should not be called")
	})

	if _, err := a.Authenticate(context.Background(), http.Header{}, nil); err == nil {
		t.Fatal("expected missing Authorization to be rejected")
	}
}

func TestExternalBearerAuthenticateRejectsNonBearerAuthorization(t *testing.T) {
	a := newTestExternalBearerAuthenticator(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("validation service should not be called")
	})

	headers := http.Header{"Authorization": []string{"Basic abc"}}
	if _, err := a.Authenticate(context.Background(), headers, nil); err == nil {
		t.Fatal("expected non-Bearer Authorization to be rejected")
	}
}

func TestExternalBearerAuthenticateActiveTokenMapsUserID(t *testing.T) {
	var gotRequest externalBearerValidationRequest
	a := newTestExternalBearerAuthenticator(t, func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Fatalf("method = %s, want %s", got, want)
		}
		if got, want := r.Header.Get("Content-Type"), "application/json"; got != want {
			t.Fatalf("Content-Type = %q, want %q", got, want)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writeJSON(t, w, map[string]any{
			"active":  true,
			"subject": "subject-1",
			"user_id": "user-id-1",
			"claims": map[string]any{
				"sub": "claim-sub",
			},
		})
	})

	session := authenticateToken(t, a, "token-1")
	if got, want := session.Principal().User.ID, "user-id-1"; got != want {
		t.Fatalf("User.ID = %q, want %q", got, want)
	}
	if gotRequest.Token != "token-1" || gotRequest.TokenType != "Bearer" {
		t.Fatalf("validation request = %+v, want token token-1 and type Bearer", gotRequest)
	}
}

func TestExternalBearerAuthenticateFallsBackToConfiguredClaim(t *testing.T) {
	a := newTestExternalBearerAuthenticatorWithConfig(t, ExternalBearerAuthenticatorConfig{UserIDClaim: "email"}, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"active": true,
			"claims": map[string]any{
				"email": "user@example.com",
				"sub":   "claim-sub",
			},
		})
	})

	session := authenticateToken(t, a, "token-1")
	if got, want := session.Principal().User.ID, "user@example.com"; got != want {
		t.Fatalf("User.ID = %q, want %q", got, want)
	}
}

func TestExternalBearerAuthenticateFallsBackToClaimsSub(t *testing.T) {
	a := newTestExternalBearerAuthenticatorWithConfig(t, ExternalBearerAuthenticatorConfig{UserIDClaim: "email"}, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"active": true,
			"claims": map[string]any{
				"sub": "claim-sub",
			},
		})
	})

	session := authenticateToken(t, a, "token-1")
	if got, want := session.Principal().User.ID, "claim-sub"; got != want {
		t.Fatalf("User.ID = %q, want %q", got, want)
	}
}

func TestExternalBearerAuthenticateFallsBackToSubject(t *testing.T) {
	a := newTestExternalBearerAuthenticator(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"active":  true,
			"subject": "subject-1",
		})
	})

	session := authenticateToken(t, a, "token-1")
	if got, want := session.Principal().User.ID, "subject-1"; got != want {
		t.Fatalf("User.ID = %q, want %q", got, want)
	}
}

func TestExternalBearerAuthenticateRejectsInactiveToken(t *testing.T) {
	a := newTestExternalBearerAuthenticator(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"active": false, "user_id": "user-id-1"})
	})

	if _, err := a.Authenticate(context.Background(), bearerHeader("token-1"), url.Values{}); err == nil {
		t.Fatal("expected inactive token to be rejected")
	}
}

func TestExternalBearerAuthenticateRejectsMalformedJSON(t *testing.T) {
	a := newTestExternalBearerAuthenticator(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{"))
	})

	if _, err := a.Authenticate(context.Background(), bearerHeader("token-1"), url.Values{}); err == nil {
		t.Fatal("expected malformed JSON to be rejected")
	}
}

func TestExternalBearerAuthenticateRejectsValidationServer500(t *testing.T) {
	a := newTestExternalBearerAuthenticator(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	if _, err := a.Authenticate(context.Background(), bearerHeader("token-1"), url.Values{}); err == nil {
		t.Fatal("expected validation server 500 to be rejected")
	}
}

func TestExternalBearerAuthenticateRejectsTimeout(t *testing.T) {
	a := newTestExternalBearerAuthenticatorWithConfig(t, ExternalBearerAuthenticatorConfig{Timeout: time.Millisecond}, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		writeJSON(t, w, map[string]any{"active": true, "user_id": "user-id-1"})
	})

	if _, err := a.Authenticate(context.Background(), bearerHeader("token-1"), url.Values{}); err == nil {
		t.Fatal("expected timeout to be rejected")
	}
}

func TestExternalBearerValidationAuthorizationHeaderIsSent(t *testing.T) {
	a := newTestExternalBearerAuthenticatorWithConfig(t, ExternalBearerAuthenticatorConfig{ValidationAuthorization: "Bearer validation-token"}, func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer validation-token"; got != want {
			t.Fatalf("validation Authorization = %q, want %q", got, want)
		}
		writeJSON(t, w, map[string]any{"active": true, "user_id": "user-id-1"})
	})

	_ = authenticateToken(t, a, "token-1")
}

func TestExternalBearerClaimsPreservedForPrincipalAndCurrentUserSurface(t *testing.T) {
	a := newTestExternalBearerAuthenticator(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"active":  true,
			"user_id": "user-id-1",
			"claims": map[string]any{
				"sub":   "claim-sub",
				"email": "user@example.com",
			},
		})
	})

	session := authenticateToken(t, a, "token-1")
	if got, want := session.Principal().Claims["email"], "user@example.com"; got != want {
		t.Fatalf("principal claim email = %q, want %q", got, want)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req = req.WithContext(pkgauth.AuthSessionTo(req.Context(), session))
	rr := httptest.NewRecorder()
	handlers.NewCurrentUserHandler().HandleGetCurrentUser(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("/api/me status = %d, want %d", rr.Code, http.StatusOK)
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode /api/me response: %v", err)
	}
	if got["email"] != "user@example.com" || got["sub"] != "claim-sub" {
		t.Fatalf("/api/me claims = %#v, want preserved claims", got)
	}
}

func newTestExternalBearerAuthenticator(t *testing.T, handler http.HandlerFunc) *ExternalBearerAuthenticator {
	t.Helper()
	return newTestExternalBearerAuthenticatorWithConfig(t, ExternalBearerAuthenticatorConfig{}, handler)
}

func newTestExternalBearerAuthenticatorWithConfig(t *testing.T, cfg ExternalBearerAuthenticatorConfig, handler http.HandlerFunc) *ExternalBearerAuthenticator {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg.URL = server.URL
	if cfg.Timeout == 0 {
		cfg.Timeout = time.Second
	}
	a, err := NewExternalBearerAuthenticator(cfg)
	if err != nil {
		t.Fatalf("NewExternalBearerAuthenticator: %v", err)
	}
	return a
}

func authenticateToken(t *testing.T, a *ExternalBearerAuthenticator, token string) pkgauth.Session {
	t.Helper()
	session, err := a.Authenticate(context.Background(), bearerHeader(token), url.Values{})
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	return session
}

func bearerHeader(token string) http.Header {
	return http.Header{"Authorization": []string{"Bearer " + token}}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("write JSON: %v", err)
	}
}
