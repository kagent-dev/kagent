package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/kagent-dev/kagent/go/internal/database"
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
	Metadata  json.RawMessage `json:"metadata"`
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

	memory := &database.Memory{
		AgentName: req.AgentName,
		UserID:    req.UserID,
		Content:   req.Content,
		Embedding: req.Vector,
		Metadata:  string(req.Metadata),
	}

	if err := h.DatabaseService.StoreAgentMemory(memory); err != nil {
		RespondWithError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save memory: %v", err))
		return
	}

	RespondWithJSON(w, http.StatusCreated, map[string]string{"id": memory.ID})
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

	if req.Limit <= 0 {
		req.Limit = 5
	}

	// Format vector string: "[0.1, 0.2, ...]"
	vectorBytes, err := json.Marshal(req.Vector)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "Failed to process vector")
		return
	}
	vectorStr := string(vectorBytes)

	results, err := h.DatabaseService.SearchAgentMemory(req.AgentName, req.UserID, vectorStr, req.Limit)
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

		response = append(response, SearchSessionMemoryResponse{
			ID:        res.ID,
			Content:   res.Content,
			Score:     res.Score,
			Metadata:  json.RawMessage(res.Metadata),
			CreatedAt: res.CreatedAt,
		})
	}

	RespondWithJSON(w, http.StatusOK, response)
}
