package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// CrewAIHandler handles CrewAI-related requests
type CrewAIHandler struct {
	*Base
}

// NewCrewAIHandler creates a new CrewAIHandler
func NewCrewAIHandler(base *Base) *CrewAIHandler {
	return &CrewAIHandler{Base: base}
}

// CrewAI request/response types following the Python Pydantic models

// KagentMemoryPayload represents memory payload data from Python
type KagentMemoryPayload struct {
	ThreadID   string         `json:"thread_id"`
	UserID     string         `json:"user_id"`
	MemoryData map[string]any `json:"memory_data"`
}

// KagentMemoryResponse represents memory response data
type KagentMemoryResponse struct {
	Data []KagentMemoryPayload `json:"data"`
}

// KagentFlowStatePayload represents flow state payload data
type KagentFlowStatePayload struct {
	ThreadID   string         `json:"thread_id"`
	MethodName string         `json:"method_name"`
	StateData  map[string]any `json:"state_data"`
}

// KagentFlowStateResponse represents flow state response data
type KagentFlowStateResponse struct {
	Data KagentFlowStatePayload `json:"data"`
}

// HandleStoreMemory handles POST /api/crewai/memory requests
func (h *CrewAIHandler) HandleStoreMemory(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("crewai-handler").WithValues("operation", "store-memory")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	var req KagentMemoryPayload
	if err := DecodeJSONBody(r, &req); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	// Validate required fields
	if req.ThreadID == "" {
		w.RespondWithError(errors.NewBadRequestError("thread_id is required", nil))
		return
	}

	log = log.WithValues(
		"threadID", req.ThreadID,
		"userID", userID,
	)

	// Serialize memory data to JSON string
	memoryDataJSON, err := json.Marshal(req.MemoryData)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to serialize memory data", err))
		return
	}

	// Create memory model
	memory := &database.CrewAIAgentMemory{
		UserID:     userID,
		ThreadID:   req.ThreadID,
		MemoryData: string(memoryDataJSON),
	}

	// Store memory
	if err := h.DatabaseService.StoreCrewAIMemory(memory); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to store CrewAI memory", err))
		return
	}

	log.Info("Successfully stored CrewAI memory")
	data := api.NewResponse(struct{}{}, "Successfully stored CrewAI memory", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

// HandleGetMemory handles GET /api/crewai/memory requests
func (h *CrewAIHandler) HandleGetMemory(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("crewai-handler").WithValues("operation", "list-memory")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	threadID := r.URL.Query().Get("thread_id")
	if threadID == "" {
		w.RespondWithError(errors.NewBadRequestError("thread_id is required", nil))
		return
	}

	taskDescription := r.URL.Query().Get("q") // query parameter for task description search

	limit := 0
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil {
			limit = parsedLimit
		}
	}

	log = log.WithValues("userID", userID, "threadID", threadID, "taskDescription", taskDescription, "limit", limit)

	var memories []*database.CrewAIAgentMemory

	// If task description is provided, search by task across all agents
	// Otherwise, list memories for a specific agent
	if taskDescription != "" {
		log.V(1).Info("Searching CrewAI memory by task description")
		memories, err = h.DatabaseService.SearchCrewAIMemoryByTask(userID, threadID, taskDescription, limit)
	} else {
		w.RespondWithError(errors.NewBadRequestError("Either agent_id or q (task description) parameter is required", nil))
		return
	}
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list CrewAI memory", err))
		return
	}

	// Convert to response format
	memoryPayloads := make([]KagentMemoryPayload, len(memories))
	for i, memory := range memories {
		var memoryData map[string]any
		if err := json.Unmarshal([]byte(memory.MemoryData), &memoryData); err != nil {
			w.RespondWithError(errors.NewInternalServerError("Failed to parse memory data", err))
			return
		}

		memoryPayloads[i] = KagentMemoryPayload{
			ThreadID:   memory.ThreadID,
			UserID:     memory.UserID,
			MemoryData: memoryData,
		}
	}

	log.Info("Successfully listed CrewAI memory", "count", len(memoryPayloads))
	data := api.NewResponse(memoryPayloads, "Successfully listed CrewAI memory", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleResetMemory handles DELETE /api/crewai/memory requests
func (h *CrewAIHandler) HandleResetMemory(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("crewai-handler").WithValues("operation", "reset-memory")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}

	threadID := r.URL.Query().Get("thread_id")
	if threadID == "" {
		w.RespondWithError(errors.NewBadRequestError("thread_id is required", nil))
		return
	}

	log = log.WithValues("userID", userID, "threadID", threadID)

	log.V(1).Info("Resetting CrewAI memory")
	err = h.DatabaseService.ResetCrewAIMemory(userID, threadID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to reset CrewAI memory", err))
		return
	}

	log.Info("Successfully reset CrewAI memory")
	data := api.NewResponse(struct{}{}, "Successfully reset CrewAI memory", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleStoreFlowState handles POST /api/crewai/flows/state requests
func (h *CrewAIHandler) HandleStoreFlowState(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("crewai-handler").WithValues("operation", "store-flow-state")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	var req KagentFlowStatePayload
	if err := DecodeJSONBody(r, &req); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	// Validate required fields
	if req.ThreadID == "" {
		w.RespondWithError(errors.NewBadRequestError("thread_id is required", nil))
		return
	}
	if req.MethodName == "" {
		w.RespondWithError(errors.NewBadRequestError("method_name is required", nil))
		return
	}

	log = log.WithValues(
		"threadID", req.ThreadID,
		"methodName", req.MethodName,
	)

	// Serialize state data to JSON string
	stateDataJSON, err := json.Marshal(req.StateData)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to serialize state data", err))
		return
	}

	// Create flow state model
	state := &database.CrewAIFlowState{
		UserID:     userID,
		ThreadID:   req.ThreadID,
		MethodName: req.MethodName,
		StateData:  string(stateDataJSON),
	}

	// Store flow state
	if err := h.DatabaseService.StoreCrewAIFlowState(state); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to store CrewAI flow state", err))
		return
	}

	log.Info("Successfully stored CrewAI flow state")
	data := api.NewResponse(struct{}{}, "Successfully stored CrewAI flow state", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

// HandleGetFlowState handles GET /api/crewai/flows/state requests
func (h *CrewAIHandler) HandleGetFlowState(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("crewai-handler").WithValues("operation", "get-flow-state")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}

	threadID := r.URL.Query().Get("thread_id")
	if threadID == "" {
		w.RespondWithError(errors.NewBadRequestError("thread_id is required", nil))
		return
	}

	log = log.WithValues("userID", userID, "threadID", threadID)

	log.V(1).Info("Getting CrewAI flow state")
	state, err := h.DatabaseService.GetCrewAIFlowState(userID, threadID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get CrewAI flow state", err))
		return
	}

	if state == nil {
		w.RespondWithError(errors.NewNotFoundError("Flow state not found", nil))
		return
	}

	// Convert to response format
	var stateData map[string]any
	if err := json.Unmarshal([]byte(state.StateData), &stateData); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to parse state data", err))
		return
	}

	statePayload := KagentFlowStatePayload{
		ThreadID:   state.ThreadID,
		MethodName: state.MethodName,
		StateData:  stateData,
	}

	log.Info("Successfully retrieved CrewAI flow state")
	data := api.NewResponse(statePayload, "Successfully retrieved CrewAI flow state", false)
	RespondWithJSON(w, http.StatusOK, data)
}
