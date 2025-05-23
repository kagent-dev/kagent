package handlers

import (
	"fmt"
	"io"
	"net/http"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
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
func (h *SessionsHandler) HandleListSessions(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "list")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Listing sessions from Autogen")
	sessions, err := h.AutogenClient.ListSessions(userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list sessions", err))
		return
	}

	log.Info("Successfully listed sessions", "count", len(sessions))
	RespondWithJSON(w, http.StatusOK, sessions)
}

// HandleCreateSession handles POST /api/sessions requests
func (h *SessionsHandler) HandleCreateSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "create")

	var sessionRequest *autogen_client.CreateSession
	if err := DecodeJSONBody(r, &sessionRequest); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	if sessionRequest.UserID == "" {
		w.RespondWithError(errors.NewBadRequestError("user_id is required", nil))
		return
	}
	log = log.WithValues("userID", sessionRequest.UserID)

	log.V(1).Info("Creating session in Autogen",
		"teamID", sessionRequest.TeamID,
		"name", sessionRequest.Name)
	session, err := h.AutogenClient.CreateSession(sessionRequest)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create session", err))
		return
	}

	log.Info("Successfully created session", "sessionID", session.ID)
	RespondWithJSON(w, http.StatusCreated, session)
}

// HandleGetSession handles GET /api/sessions/{sessionID} requests
func (h *SessionsHandler) HandleGetSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "get")

	sessionID, err := GetIntPathParam(r, "sessionID")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}
	log = log.WithValues("sessionID", sessionID)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Getting session from Autogen")
	session, err := h.AutogenClient.GetSessionById(sessionID, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get session", err))
		return
	}

	if session == nil {
		w.RespondWithError(errors.NewNotFoundError("Session not found", nil))
		return
	}

	log.Info("Successfully retrieved session")
	RespondWithJSON(w, http.StatusOK, session)
}

func (h *SessionsHandler) HandleSessionInvoke(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "invoke")

	sessionID, err := GetIntPathParam(r, "sessionID")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}
	log = log.WithValues("sessionID", sessionID)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to read request body", err))
		return
	}

	result, err := h.AutogenClient.InvokeSession(sessionID, userID, string(body))
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to invoke session", err))
		return
	}

	RespondWithJSON(w, http.StatusOK, result)
}

func (h *SessionsHandler) HandleSessionInvokeStream(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "invoke-stream")

	sessionID, err := GetIntPathParam(r, "sessionID")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}
	log = log.WithValues("sessionID", sessionID)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to read request body", err))
		return
	}

	ch, err := h.AutogenClient.InvokeSessionStream(sessionID, userID, string(body))
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to invoke session", err))
		return
	}

	for event := range ch {
		w.Write([]byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event.Event, event.Data)))
	}
}

// HandleListSessionMessages handles GET /api/sessions/{sessionID}/messages requests
func (h *SessionsHandler) HandleListSessionMessages(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "list-session-messages")

	sessionID, err := GetIntPathParam(r, "sessionID")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}
	log = log.WithValues("sessionID", sessionID)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Listing runs for session from Autogen")
	runs, err := h.AutogenClient.ListSessionRuns(sessionID, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list session runs", err))
		return
	}

	// Collect the list of configs within runs and messages
	configs := []autogen_client.TaskMessageMap{}
	for _, run := range runs {
		for _, message := range run.Messages {
			item := make(autogen_client.TaskMessageMap)
			if message.Config != nil {
				for k, v := range message.Config {
					item[k] = v
				}
			}
			item["id"] = message.ID
			configs = append(configs, item)
		}
	}
	RespondWithJSON(w, http.StatusOK, configs)
}

func (h *SessionsHandler) HandleDeleteSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "delete-session")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	sessionID, err := GetIntPathParam(r, "sessionID")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}
	log = log.WithValues("sessionID", sessionID)

	err = h.AutogenClient.DeleteSession(sessionID, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to delete session", err))
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

func (h *SessionsHandler) HandleUpdateSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "update-session")

	sessionID, err := GetIntPathParam(r, "sessionID")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}
	log = log.WithValues("sessionID", sessionID)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	var sessionRequest *autogen_client.Session
	if err := DecodeJSONBody(r, &sessionRequest); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	updatedSession, err := h.AutogenClient.UpdateSession(sessionID, userID, sessionRequest)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to update session", err))
		return
	}

	RespondWithJSON(w, http.StatusOK, updatedSession)
}
