package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/pgvector/pgvector-go"
)

const (
	VectorDimension    = 768
	MaxBatchSize       = 50
	DefaultTTLDays     = 15
	DefaultSearchLimit = 5
)

type Store interface {
	StoreAgentMemory(context.Context, *database.Memory) error
	StoreAgentMemories(context.Context, []*database.Memory) error
	SearchAgentMemory(context.Context, string, string, pgvector.Vector, int) ([]database.AgentMemorySearchResult, error)
	ListAgentMemories(context.Context, string, string) ([]database.Memory, error)
	DeleteAgentMemory(context.Context, string, string) error
}

type Input struct {
	AgentName string
	UserID    string
	Content   string
	Vector    []float32
	Metadata  json.RawMessage
	TTLDays   int
}

type SearchRequest struct {
	AgentName string
	UserID    string
	Vector    []float32
	Limit     int
	MinScore  float64
}

type SearchResult struct {
	ID        string
	Content   string
	Score     float64
	Metadata  json.RawMessage
	CreatedAt time.Time
}

type Option func(*Service)

func WithClock(now func() time.Time) Option {
	return func(service *Service) {
		service.now = now
	}
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store, options ...Option) *Service {
	service := &Service{store: store, now: time.Now}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) Add(ctx context.Context, input Input) (string, error) {
	if err := validateInput(input, "Missing required fields (agent_name, user_id, vector)"); err != nil {
		return "", err
	}
	if s.store == nil {
		return "", serviceerrors.NewInternal("failed to save memory", fmt.Errorf("database client is not configured"))
	}
	memory, err := s.toMemory(input)
	if err != nil {
		return "", err
	}
	if err := s.store.StoreAgentMemory(ctx, memory); err != nil {
		return "", serviceerrors.NewInternal("failed to save memory", err)
	}
	return memory.ID, nil
}

func (s *Service) AddBatch(ctx context.Context, inputs []Input) (int, error) {
	if len(inputs) == 0 {
		return 0, serviceerrors.NewInvalidArgument("Empty batch", nil)
	}
	if len(inputs) > MaxBatchSize {
		return 0, serviceerrors.NewInvalidArgument(
			fmt.Sprintf("batch size %d exceeds maximum allowed size of %d", len(inputs), MaxBatchSize),
			nil,
		)
	}
	if s.store == nil {
		return 0, serviceerrors.NewInternal("failed to save memory batch", fmt.Errorf("database client is not configured"))
	}

	memories := make([]*database.Memory, 0, len(inputs))
	for _, input := range inputs {
		if err := validateInput(input, "Missing required fields in batch item"); err != nil {
			return 0, err
		}
		memory, err := s.toMemory(input)
		if err != nil {
			return 0, err
		}
		memories = append(memories, memory)
	}
	if err := s.store.StoreAgentMemories(ctx, memories); err != nil {
		return 0, serviceerrors.NewInternal("failed to save memory batch", err)
	}
	return len(memories), nil
}

func (s *Service) Search(ctx context.Context, request SearchRequest) ([]SearchResult, error) {
	if request.AgentName == "" || request.UserID == "" || len(request.Vector) == 0 {
		return nil, serviceerrors.NewInvalidArgument("Missing required fields (agent_name, user_id, vector)", nil)
	}
	if err := validateVector(request.Vector); err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, serviceerrors.NewInternal("search failed", fmt.Errorf("database client is not configured"))
	}
	if request.Limit <= 0 {
		request.Limit = DefaultSearchLimit
	}
	results, err := s.store.SearchAgentMemory(
		ctx,
		request.AgentName,
		request.UserID,
		pgvector.NewVector(request.Vector),
		request.Limit,
	)
	if err != nil {
		return nil, serviceerrors.NewInternal("search failed", err)
	}

	response := make([]SearchResult, 0, len(results))
	for _, result := range results {
		if request.MinScore > 0 && result.Score < request.MinScore {
			continue
		}
		metadata := json.RawMessage(result.Metadata)
		if len(metadata) == 0 || !json.Valid(metadata) {
			metadata = json.RawMessage("{}")
		}
		response = append(response, SearchResult{
			ID:        result.ID,
			Content:   result.Content,
			Score:     result.Score,
			Metadata:  metadata,
			CreatedAt: result.CreatedAt,
		})
	}
	return response, nil
}

func (s *Service) List(ctx context.Context, agentName, userID string) ([]database.Memory, error) {
	if agentName == "" || userID == "" {
		return nil, serviceerrors.NewInvalidArgument("Missing required query parameters (agent_name, user_id)", nil)
	}
	if s.store == nil {
		return nil, serviceerrors.NewInternal("failed to list memories", fmt.Errorf("database client is not configured"))
	}
	memories, err := s.store.ListAgentMemories(ctx, agentName, userID)
	if err != nil {
		return nil, serviceerrors.NewInternal("failed to list memories", err)
	}
	return memories, nil
}

func (s *Service) Delete(ctx context.Context, agentName, userID string) error {
	if agentName == "" || userID == "" {
		return serviceerrors.NewInvalidArgument("Missing required query parameters (agent_name, user_id)", nil)
	}
	if s.store == nil {
		return serviceerrors.NewInternal("failed to delete memory", fmt.Errorf("database client is not configured"))
	}
	if err := s.store.DeleteAgentMemory(ctx, agentName, userID); err != nil {
		return serviceerrors.NewInternal("failed to delete memory", err)
	}
	return nil
}

func (s *Service) toMemory(input Input) (*database.Memory, error) {
	metadata := input.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	} else if !json.Valid(metadata) {
		return nil, serviceerrors.NewInvalidArgument("metadata must be valid JSON", nil)
	}
	ttlDays := input.TTLDays
	if ttlDays <= 0 {
		ttlDays = DefaultTTLDays
	}
	expiresAt := s.now().Add(time.Duration(ttlDays) * 24 * time.Hour)
	return &database.Memory{
		AgentName: input.AgentName,
		UserID:    input.UserID,
		Content:   input.Content,
		Embedding: pgvector.NewVector(input.Vector),
		Metadata:  string(metadata),
		ExpiresAt: &expiresAt,
	}, nil
}

func validateInput(input Input, missingFieldsMessage string) error {
	if input.AgentName == "" || input.UserID == "" || len(input.Vector) == 0 {
		return serviceerrors.NewInvalidArgument(missingFieldsMessage, nil)
	}
	return validateVector(input.Vector)
}

func validateVector(vector []float32) error {
	if len(vector) != VectorDimension {
		return serviceerrors.NewInvalidArgument(
			fmt.Sprintf("vector must have exactly %d dimensions, got %d", VectorDimension, len(vector)),
			nil,
		)
	}
	return nil
}
