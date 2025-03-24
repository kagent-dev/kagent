package handlers

import (
	"net/http"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
)

// SessionsHandler handles session-related requests
type SessionsHandler struct {
	*Base
}

// NewSessionsHandler creates a new SessionsHandler
func NewSessionsHandler(base *Base) *SessionsHandler {
	return &SessionsHandler{Base: base}
}

// HandleListSessions handles GET /api/sessions requests
func (h *SessionsHandler) HandleListSessions(w http.ResponseWriter, r *http.Request) {
	userID, err := GetUserID(r)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	sessions, err := h.AutogenClient.ListSessions(userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondWithJSON(w, http.StatusOK, sessions)
}

// HandleCreateSession handles POST /api/sessions requests
func (h *SessionsHandler) HandleCreateSession(w http.ResponseWriter, r *http.Request) {
	var sessionRequest *autogen_client.CreateSession

	if err := DecodeJSONBody(r, &sessionRequest); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if sessionRequest.UserID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id is required")
		return
	}

	session, err := h.AutogenClient.CreateSession(sessionRequest)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondWithJSON(w, http.StatusCreated, session)
}
