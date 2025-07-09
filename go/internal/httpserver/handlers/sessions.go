package handlers

import (
	"net/http"

	"github.com/google/uuid"
	autogen_client "github.com/kagent-dev/kagent/go/internal/autogen/client"
	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
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

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}

	// Get agent ID from agent ref
	agent, err := h.DatabaseService.GetAgent(namespace + "/" + agentName)
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

	if sessionRequest.UserID == "" {
		w.RespondWithError(errors.NewBadRequestError("user_id is required", nil))
		return
	}
	log = log.WithValues("userID", sessionRequest.UserID)

	if sessionRequest.AgentRef == nil {
		w.RespondWithError(errors.NewBadRequestError("agent_ref is required", nil))
		return
	}
	log = log.WithValues("agentRef", *sessionRequest.AgentRef)

	id := uuid.New().String()
	name := id
	if sessionRequest.Name != nil {
		name = *sessionRequest.Name
	}

	agent, err := h.DatabaseService.GetAgent(*sessionRequest.AgentRef)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Agent not found", err))
		return
	}

	session := &database.Session{
		ID:      id,
		Name:    name,
		UserID:  sessionRequest.UserID,
		AgentID: &agent.ID,
	}

	log.V(1).Info("Creating session in database",
		"agentRef", sessionRequest.AgentRef,
		"name", sessionRequest.Name)

	if err := h.DatabaseService.CreateSession(session); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create session", err))
		return
	}

	log.Info("Successfully created session", "sessionID", session.ID)
	data := api.NewResponse(session, "Successfully created session", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

// HandleGetSession handles GET /api/sessions/{session_name} requests using database
func (h *SessionsHandler) HandleGetSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "get-db")

	sessionName, err := GetPathParam(r, "session_name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session name from path", err))
		return
	}
	log = log.WithValues("session_name", sessionName)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Getting session from database")
	session, err := h.DatabaseService.GetSession(sessionName, userID)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Session not found", err))
		return
	}

	log.Info("Successfully retrieved session")
	data := api.NewResponse(session, "Successfully retrieved session", false)
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

	// Get existing session
	session, err := h.DatabaseService.GetSession(*sessionRequest.Name, sessionRequest.UserID)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Session not found", err))
		return
	}

	agent, err := h.DatabaseService.GetAgent(*sessionRequest.AgentRef)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Agent not found", err))
		return
	}

	// Update fields
	session.AgentID = &agent.ID

	if err := h.DatabaseService.UpdateSession(session); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to update session", err))
		return
	}

	log.Info("Successfully updated session")
	data := api.NewResponse(session, "Successfully updated session", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleDeleteSession handles DELETE /api/sessions/{session_name} requests using database
func (h *SessionsHandler) HandleDeleteSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "delete-db")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	sessionName, err := GetPathParam(r, "session_name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}
	log = log.WithValues("session_name", sessionName)

	if err := h.DatabaseService.DeleteSession(sessionName, userID); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to delete session", err))
		return
	}

	log.Info("Successfully deleted session")
	data := api.NewResponse(struct{}{}, "Session deleted successfully", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleListSessionRuns handles GET /api/sessions/{session_name}/tasks requests using database
func (h *SessionsHandler) HandleListSessionTasks(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "list-tasks-db")

	sessionName, err := GetPathParam(r, "session_name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}
	log = log.WithValues("session_name", sessionName)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Getting session tasks from database")
	tasks, err := h.DatabaseService.ListSessionTasks(sessionName, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get session runs", err))
		return
	}

	log.Info("Successfully retrieved session tasks", "count", len(tasks))
	data := api.NewResponse(tasks, "Successfully retrieved session tasks", false)
	RespondWithJSON(w, http.StatusOK, data)
}

func (h *SessionsHandler) HandleInvokeSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "invoke-session")

	sessionName, err := GetPathParam(r, "session_name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	var req autogen_client.InvokeTaskRequest
	if err := DecodeJSONBody(r, &req); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}
	session, err := h.DatabaseService.GetSession(sessionName, userID)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Session not found", err))
		return
	}

	messages, err := h.DatabaseService.ListMessagesForSession(session.ID, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get messages for session", err))
		return
	}

	parsedMessages, err := database.ParseMessages(messages)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to parse messages", err))
		return
	}

	autogenEvents, err := utils.ConvertMessagesToAutogenEvents(parsedMessages)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to convert messages to autogen events", err))
		return
	}
	req.Messages = autogenEvents

	result, err := h.AutogenClient.InvokeTask(r.Context(), &req)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to invoke session", err))
		return
	}

	data := api.NewResponse(result, "Successfully invoked session", false)
	RespondWithJSON(w, http.StatusOK, data)
}

func (h *SessionsHandler) HandleInvokeSessionStream(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "invoke-session")

	sessionName, err := GetPathParam(r, "session_name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	var req autogen_client.InvokeTaskRequest
	if err := DecodeJSONBody(r, &req); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}
	session, err := h.DatabaseService.GetSession(sessionName, userID)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Session not found", err))
		return
	}

	messages, err := h.DatabaseService.ListMessagesForSession(session.ID, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get messages for session", err))
		return
	}

	parsedMessages, err := database.ParseMessages(messages)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to parse messages", err))
		return
	}

	autogenEvents, err := utils.ConvertMessagesToAutogenEvents(parsedMessages)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to convert messages to autogen events", err))
		return
	}
	req.Messages = autogenEvents

	ch, err := h.AutogenClient.InvokeTaskStream(r.Context(), &req)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to invoke session", err))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	w.Flush()

	for event := range ch {
		w.Write([]byte(event.String()))
		w.Flush()
	}
}
