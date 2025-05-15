package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-logr/logr"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// AutogenClient defines operations for interacting with the autogen backend.
type AutogenClient interface {
	CreateSession(*autogen_client.CreateSession) (*autogen_client.Session, error)
	CreateRun(*autogen_client.CreateRunRequest) (*autogen_client.CreateRunResult, error)
}

// InvokeHandler processes agent invocation API requests.
type InvokeHandler struct {
	*Base
	client AutogenClient
}

// NewInvokeHandler creates a handler with the given base dependencies.
func NewInvokeHandler(base *Base) *InvokeHandler {
	return &InvokeHandler{
		Base:   base,
		client: base.AutogenClient,
	}
}

// WithClient sets a client and returns the handler for chaining.
// Used primarily for testing to inject mock clients.
func (h *InvokeHandler) WithClient(client AutogenClient) *InvokeHandler {
	h.client = client
	return h
}

// InvokeRequest represents an agent invocation request.
type InvokeRequest struct {
	Message string `json:"message"`
	UserID  string `json:"user_id,omitempty"`
}

// InvokeResponse contains data returned after an agent invocation.
type InvokeResponse struct {
	SessionID   string `json:"sessionId"`
	Response    string `json:"response,omitempty"`
	StatusURL   string `json:"statusUrl,omitempty"`
	Status      string `json:"status"`
	CompletedAt string `json:"completedAt,omitempty"`
}

// HandleInvokeAgent processes synchronous agent execution requests.
func (h *InvokeHandler) HandleInvokeAgent(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("invoke-handler").WithValues("operation", "invoke")

	agentID, req, err := h.extractAgentParams(w, r, log)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to extract agent params", err))
		return
	}

	team, err := h.AutogenClient.GetTeamByID(agentID, req.UserID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get team", err))
		return
	}

	result, err := h.AutogenClient.InvokeTask(&autogen_client.InvokeTaskRequest{
		Task:       req.Message,
		TeamConfig: team.Component,
	})
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to invoke task", err))
		return
	}

	log.Info("Synchronous request - waiting for response")

	log.Info("Successfully invoked agent")
	RespondWithJSON(w, http.StatusOK, result)
}

// HandleInvokeAgentStream processes asynchronous agent execution requests.
func (h *InvokeHandler) HandleInvokeAgentStream(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("invoke-handler").WithValues("operation", "invoke")

	agentID, req, err := h.extractAgentParams(w, r, log)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to extract agent params", err))
		return
	}

	team, err := h.AutogenClient.GetTeamByID(agentID, req.UserID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get team", err))
		return
	}

	ch, err := h.AutogenClient.InvokeTaskStream(&autogen_client.InvokeTaskRequest{
		Task:       req.Message,
		TeamConfig: team.Component,
	})
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to invoke task", err))
		return
	}

	log.Info("Asynchronous request - streaming response")

	log.Info("Successfully invoked agent")
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)

	for event := range ch {
		w.Write([]byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event.Event, event.Data)))
	}
}

// HandleStartAgent processes asynchronous agent execution requests.
// func (h *InvokeHandler) HandleStartAgent(w ErrorResponseWriter, r *http.Request) {
// 	log := ctrllog.FromContext(r.Context()).WithName("invoke-handler").WithValues("operation", "start")

// 	agentID, req, err := h.extractAgentParams(w, r, log)
// 	if err != nil {
// 		return
// 	}

// 	session, _, err := h.createSessionAndRun(w, agentID, req.UserID, log)
// 	if err != nil {
// 		return
// 	}

// 	log.Info("Asynchronous request - returning immediately")
// 	response := InvokeResponse{
// 		SessionID: fmt.Sprintf("%d", session.ID),
// 		Status:    "processing",
// 		StatusURL: fmt.Sprintf("/api/sessions/%d", session.ID),
// 	}

// 	log.Info("Successfully started agent")
// 	RespondWithJSON(w, http.StatusAccepted, response)
// }

// extractAgentParams parses and validates agent ID and user ID from the request.
func (h *InvokeHandler) extractAgentParams(w ErrorResponseWriter, r *http.Request, log logr.Logger) (int, *InvokeRequest, error) {
	agentIDStr, err := GetPathParam(r, "agentId")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Agent ID is required", err))
		return 0, nil, err
	}

	agentID, err := strconv.Atoi(agentIDStr)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid agent ID format, must be an integer", err))
		return 0, nil, err
	}
	log.WithValues("agentId", agentID)

	var invokeRequest InvokeRequest
	if err = DecodeJSONBody(r, &invokeRequest); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return 0, nil, err
	}

	userID := invokeRequest.UserID
	if userID == "" {
		userID, err = GetUserID(r)
		if err != nil {
			w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
			return 0, nil, err
		}
	}
	log.WithValues("userID", userID)

	return agentID, &invokeRequest, nil
}

// createSessionAndRun creates a session and run for the specified agent.
func (h *InvokeHandler) createSessionAndRun(w ErrorResponseWriter, agentID int, userID string, log logr.Logger) (*autogen_client.Session, *autogen_client.CreateRunResult, error) {
	if h.client == nil {
		panic("No client available for agent execution - this is a critical error")
	}

	sessionRequest := &autogen_client.CreateSession{
		UserID: userID,
		Name:   fmt.Sprintf("Invocation of agent %d", agentID),
		TeamID: agentID,
	}

	log.V(1).Info("Creating session for agent execution")

	session, err := h.client.CreateSession(sessionRequest)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create session", err))
		return nil, nil, err
	}

	runRequest := &autogen_client.CreateRunRequest{
		UserID:    userID,
		SessionID: session.ID,
	}

	log.V(1).Info("Creating run for agent execution")
	run, err := h.client.CreateRun(runRequest)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create run", err))
		return nil, nil, err
	}

	log.WithValues("sessionID", session.ID, "runID", run.ID)
	return session, run, nil
}
