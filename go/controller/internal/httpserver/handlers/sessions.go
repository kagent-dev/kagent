package handlers

import (
	"net/http"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
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
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "list")
	log.Info("Handling list sessions request")

	userID, err := GetUserID(r)
	if err != nil {
		log.Error(err, "Failed to get user ID")
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Listing sessions from Autogen")
	sessions, err := h.AutogenClient.ListSessions(userID)
	if err != nil {
		log.Error(err, "Failed to list sessions")
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Info("Successfully listed sessions", "count", len(sessions))
	RespondWithJSON(w, http.StatusOK, sessions)
}

// HandleCreateSession handles POST /api/sessions requests
func (h *SessionsHandler) HandleCreateSession(w http.ResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "create")
	log.Info("Handling create session request")

	var sessionRequest *autogen_client.CreateSession

	if err := DecodeJSONBody(r, &sessionRequest); err != nil {
		log.Error(err, "Invalid request body")
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if sessionRequest.UserID == "" {
		log.Error(nil, "Missing user_id in request")
		RespondWithError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	log = log.WithValues("userID", sessionRequest.UserID)

	log.V(1).Info("Creating session in Autogen",
		"teamID", sessionRequest.TeamID,
		"name", sessionRequest.Name)
	session, err := h.AutogenClient.CreateSession(sessionRequest)
	if err != nil {
		log.Error(err, "Failed to create session")
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Info("Successfully created session", "sessionID", session.ID)
	RespondWithJSON(w, http.StatusCreated, session)
}

func (h *SessionsHandler) HandleGetSession(w http.ResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "get")
	log.Info("Handling get session request")

	sessionID, err := GetIntPathParam(r, "sessionID")
	if err != nil {
		log.Error(err, "Failed to get session ID from path")
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	log = log.WithValues("sessionID", sessionID)

	userID, err := GetUserID(r)
	if err != nil {
		log.Error(err, "Failed to get user ID")
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Getting session from Autogen")
	session, err := h.AutogenClient.GetSession(sessionID, userID)
	if err != nil {
		log.Error(err, "Failed to get session")
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if session == nil {
		log.Info("Session not found")
		RespondWithError(w, http.StatusNotFound, "Session not found")
		return
	}

	log.Info("Successfully retrieved session")
	RespondWithJSON(w, http.StatusOK, session)
}
