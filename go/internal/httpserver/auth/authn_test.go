package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	authimpl "github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/pkg/auth"
)

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

			router.Use(authimpl.AuthnMiddleware(tt.authn))
			var session *auth.Session
			router.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				session, _ = authimpl.AuthSessionFrom(r.Context())
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
				if session == nil || session.Principal.User != tt.expectedUser {
					t.Fatalf("Expected user %s but got %v", tt.expectedUser, session)
				}
			} else if session != nil {
				t.Fatalf("Expected no session but got %v", session)
			}
		})
	}
}
