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

// InvokeHandler processes agent invocation API requests.
type InvokeHandler struct {
	*Base
}

// NewInvokeHandler creates a handler with the given base dependencies.
func NewInvokeHandler(base *Base) *InvokeHandler {
	return &InvokeHandler{
		Base: base,
	}
}

// WithClient sets a client and returns the handler for chaining.
// Used primarily for testing to inject mock clients.
func (h *InvokeHandler) WithClient(client autogen_client.Client) *InvokeHandler {
	h.AutogenClient = client
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
	w.Flush()

	for event := range ch {
		w.Write([]byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event.Event, event.Data)))
		w.Flush()
	}
}

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
