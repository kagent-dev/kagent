package handlers

import (
	"net/http"

	"github.com/kagent-dev/kagent/go/internal/httpserver/auth"
)

type AuthHandlers struct {
	auth *auth.SecureAuthenticator
}

func NewAuthHandlers() *AuthHandlers {
	return &AuthHandlers{
		auth: auth.NewSecureAuthenticator(),
	}
}

func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	h.auth.LoginHandler(w, r)
}

func (h *AuthHandlers) Me(w http.ResponseWriter, r *http.Request) {
	h.auth.MeHandler(w, r)
}

func (h *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	h.auth.LogoutHandler(w, r)
}

// RegisterAuthRoutes registers authentication routes
func RegisterAuthRoutes(mux *http.ServeMux) {
	authHandlers := NewAuthHandlers()
	
	mux.HandleFunc("/api/auth/login", authHandlers.Login)
	mux.HandleFunc("/api/auth/me", authHandlers.Me)
	mux.HandleFunc("/api/auth/logout", authHandlers.Logout)
}

