package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// mockAutogenClient interface for testing
type mockAutogenClient interface {
	CreateSession(*autogen_client.CreateSession) (*autogen_client.Session, error)
	CreateRun(*autogen_client.CreateRunRequest) (*autogen_client.CreateRunResult, error)
}

// InvokeHandler handles agent invocation requests
type InvokeHandler struct {
	*Base
	testClient interface{}
}

// NewInvokeHandler creates a new InvokeHandler
func NewInvokeHandler(base *Base) *InvokeHandler {
	return &InvokeHandler{Base: base}
}

// SetTestClient allows setting a mock client for testing
func (h *InvokeHandler) SetTestClient(client interface{}) {
	h.testClient = client
}

// InvokeRequest represents a request to invoke an agent
type InvokeRequest struct {
	Message string                 `json:"message"`
	Sync    bool                   `json:"sync"`
	Context map[string]interface{} `json:"context,omitempty"`
	UserID  string                 `json:"user_id,omitempty"`
}

// InvokeResponse represents a response from an agent invocation
type InvokeResponse struct {
	SessionID   string `json:"sessionId"`
	Response    string `json:"response,omitempty"`
	StatusURL   string `json:"statusUrl,omitempty"`
	Status      string `json:"status"`
	CompletedAt string `json:"completedAt,omitempty"`
}

// HandleInvokeAgent handles POST /api/agents/{agentId}/invoke requests
func (h *InvokeHandler) HandleInvokeAgent(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("invoke-handler").WithValues("operation", "invoke")

	// Extract agent ID from path
	vars := mux.Vars(r)
	agentIDStr := vars["agentId"]
	if agentIDStr == "" {
		w.RespondWithError(errors.NewBadRequestError("Agent ID is required", nil))
		return
	}
	
	// Convert agent ID to int
	agentID, err := strconv.Atoi(agentIDStr)
	if err != nil {
		if httpWriter, ok := w.(http.ResponseWriter); ok {
			httpWriter.WriteHeader(http.StatusBadRequest)
		}
		w.RespondWithError(errors.NewBadRequestError("Invalid agent ID format, must be an integer", err))
		return
	}
	log = log.WithValues("agentId", agentID)

	// Parse request body
	var invokeRequest InvokeRequest
	if err = DecodeJSONBody(r, &invokeRequest); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	// Get user ID from request or header
	userID := invokeRequest.UserID
	if userID == "" {
		userID, err = GetUserID(r)
		if err != nil {
			w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
			return
		}
	}
	log = log.WithValues("userID", userID)

	// Create session request
	sessionRequest := &autogen_client.CreateSession{
		UserID: userID,
		Name:   fmt.Sprintf("Invocation of agent %d", agentID),
		TeamID: agentID,
	}
	
	log.V(1).Info("Creating session for agent invocation")
	var session *autogen_client.Session
	
	// Handle client selection for testing vs. production
	if h.testClient != nil {
		if mockClient, ok := h.testClient.(mockAutogenClient); ok {
			session, err = mockClient.CreateSession(sessionRequest)
		}
	} else if h.AutogenClient != nil {
		session, err = h.AutogenClient.CreateSession(sessionRequest)
	} else {
		err = fmt.Errorf("no client available")
	}
	
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create session", err))
		return
	}
	log = log.WithValues("sessionID", session.ID)

	// Create run request
	runRequest := &autogen_client.CreateRunRequest{
		UserID:    userID,
		SessionID: session.ID,
	}
	
	log.V(1).Info("Creating run for agent invocation")
	var run *autogen_client.CreateRunResult
	
	if h.testClient != nil {
		if mockClient, ok := h.testClient.(mockAutogenClient); ok {
			run, err = mockClient.CreateRun(runRequest)
		}
	} else if h.AutogenClient != nil {
		run, err = h.AutogenClient.CreateRun(runRequest)
	} else {
		err = fmt.Errorf("no client available")
	}
	
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create run", err))
		return
	}
	log = log.WithValues("runID", run.ID)

	// Prepare response
	response := InvokeResponse{
		SessionID: fmt.Sprintf("%d", session.ID),
	}

	// Handle based on sync parameter
	if invokeRequest.Sync {
		log.Info("Synchronous request - waiting for response")
		response.Status = "completed"
		response.Response = "This is a placeholder response. In a real implementation, we would wait for the agent to respond."
		response.CompletedAt = time.Now().Format(time.RFC3339)
	} else {
		log.Info("Asynchronous request - returning immediately")
		response.Status = "processing"
		response.StatusURL = fmt.Sprintf("/api/sessions/%d", session.ID)
	}

	log.Info("Successfully invoked agent")
	RespondWithJSON(w, http.StatusOK, response)
} 
