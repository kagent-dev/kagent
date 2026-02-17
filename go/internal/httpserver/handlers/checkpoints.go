package handlers

import (
	"net/http"
	"strconv"

	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	"github.com/kagent-dev/kagent/go/pkg/database"
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

// KAgent checkpoint types converted from Python Pydantic models

// KAgentCheckpointPayload represents checkpoint payload data
type KAgentCheckpointPayload struct {
	ThreadID           string  `json:"thread_id"`
	CheckpointNS       string  `json:"checkpoint_ns"`
	CheckpointID       string  `json:"checkpoint_id"`
	ParentCheckpointID *string `json:"parent_checkpoint_id"`
	Checkpoint         string  `json:"checkpoint"`
	Metadata           string  `json:"metadata"`
	Type               string  `json:"type_"`
	Version            int     `json:"version"`
}

// KagentCheckpointWrite represents a single checkpoint write operation
type KagentCheckpointWrite struct {
	Idx     int    `json:"idx"`
	Channel string `json:"channel"`
	Type    string `json:"type_"`
	Value   string `json:"value"`
}

// KAgentCheckpointWritePayload represents checkpoint write payload data
type KAgentCheckpointWritePayload struct {
	ThreadID     string                  `json:"thread_id"`
	CheckpointNS string                  `json:"checkpoint_ns"`
	CheckpointID string                  `json:"checkpoint_id"`
	TaskID       string                  `json:"task_id"`
	Writes       []KagentCheckpointWrite `json:"writes"`
}

// KAgentCheckpointTuple represents a complete checkpoint tuple
type KAgentCheckpointTuple struct {
	ThreadID           string                        `json:"thread_id"`
	CheckpointNS       string                        `json:"checkpoint_ns"`
	CheckpointID       string                        `json:"checkpoint_id"`
	ParentCheckpointID *string                       `json:"parent_checkpoint_id"`
	Checkpoint         string                        `json:"checkpoint"`
	Metadata           string                        `json:"metadata"`
	Type               string                        `json:"type_"`
	Writes             *KAgentCheckpointWritePayload `json:"writes"`
}

// KAgentCheckpointTupleResponse represents the response containing checkpoint tuples
type KAgentCheckpointTupleResponse struct {
	Data []KAgentCheckpointTuple `json:"data"`
}

// HandlePutCheckpoint handles POST /api/langgraph/checkpoints requests
func (h *CheckpointsHandler) HandlePutCheckpoint(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("checkpoints-handler").WithValues("operation", "put")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	var req KAgentCheckpointPayload
	if err := DecodeJSONBody(r, &req); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	// Validate required fields
	if req.ThreadID == "" {
		w.RespondWithError(errors.NewBadRequestError("thread_id is required", nil))
		return
	}
	if req.Checkpoint == "" {
		w.RespondWithError(errors.NewBadRequestError("checkpoint is required", nil))
		return
	}

	log = log.WithValues(
		"threadID", req.ThreadID,
		"checkpointNS", req.CheckpointNS,
		"checkpointID", req.CheckpointID,
	)

	// Create checkpoint model
	checkpoint := &database.LangGraphCheckpoint{
		UserID:             userID,
		ThreadID:           req.ThreadID,
		CheckpointNS:       req.CheckpointNS,
		CheckpointID:       req.CheckpointID,
		ParentCheckpointID: req.ParentCheckpointID,
		Metadata:           req.Metadata,
		Checkpoint:         req.Checkpoint,
		Version:            req.Version,
		CheckpointType:     req.Type,
	}
	// Store checkpoint and writes atomically
	if err := h.DatabaseService.StoreCheckpoint(checkpoint); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to store checkpoint", err))
		return
	}

	log.Info("Successfully stored checkpoint")
	data := api.NewResponse(struct{}{}, "Successfully stored checkpoint", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

// HandleListCheckpoints handles GET /api/langgraph/checkpoints requests
func (h *CheckpointsHandler) HandleListCheckpoints(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("checkpoints-handler").WithValues("operation", "list")

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

	checkpointNS := r.URL.Query().Get("checkpoint_ns")

	var checkpointID *string
	if checkpointIDStr := r.URL.Query().Get("checkpoint_id"); checkpointIDStr != "" {
		checkpointID = &checkpointIDStr
	}

	limit := 0
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil {
			limit = parsedLimit
		}
	}

	log = log.WithValues("userID", userID, "threadID", threadID, "checkpointNS", checkpointNS, "limit", limit)

	log.V(1).Info("Listing checkpoints")
	checkpoints, err := h.DatabaseService.ListCheckpoints(userID, threadID, checkpointNS, checkpointID, limit)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list checkpoints", err))
		return
	}

	// Convert to response format (without writes for list operation)
	tuples := make([]KAgentCheckpointTuple, len(checkpoints))
	for i, tuple := range checkpoints {
		taskID := ""
		writes := make([]KagentCheckpointWrite, len(tuple.Writes))
		for j, write := range tuple.Writes {
			taskID = write.TaskID
			writes[j] = KagentCheckpointWrite{
				Idx:     write.WriteIdx,
				Channel: write.Channel,
				Type:    write.ValueType,
				Value:   write.Value,
			}
		}
		tuples[i] = KAgentCheckpointTuple{
			ThreadID:           tuple.Checkpoint.ThreadID,
			CheckpointNS:       tuple.Checkpoint.CheckpointNS,
			CheckpointID:       tuple.Checkpoint.CheckpointID,
			Checkpoint:         tuple.Checkpoint.Checkpoint,
			Metadata:           tuple.Checkpoint.Metadata,
			Type:               tuple.Checkpoint.CheckpointType,
			ParentCheckpointID: tuple.Checkpoint.ParentCheckpointID,
			Writes: &KAgentCheckpointWritePayload{
				ThreadID:     tuple.Checkpoint.ThreadID,
				CheckpointNS: tuple.Checkpoint.CheckpointNS,
				CheckpointID: tuple.Checkpoint.CheckpointID,
				TaskID:       taskID,
				Writes:       writes,
			},
		}
	}

	log.Info("Successfully listed checkpoints", "count", len(tuples))
	data := api.NewResponse(tuples, "Successfully listed checkpoints", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandlePutWrites handles POST /api/langgraph/checkpoints/writes requests
func (h *CheckpointsHandler) HandlePutWrites(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("checkpoints-handler").WithValues("operation", "put")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}

	log = log.WithValues("userID", userID)

	var req KAgentCheckpointWritePayload
	if err := DecodeJSONBody(r, &req); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	log = log.WithValues(
		"threadID", req.ThreadID,
		"checkpointNS", req.CheckpointNS,
		"checkpointID", req.CheckpointID,
	)

	// Prepare writes
	writes := make([]*database.LangGraphCheckpointWrite, len(req.Writes))
	for i, writeReq := range req.Writes {
		writes[i] = &database.LangGraphCheckpointWrite{
			UserID:       userID,
			ThreadID:     req.ThreadID,
			CheckpointNS: req.CheckpointNS,
			CheckpointID: req.CheckpointID,
			WriteIdx:     writeReq.Idx,
			Value:        writeReq.Value,
			ValueType:    writeReq.Type,
			Channel:      writeReq.Channel,
			TaskID:       req.TaskID,
		}
	}

	log.V(1).Info("Storing checkpoint with writes", "writesCount", len(writes))

	// Store checkpoint and writes atomically
	if err := h.DatabaseService.StoreCheckpointWrites(writes); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to store checkpoint writes", err))
		return
	}

	log.Info("Successfully stored checkpoint writes")
	data := api.NewResponse(struct{}{}, "Successfully stored checkpoint writes", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

// HandleDeleteThread handles DELETE /api/langgraph/checkpoints/{thread_id} requests
func (h *CheckpointsHandler) HandleDeleteThread(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("checkpoints-handler").WithValues("operation", "delete")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}

	threadID, err := GetPathParam(r, "thread_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get thread_id from path", err))
		return
	}

	log = log.WithValues("userID", userID, "threadID", threadID)

	if err := h.DatabaseService.DeleteCheckpoint(userID, threadID); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to delete thread", err))
		return
	}

	log.Info("Successfully deleted thread")
	data := api.NewResponse(struct{}{}, "Successfully deleted thread", false)
	RespondWithJSON(w, http.StatusOK, data)
}
