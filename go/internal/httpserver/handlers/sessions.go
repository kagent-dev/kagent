package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// SessionsHandler handles session-related requests
type SessionsHandler struct {
	*Base
}

// NewSessionsHandler creates a new SessionsHandler
func NewSessionsHandler(base *Base) *SessionsHandler {
	return &SessionsHandler{Base: base}
}

// RunRequest represents a run creation request
type RunRequest struct {
	Task string `json:"task"`
}

func (h *SessionsHandler) HandleGetSessionsForAgent(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "get-sessions-for-agent")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get agent ref from path", err))
		return
	}
	log = log.WithValues("namespace", namespace)

	agentName, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get agent namespace from path", err))
		return
	}
	log = log.WithValues("agentName", agentName)

	userID, err := getUserIDOrAgentUser(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}

	// Get agent ID from agent ref
	agent, err := h.DatabaseService.GetAgent(utils.ConvertToPythonIdentifier(namespace + "/" + agentName))
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Agent not found", err))
		return
	}

	log.V(1).Info("Getting sessions for agent from database")
	sessions, err := h.DatabaseService.ListSessionsForAgent(agent.ID, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get sessions for agent", err))
		return
	}

	log.Info("Successfully listed sessions", "count", len(sessions))
	data := api.NewResponse(sessions, "Successfully listed sessions", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleListSessions handles GET /api/sessions requests using database
func (h *SessionsHandler) HandleListSessions(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "list-db")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Listing sessions from database")
	sessions, err := h.DatabaseService.ListSessions(userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list sessions", err))
		return
	}

	log.Info("Successfully listed sessions", "count", len(sessions))
	data := api.NewResponse(sessions, "Successfully listed sessions", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleCreateSession handles POST /api/sessions requests using database
func (h *SessionsHandler) HandleCreateSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "create-db")

	var sessionRequest api.SessionRequest
	if err := DecodeJSONBody(r, &sessionRequest); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	userID, err := getUserIDOrAgentUser(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}

	log = log.WithValues("userID", userID)

	if sessionRequest.AgentRef == nil {
		w.RespondWithError(errors.NewBadRequestError("agent_ref is required", nil))
		return
	}
	log = log.WithValues("agentRef", *sessionRequest.AgentRef)

	id := protocol.GenerateContextID()
	if sessionRequest.ID != nil && *sessionRequest.ID != "" {
		id = *sessionRequest.ID
	}

	log.V(1).Info("Getting agent from database", "session_request", sessionRequest)

	agent, err := h.DatabaseService.GetAgent(utils.ConvertToPythonIdentifier(*sessionRequest.AgentRef))
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError(fmt.Sprintf("Agent ref is invalid, please check the agent ref %s", *sessionRequest.AgentRef), err))
		return
	}

	session := &database.Session{
		ID:      id,
		Name:    sessionRequest.Name,
		UserID:  userID,
		AgentID: &agent.ID,
	}

	log.V(1).Info("Creating session in database",
		"agentRef", sessionRequest.AgentRef,
		"name", sessionRequest.Name)

	if err := h.DatabaseService.StoreSession(session); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create session", err))
		return
	}

	log.Info("Successfully created session", "sessionID", session.ID)
	data := api.NewResponse(session, "Successfully created session", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

type SessionResponse struct {
	Session *database.Session `json:"session"`
	Events  []*database.Event `json:"events"`
}

// HandleGetSession handles GET /api/sessions/{session_id} requests using database
func (h *SessionsHandler) HandleGetSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "get-db")

	sessionID, err := GetPathParam(r, "session_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session name from path", err))
		return
	}
	log = log.WithValues("session_id", sessionID)

	userID, err := getUserIDOrAgentUser(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Getting session from database")
	session, err := h.DatabaseService.GetSession(sessionID, userID)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Session not found", err))
		return
	}

	queryOptions := database.QueryOptions{
		Limit: 0,
	}
	after := r.URL.Query().Get("after")
	if after != "" {
		afterTime, err := time.Parse(time.RFC3339, after)
		if err != nil {
			w.RespondWithError(errors.NewBadRequestError("Failed to parse after timestamp", err))
			return
		}
		queryOptions.After = afterTime
	}

	limit := r.URL.Query().Get("limit")
	if limit != "" {
		queryOptions.Limit, err = strconv.Atoi(limit)
		if err != nil {
			w.RespondWithError(errors.NewBadRequestError("Failed to parse limit", err))
			return
		}
	}

	events, err := h.DatabaseService.ListEventsForSession(sessionID, userID, queryOptions)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get events for session", err))
		return
	}

	log.Info("Successfully retrieved session")
	data := api.NewResponse(SessionResponse{
		Session: session,
		Events:  events,
	}, "Successfully retrieved session", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleUpdateSession handles PUT /api/sessions requests using database
func (h *SessionsHandler) HandleUpdateSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "update-db")

	var sessionRequest api.SessionRequest
	if err := DecodeJSONBody(r, &sessionRequest); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	if sessionRequest.Name == nil {
		w.RespondWithError(errors.NewBadRequestError("session name is required", nil))
		return
	}

	if sessionRequest.AgentRef == nil {
		w.RespondWithError(errors.NewBadRequestError("agent_ref is required", nil))
		return
	}
	log = log.WithValues("agentRef", *sessionRequest.AgentRef)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	// Get existing session
	session, err := h.DatabaseService.GetSession(*sessionRequest.Name, userID)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Session not found", err))
		return
	}

	agent, err := h.DatabaseService.GetAgent(utils.ConvertToPythonIdentifier(*sessionRequest.AgentRef))
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Agent not found", err))
		return
	}

	// Update fields
	session.AgentID = &agent.ID

	if err := h.DatabaseService.StoreSession(session); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to update session", err))
		return
	}

	log.Info("Successfully updated session")
	data := api.NewResponse(session, "Successfully updated session", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleDeleteSession handles DELETE /api/sessions/{session_id} requests using database
func (h *SessionsHandler) HandleDeleteSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "delete-db")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	sessionID, err := GetPathParam(r, "session_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}
	log = log.WithValues("session_id", sessionID)

	if err := h.DatabaseService.DeleteSession(sessionID, userID); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to delete session", err))
		return
	}

	log.Info("Successfully deleted session")
	data := api.NewResponse(struct{}{}, "Session deleted successfully", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleListSessionRuns handles GET /api/sessions/{session_id}/tasks requests using database
func (h *SessionsHandler) HandleListTasksForSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "list-tasks-db")

	sessionID, err := GetPathParam(r, "session_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}
	log = log.WithValues("session_id", sessionID)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	// Verify session exists
	_, err = h.DatabaseService.GetSession(sessionID, userID)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Session not found for given ID", err))
		return
	}

	log.V(1).Info("Getting session tasks from database")
	tasks, err := h.DatabaseService.ListTasksForSession(sessionID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get session runs", err))
		return
	}

	log.Info("Successfully retrieved session tasks", "count", len(tasks))
	data := api.NewResponse(tasks, "Successfully retrieved session tasks", false)
	RespondWithJSON(w, http.StatusOK, data)
}

