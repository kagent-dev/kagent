package handlers

import (
	"net/http"

	"github.com/kagent-dev/kagent/go/pkg/auth"
)

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
	if principal.Claims != nil {
		RespondWithJSON(w, http.StatusOK, principal.Claims)
	} else {
		RespondWithJSON(w, http.StatusOK, map[string]any{
			"sub": principal.User.ID,
		})
	}
}
