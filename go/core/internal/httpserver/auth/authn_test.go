package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gorilla/mux"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type middlewareStaticProvider struct {
	session auth.Session
}

func (p middlewareStaticProvider) Authenticate(context.Context, http.Header, url.Values) (auth.Session, error) {
	return p.session, nil
}

func (p middlewareStaticProvider) UpstreamAuth(*http.Request, auth.Session, auth.Principal) error {
	return nil
}

type middlewareSession struct {
	principal auth.Principal
	a2aOnly   bool
}

func (s middlewareSession) Principal() auth.Principal {
	return s.principal
}

func (s middlewareSession) A2AOnly() bool {
	return s.a2aOnly
}

func TestAuthnMiddleware(t *testing.T) {
	testCases := []struct {
		name         string
		authn        auth.AuthProvider
		url          string
		expectedUser string
	}{
		{
			name:         "gets user from query param",
			authn:        &authimpl.UnsecureAuthenticator{},
			url:          "http://foo.com/index?user_id=foo",
			expectedUser: "foo",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			router := mux.NewRouter()

			router.Use(auth.AuthnMiddleware(tt.authn))
			var session auth.Session
			router.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				session, _ = auth.AuthSessionFrom(r.Context())
			})

			rw := httptest.NewRecorder()
			req, err := http.NewRequest("GET", tt.url, nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			router.ServeHTTP(rw, req)
			if rw.Code == http.StatusNotFound {
				t.Fatalf("status Not Found, router not engaged")
			}
			if tt.expectedUser != "" {
				if session == nil || session.Principal().User.ID != tt.expectedUser {
					t.Fatalf("Expected user %s but got %v", tt.expectedUser, session)
				}
			} else if session != nil {
				t.Fatalf("Expected no session but got %v", session)
			}
		})
	}
}

func TestAuthnMiddlewareA2AOnlyGuard(t *testing.T) {
	serviceSession := middlewareSession{a2aOnly: true}
	userSession := middlewareSession{principal: auth.Principal{User: auth.User{ID: "user@example.com"}}}

	tests := []struct {
		name       string
		authn      auth.AuthProvider
		url        string
		wantStatus int
	}{
		{name: "service actor allowed on a2a route", authn: middlewareStaticProvider{session: serviceSession}, url: "http://foo.com/api/a2a/kagent/agent", wantStatus: http.StatusNoContent},
		{name: "service actor allowed under a2a route", authn: middlewareStaticProvider{session: serviceSession}, url: "http://foo.com/api/a2a/kagent/agent/tasks", wantStatus: http.StatusNoContent},
		{name: "service actor allowed on sandbox route", authn: middlewareStaticProvider{session: serviceSession}, url: "http://foo.com/api/a2a-sandboxes/kagent/sandbox", wantStatus: http.StatusNoContent},
		{name: "service actor denied on non-a2a api", authn: middlewareStaticProvider{session: serviceSession}, url: "http://foo.com/api/me", wantStatus: http.StatusForbidden},
		{name: "service actor denied on a2aevil", authn: middlewareStaticProvider{session: serviceSession}, url: "http://foo.com/api/a2aevil/kagent/agent", wantStatus: http.StatusForbidden},
		{name: "service actor denied on sandboxesevil", authn: middlewareStaticProvider{session: serviceSession}, url: "http://foo.com/api/a2a-sandboxesevil/kagent/sandbox", wantStatus: http.StatusForbidden},
		{name: "service actor denied missing name segment", authn: middlewareStaticProvider{session: serviceSession}, url: "http://foo.com/api/a2a/kagent", wantStatus: http.StatusForbidden},
		{name: "service actor denied encoded slash", authn: middlewareStaticProvider{session: serviceSession}, url: "http://foo.com/api/a2a/kagent%2Fagent/tasks", wantStatus: http.StatusForbidden},
		{name: "user actor allowed on non-a2a api", authn: middlewareStaticProvider{session: userSession}, url: "http://foo.com/api/me", wantStatus: http.StatusNoContent},
		{name: "unsecure mode unaffected", authn: &authimpl.UnsecureAuthenticator{}, url: "http://foo.com/api/me", wantStatus: http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := mux.NewRouter()
			router.Use(auth.AuthnMiddleware(tt.authn))
			router.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})

			req, err := http.NewRequest(http.MethodGet, tt.url, nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			rw := httptest.NewRecorder()
			router.ServeHTTP(rw, req)
			if rw.Code != tt.wantStatus {
				t.Fatalf("status: want %d, got %d", tt.wantStatus, rw.Code)
			}
		})
	}
}