func (h *SessionsHandler) HandleAddEventToSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "add-event")
	sessionID, err := GetPathParam(r, "session_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}
	log = log.WithValues("session_id", sessionID)

	principal, err := GetPrincipal(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	userID, err := getUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	var eventData struct {
		ID   string `json:"id"`
		Data string `json:"data"`
	}
	if err := DecodeJSONBody(r, &eventData); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	// Get session to verify it exists
	session, err := h.DatabaseService.GetSession(sessionID, userID)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Session not found", err))
		return
	}

	if session.AgentID != nil && *session.AgentID != utils.ConvertToPythonIdentifier(principal.Agent.ID) {
		w.RespondWithError(errors.NewForbiddenError("Session does not belong to this agent", nil))
		return
	}
	event := &database.Event{
		ID:        eventData.ID,
		SessionID: sessionID,
		Data:      eventData.Data,
		UserID:    userID,
	}
	if err := h.DatabaseService.StoreEvents(event); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to store event", err))
		return
	}

	log.Info("Successfully added event to session")
	data := api.NewResponse(event, "Event added to session successfully", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

func getUserID(r *http.Request) (string, error) {
	log := ctrllog.Log.WithName("http-helpers")

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		log.Info("Missing user_id parameter in request")
	}

	// if not in query param, check header
	if userID == "" {
		userID = r.Header.Get("X-User-ID")
	}
	if userID == "" {
		log.Info("Missing X-User-ID header in request")
		return "", fmt.Errorf("user_id is required")
	}

	log.V(2).Info("Retrieved user_id from request", "userID", userID)
	return userID, nil
}

func getUserIDOrAgentUser(r *http.Request) (string, error) {
	principal, err := GetPrincipal(r)
	if err != nil {
		return "", err
	}

	if principal.User.ID != "" {
		return principal.User.ID, nil
	} else if principal.Agent.ID != "" {
		// grab the user id from the query param
		return getUserID(r)
	}
	return "", fmt.Errorf("no user or agent in principal")
}
