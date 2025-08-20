package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// CheckpointsHandler handles LangGraph checkpoint-related requests
type CheckpointsHandler struct {
	*Base
}

// NewCheckpointsHandler creates a new CheckpointsHandler
func NewCheckpointsHandler(base *Base) *CheckpointsHandler {
	return &CheckpointsHandler{Base: base}
}

// CheckpointRequest represents a request to store a checkpoint
type CheckpointRequest struct {
	ThreadID           string                   `json:"thread_id"`
	CheckpointNS       string                   `json:"checkpoint_ns,omitempty"`
	CheckpointID       string                   `json:"checkpoint_id,omitempty"`
	ParentCheckpointID *string                  `json:"parent_checkpoint_id,omitempty"`
	Checkpoint         map[string]interface{}   `json:"checkpoint"`
	Metadata           map[string]interface{}   `json:"metadata"`
	Writes             []CheckpointWriteRequest `json:"writes,omitempty"`
	Version            int                      `json:"version,omitempty"`
}

// CheckpointWriteRequest represents a write operation in a checkpoint request
type CheckpointWriteRequest struct {
	Key        string      `json:"key"`
	Value      interface{} `json:"value"`
	WriteIndex *int        `json:"write_index,omitempty"`
	Source     *string     `json:"source,omitempty"`
}

// CheckpointResponse represents the response format for checkpoint operations
type CheckpointResponse struct {
	CheckpointID string    `json:"checkpoint_id"`
	CreatedAt    time.Time `json:"created_at"`
}

// CheckpointTuple represents a LangGraph checkpoint tuple
type CheckpointTuple struct {
	Config       map[string]interface{} `json:"config"`
	Checkpoint   map[string]interface{} `json:"checkpoint"`
	Metadata     map[string]interface{} `json:"metadata"`
	ParentConfig map[string]interface{} `json:"parent_config,omitempty"`
}

// CheckpointWrite represents a checkpoint write in responses
type CheckpointWrite struct {
	WriteIndex int         `json:"write_index"`
	Key        string      `json:"key"`
	Value      interface{} `json:"value"`
	Source     *string     `json:"source,omitempty"`
}

// GetUserIDFromHeaderOrQuery gets user ID from X-User-ID header or user_id query param
func GetUserIDFromHeaderOrQuery(r *http.Request) (string, error) {
	// Prefer X-User-ID header
	if userID := r.Header.Get("X-User-ID"); userID != "" {
		return userID, nil
	}

	// Fall back to query parameter
	return GetUserID(r)
}

