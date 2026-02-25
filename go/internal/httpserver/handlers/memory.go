package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/kagent-dev/kagent/go/pkg/database"
	"github.com/pgvector/pgvector-go"
)

const (
	// memoryVectorDimension is the required dimension for all embedding vectors.
	memoryVectorDimension = 768
	// memoryMaxBatchSize is the maximum number of items accepted in a single batch request.
	memoryMaxBatchSize = 50
	// defaultMemoryTTLDays is used when the caller does not supply a ttl_days value.
	defaultMemoryTTLDays = 15
)

// MemoryHandler handles Memory requests
type MemoryHandler struct {
	*Base
}

// NewMemoryHandler creates a new MemoryHandler
func NewMemoryHandler(base *Base) *MemoryHandler {
	return &MemoryHandler{Base: base}
}

// AddSessionMemoryRequest represents the request body for adding a memory session
type AddSessionMemoryRequest struct {
	AgentName string          `json:"agent_name"`
	UserID    string          `json:"user_id"`
	Content   string          `json:"content"`
	Vector    []float32       `json:"vector"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	TTLDays   int             `json:"ttl_days,omitempty"`
}

// SearchSessionMemoryRequest represents the request body for searching memory sessions
type SearchSessionMemoryRequest struct {
	AgentName string    `json:"agent_name"`
	UserID    string    `json:"user_id"`
	Vector    []float32 `json:"vector"`
	Limit     int       `json:"limit"`
	MinScore  float64   `json:"min_score"` // Minimum similarity score (0-1)
}

// SearchSessionMemoryResponse represents a found memory item
type SearchSessionMemoryResponse struct {
	ID        string          `json:"id"`
	Content   string          `json:"content"`
	Score     float64         `json:"score"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"created_at"`
}

// ListMemoryResponse represents a single memory item for the list endpoint
type ListMemoryResponse struct {
	ID          string `json:"id"`
	Content     string `json:"content"`
	AccessCount int    `json:"access_count"`
	CreatedAt   string `json:"created_at"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// AddSession handles POST /api/memories/sessions
func (h *MemoryHandler) AddSession(w ErrorResponseWriter, r *http.Request) {
	var req AddSessionMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.AgentName == "" || req.UserID == "" || len(req.Vector) == 0 {
		RespondWithError(w, http.StatusBadRequest, "Missing required fields (agent_name, user_id, vector)")
		return
	}

	if len(req.Vector) != memoryVectorDimension {
		RespondWithError(w, http.StatusBadRequest, fmt.Sprintf("vector must have exactly %d dimensions, got %d", memoryVectorDimension, len(req.Vector)))
		return
	}

	// Ensure metadata is valid JSON
	metadata := req.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}

	ttlDays := req.TTLDays
	if ttlDays <= 0 {
		ttlDays = defaultMemoryTTLDays
	}
	expiresAt := time.Now().Add(time.Duration(ttlDays) * 24 * time.Hour)
	memory := &database.Memory{
		AgentName: req.AgentName,
		UserID:    req.UserID,
		Content:   req.Content,
		Embedding: pgvector.NewVector(req.Vector),
		Metadata:  string(metadata),
		ExpiresAt: &expiresAt,
	}

	if err := h.DatabaseService.StoreAgentMemory(memory); err != nil {
		log.Printf("Failed to store agent memory: %v", err)
		RespondWithError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save memory: %v", err))
		return
	}

	log.Printf("Successfully added memory ID %s for user %s agent %s", memory.ID, req.UserID, req.AgentName)

	RespondWithJSON(w, http.StatusCreated, map[string]string{"id": memory.ID})
}

// AddSessionMemoryBatchRequest represents the request body for adding multiple memory sessions
type AddSessionMemoryBatchRequest struct {
	Items []AddSessionMemoryRequest `json:"items"`
}

