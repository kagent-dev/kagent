package handlers

import (
	"net/http"

	"github.com/kagent-dev/kagent/go/pkg/auth"
)

type CurrentUserResponse struct {
	User   string   `json:"user"`
	Email  string   `json:"email"`
	Name   string   `json:"name"`
	Groups []string `json:"groups"`
}

type CurrentUserHandler struct{}

func NewCurrentUserHandler() *CurrentUserHandler {
	return &CurrentUserHandler{}
}

func (h *CurrentUserHandler) HandleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.AuthSessionFrom(r.Context())
	if !ok || session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	principal := session.Principal()
	response := CurrentUserResponse{
		User:   principal.User.ID,
		Email:  principal.User.Email,
		Name:   principal.User.Name,
		Groups: principal.Groups,
	}

	RespondWithJSON(w, http.StatusOK, response)
}