// HandlePutCheckpoint handles POST /api/langgraph/checkpoints requests
func (h *CheckpointsHandler) HandlePutCheckpoint(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("checkpoints-handler").WithValues("operation", "put")

	userID, err := GetUserIDFromHeaderOrQuery(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	var req CheckpointRequest
	if err := DecodeJSONBody(r, &req); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	// Validate required fields
	if req.ThreadID == "" {
		w.RespondWithError(errors.NewBadRequestError("thread_id is required", nil))
		return
	}
	if req.Checkpoint == nil {
		w.RespondWithError(errors.NewBadRequestError("checkpoint is required", nil))
		return
	}
	if req.Metadata == nil {
		req.Metadata = make(map[string]interface{})
	}

	// Generate checkpoint ID if not provided
	if req.CheckpointID == "" {
		req.CheckpointID = uuid.New().String()
	}

	// Default checkpoint namespace
	if req.CheckpointNS == "" {
		req.CheckpointNS = ""
	}

	// Default version
	if req.Version == 0 {
		req.Version = 1
	}

	log = log.WithValues(
		"threadID", req.ThreadID,
		"checkpointNS", req.CheckpointNS,
		"checkpointID", req.CheckpointID,
	)

	// Serialize checkpoint and metadata
	checkpointJSON, err := json.Marshal(req.Checkpoint)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to serialize checkpoint", err))
		return
	}

	metadataJSON, err := json.Marshal(req.Metadata)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to serialize metadata", err))
		return
	}

	// Create checkpoint model
	checkpoint := &database.LangGraphCheckpoint{
		UserID:             userID,
		ThreadID:           req.ThreadID,
		CheckpointNS:       req.CheckpointNS,
		CheckpointID:       req.CheckpointID,
		ParentCheckpointID: req.ParentCheckpointID,
		Metadata:           string(metadataJSON),
		Checkpoint:         string(checkpointJSON),
		Version:            req.Version,
	}

	// Prepare writes
	writes := make([]*database.LangGraphCheckpointWrite, len(req.Writes))
	for i, writeReq := range req.Writes {
		writeIndex := i
		if writeReq.WriteIndex != nil {
			writeIndex = *writeReq.WriteIndex
		}

		valueJSON, err := json.Marshal(writeReq.Value)
		if err != nil {
			w.RespondWithError(errors.NewBadRequestError(fmt.Sprintf("Failed to serialize write value for key %s", writeReq.Key), err))
			return
		}

		writes[i] = &database.LangGraphCheckpointWrite{
			UserID:       userID,
			ThreadID:     req.ThreadID,
			CheckpointNS: req.CheckpointNS,
			CheckpointID: req.CheckpointID,
			WriteIndex:   writeIndex,
			Key:          writeReq.Key,
			Value:        string(valueJSON),
			Source:       writeReq.Source,
		}
	}

	log.V(1).Info("Storing checkpoint with writes", "writesCount", len(writes))

	// Store checkpoint and writes atomically
	if err := h.DatabaseService.StoreCheckpoint(checkpoint, writes); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to store checkpoint", err))
		return
	}

	log.Info("Successfully stored checkpoint")
	response := CheckpointResponse{
		CheckpointID: checkpoint.CheckpointID,
		CreatedAt:    checkpoint.CreatedAt,
	}
	data := api.NewResponse(response, "Successfully stored checkpoint", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

// HandleGetLatestCheckpoint handles GET /api/langgraph/checkpoints/latest requests
func (h *CheckpointsHandler) HandleGetLatestCheckpoint(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("checkpoints-handler").WithValues("operation", "get-latest")

	userID, err := GetUserIDFromHeaderOrQuery(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}

	threadID := r.URL.Query().Get("thread_id")
	if threadID == "" {
		w.RespondWithError(errors.NewBadRequestError("thread_id is required", nil))
		return
	}

	checkpointNS := r.URL.Query().Get("checkpoint_ns")
	if checkpointNS == "" {
		checkpointNS = ""
	}

	log = log.WithValues("userID", userID, "threadID", threadID, "checkpointNS", checkpointNS)

	log.V(1).Info("Getting latest checkpoint")
	checkpoint, writes, err := h.DatabaseService.GetLatestCheckpoint(userID, threadID, checkpointNS)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get latest checkpoint", err))
		return
	}

	if checkpoint == nil {
		w.RespondWithError(errors.NewNotFoundError("No checkpoint found", nil))
		return
	}

	// Convert to response format
	tuple, err := h.convertToCheckpointTuple(checkpoint, writes)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to convert checkpoint", err))
		return
	}

	log.Info("Successfully retrieved latest checkpoint")
	data := api.NewResponse(tuple, "Successfully retrieved latest checkpoint", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleListCheckpoints handles GET /api/langgraph/checkpoints requests
func (h *CheckpointsHandler) HandleListCheckpoints(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("checkpoints-handler").WithValues("operation", "list")

	userID, err := GetUserIDFromHeaderOrQuery(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}

	threadID := r.URL.Query().Get("thread_id")
	if threadID == "" {
		w.RespondWithError(errors.NewBadRequestError("thread_id is required", nil))
		return
	}

	checkpointNS := r.URL.Query().Get("checkpoint_ns")
	if checkpointNS == "" {
		checkpointNS = ""
	}

	beforeCheckpointID := r.URL.Query().Get("before_checkpoint_id")

	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil {
			limit = parsedLimit
		}
	}

	log = log.WithValues("userID", userID, "threadID", threadID, "checkpointNS", checkpointNS, "limit", limit)

	log.V(1).Info("Listing checkpoints")
	checkpoints, err := h.DatabaseService.ListCheckpoints(userID, threadID, checkpointNS, beforeCheckpointID, limit)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list checkpoints", err))
		return
	}

	// Convert to response format (without writes for list operation)
	tuples := make([]CheckpointTuple, len(checkpoints))
	for i, checkpoint := range checkpoints {
		tuple, err := h.convertToCheckpointTuple(checkpoint, nil)
		if err != nil {
			w.RespondWithError(errors.NewInternalServerError("Failed to convert checkpoint", err))
			return
		}
		tuples[i] = *tuple
	}

	log.Info("Successfully listed checkpoints", "count", len(tuples))
	data := api.NewResponse(tuples, "Successfully listed checkpoints", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleGetCheckpoint handles GET /api/langgraph/checkpoints/{thread_id}/{checkpoint_ns}/{checkpoint_id} requests
func (h *CheckpointsHandler) HandleGetCheckpoint(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("checkpoints-handler").WithValues("operation", "get")

	userID, err := GetUserIDFromHeaderOrQuery(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}

	threadID, err := GetPathParam(r, "thread_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get thread_id from path", err))
		return
	}

	checkpointNS, err := GetPathParam(r, "checkpoint_ns")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get checkpoint_ns from path", err))
		return
	}

	checkpointID, err := GetPathParam(r, "checkpoint_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get checkpoint_id from path", err))
		return
	}

	log = log.WithValues("userID", userID, "threadID", threadID, "checkpointNS", checkpointNS, "checkpointID", checkpointID)

	log.V(1).Info("Getting checkpoint")
	checkpoint, writes, err := h.DatabaseService.GetCheckpoint(userID, threadID, checkpointNS, checkpointID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get checkpoint", err))
		return
	}

	if checkpoint == nil {
		w.RespondWithError(errors.NewNotFoundError("Checkpoint not found", nil))
		return
	}

	// Convert to response format
	tuple, err := h.convertToCheckpointTuple(checkpoint, writes)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to convert checkpoint", err))
		return
	}

	log.Info("Successfully retrieved checkpoint")
	data := api.NewResponse(tuple, "Successfully retrieved checkpoint", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleListWrites handles GET /api/langgraph/checkpoints/{thread_id}/{checkpoint_ns}/{checkpoint_id}/writes requests
func (h *CheckpointsHandler) HandleListWrites(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("checkpoints-handler").WithValues("operation", "list-writes")

	userID, err := GetUserIDFromHeaderOrQuery(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}

	threadID, err := GetPathParam(r, "thread_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get thread_id from path", err))
		return
	}

	checkpointNS, err := GetPathParam(r, "checkpoint_ns")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get checkpoint_ns from path", err))
		return
	}

	checkpointID, err := GetPathParam(r, "checkpoint_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get checkpoint_id from path", err))
		return
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if parsedOffset, err := strconv.Atoi(offsetStr); err == nil {
			offset = parsedOffset
		}
	}

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil {
			limit = parsedLimit
		}
	}

	log = log.WithValues("userID", userID, "threadID", threadID, "checkpointNS", checkpointNS, "checkpointID", checkpointID)

	log.V(1).Info("Listing checkpoint writes")
	writes, err := h.DatabaseService.ListWrites(userID, threadID, checkpointNS, checkpointID, offset, limit)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list checkpoint writes", err))
		return
	}

	// Convert to response format
	responseWrites := make([]CheckpointWrite, len(writes))
	for i, write := range writes {
		var value interface{}
		if err := json.Unmarshal([]byte(write.Value), &value); err != nil {
			w.RespondWithError(errors.NewInternalServerError("Failed to deserialize write value", err))
			return
		}

		responseWrites[i] = CheckpointWrite{
			WriteIndex: write.WriteIndex,
			Key:        write.Key,
			Value:      value,
			Source:     write.Source,
		}
	}

	log.Info("Successfully listed checkpoint writes", "count", len(responseWrites))
	data := api.NewResponse(responseWrites, "Successfully listed checkpoint writes", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// convertToCheckpointTuple converts database models to LangGraph checkpoint tuple format
func (h *CheckpointsHandler) convertToCheckpointTuple(checkpoint *database.LangGraphCheckpoint, writes []*database.LangGraphCheckpointWrite) (*CheckpointTuple, error) {
	// Deserialize checkpoint and metadata
	var checkpointData map[string]interface{}
	if err := json.Unmarshal([]byte(checkpoint.Checkpoint), &checkpointData); err != nil {
		return nil, fmt.Errorf("failed to deserialize checkpoint: %w", err)
	}

	var metadataData map[string]interface{}
	if err := json.Unmarshal([]byte(checkpoint.Metadata), &metadataData); err != nil {
		return nil, fmt.Errorf("failed to deserialize metadata: %w", err)
	}

	// Create config
	config := map[string]interface{}{
		"configurable": map[string]interface{}{
			"user_id":       checkpoint.UserID,
			"thread_id":     checkpoint.ThreadID,
			"checkpoint_ns": checkpoint.CheckpointNS,
			"checkpoint_id": checkpoint.CheckpointID,
		},
	}

	tuple := &CheckpointTuple{
		Config:     config,
		Checkpoint: checkpointData,
		Metadata:   metadataData,
	}

	// Add parent config if parent exists
	if checkpoint.ParentCheckpointID != nil {
		tuple.ParentConfig = map[string]interface{}{
			"configurable": map[string]interface{}{
				"user_id":       checkpoint.UserID,
				"thread_id":     checkpoint.ThreadID,
				"checkpoint_ns": checkpoint.CheckpointNS,
				"checkpoint_id": *checkpoint.ParentCheckpointID,
			},
		}
	}

	return tuple, nil
}