// AddSessionBatch handles POST /api/memories/sessions/batch
func (h *MemoryHandler) AddSessionBatch(w ErrorResponseWriter, r *http.Request) {
	var req AddSessionMemoryBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.Items) == 0 {
		RespondWithError(w, http.StatusBadRequest, "Empty batch")
		return
	}

	if len(req.Items) > memoryMaxBatchSize {
		RespondWithError(w, http.StatusBadRequest, fmt.Sprintf("batch size %d exceeds maximum allowed size of %d", len(req.Items), memoryMaxBatchSize))
		return
	}

	var memories []*database.Memory

	for _, item := range req.Items {
		if item.AgentName == "" || item.UserID == "" || len(item.Vector) == 0 {
			RespondWithError(w, http.StatusBadRequest, "Missing required fields in batch item")
			return
		}

		if len(item.Vector) != memoryVectorDimension {
			RespondWithError(w, http.StatusBadRequest, fmt.Sprintf("vector must have exactly %d dimensions, got %d", memoryVectorDimension, len(item.Vector)))
			return
		}

		// Ensure metadata is valid JSON
		metadata := item.Metadata
		if len(metadata) == 0 {
			metadata = json.RawMessage("{}")
		}

		ttlDays := item.TTLDays
		if ttlDays <= 0 {
			ttlDays = defaultMemoryTTLDays
		}
		expiresAt := time.Now().Add(time.Duration(ttlDays) * 24 * time.Hour)
		memories = append(memories, &database.Memory{
			AgentName: item.AgentName,
			UserID:    item.UserID,
			Content:   item.Content,
			Embedding: pgvector.NewVector(item.Vector),
			Metadata:  string(metadata),
			ExpiresAt: &expiresAt,
		})
	}

	if err := h.DatabaseService.StoreAgentMemories(memories); err != nil {
		log.Printf("Failed to store agent memory batch: %v", err)
		RespondWithError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save memory batch: %v", err))
		return
	}

	log.Printf("Successfully added %d memory items", len(memories))
	RespondWithJSON(w, http.StatusCreated, map[string]int{"count": len(memories)})
}

// Search handles POST /api/memories/search
func (h *MemoryHandler) Search(w ErrorResponseWriter, r *http.Request) {
	var req SearchSessionMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.AgentName == "" || req.UserID == "" || len(req.Vector) == 0 {
		RespondWithError(w, http.StatusBadRequest, "Missing required fields (agent_name, user_id, vector)")
		return
	}

	if len(req.Vector) != memoryVectorDimension {
		RespondWithError(w, http.StatusBadRequest, fmt.Sprintf("vector must have exactly %d dimensions, got %d", memoryVectorDimension, len(req.Vector)))
		return
	}

	if req.Limit <= 0 {
		req.Limit = 5
	}

	// Format vector using pgvector.NewVector
	vector := pgvector.NewVector(req.Vector)

	// Update DB client call to pass pgvector.Vector
	results, err := h.DatabaseService.SearchAgentMemory(req.AgentName, req.UserID, vector, req.Limit)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, fmt.Sprintf("search failed: %v", err))
		return
	}

	response := make([]SearchSessionMemoryResponse, 0, len(results))
	for _, res := range results {
		// Filter by MinScore if provided
		if req.MinScore > 0 && res.Score < req.MinScore {
			continue
		}

		// Handle empty or invalid metadata
		metadata := json.RawMessage(res.Metadata)
		if len(metadata) == 0 {
			metadata = json.RawMessage("{}")
		}

		response = append(response, SearchSessionMemoryResponse{
			ID:        res.ID,
			Content:   res.Content,
			Score:     res.Score,
			Metadata:  metadata,
			CreatedAt: res.CreatedAt,
		})
	}

	RespondWithJSON(w, http.StatusOK, response)
}

// List handles GET /api/memories and returns all memories for an agent+user, ranked by access frequency
func (h *MemoryHandler) List(w ErrorResponseWriter, r *http.Request) {
	agentName := r.URL.Query().Get("agent_name")
	userID := r.URL.Query().Get("user_id")

	if agentName == "" || userID == "" {
		RespondWithError(w, http.StatusBadRequest, "Missing required query parameters (agent_name, user_id)")
		return
	}

	memories, err := h.DatabaseService.ListAgentMemories(agentName, userID)
	if err != nil {
		log.Printf("Failed to list agent memories: %v", err)
		RespondWithError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list memories: %v", err))
		return
	}

	response := make([]ListMemoryResponse, 0, len(memories))
	for _, m := range memories {
		item := ListMemoryResponse{
			ID:          m.ID,
			Content:     m.Content,
			AccessCount: m.AccessCount,
			CreatedAt:   m.CreatedAt.Format(time.RFC3339),
		}
		if m.ExpiresAt != nil {
			item.ExpiresAt = m.ExpiresAt.Format(time.RFC3339)
		}
		response = append(response, item)
	}

	RespondWithJSON(w, http.StatusOK, response)
}

// Delete handles DELETE /api/memories
func (h *MemoryHandler) Delete(w ErrorResponseWriter, r *http.Request) {
	agentName := r.URL.Query().Get("agent_name")
	userID := r.URL.Query().Get("user_id")

	if agentName == "" || userID == "" {
		RespondWithError(w, http.StatusBadRequest, "Missing required query parameters (agent_name, user_id)")
		return
	}

	if err := h.DatabaseService.DeleteAgentMemory(agentName, userID); err != nil {
		log.Printf("Failed to delete agent memory: %v", err)
		RespondWithError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete memory: %v", err))
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
