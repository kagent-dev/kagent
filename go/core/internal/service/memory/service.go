package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/pgvector/pgvector-go"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

const (
	VectorDimension    = 768
	MaxBatchSize       = 50
	DefaultTTLDays     = 15
	DefaultSearchLimit = 5
)

type Store interface {
	runtimeauth.SessionStore
	StoreAgentMemories(context.Context, ...*database.Memory) error
	SearchAgentMemory(context.Context, string, string, pgvector.Vector, int) ([]database.AgentMemorySearchResult, error)
	ListAgentMemories(context.Context, string, string) ([]database.Memory, error)
	DeleteAgentMemory(context.Context, string, string) error
}

type Input struct {
	Content  string
	Vector   []float32
	Metadata json.RawMessage
	TTLDays  int
}

type SearchRequest struct {
	Vector   []float32
	Limit    int
	MinScore float64
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
	store      Store
	authorizer auth.Authorizer
	now        func() time.Time
}

func NewService(store Store, authorizer auth.Authorizer, options ...Option) *Service {
	service := &Service{store: store, authorizer: authorizer, now: time.Now}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) Add(ctx context.Context, input Input) (string, error) {
	agentName, userID, err := s.scope(ctx, auth.VerbCreate)
	if err != nil {
		return "", err
	}
	if err := validateVector(input.Vector); err != nil {
		return "", err
	}
	if s.store == nil {
		return "", serviceerrors.NewInternal("failed to save memory", fmt.Errorf("database client is not configured"))
	}
	memory, err := s.toMemory(input, agentName, userID)
	if err != nil {
		return "", err
	}
	if err := s.store.StoreAgentMemories(ctx, memory); err != nil {
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

	agentName, userID, err := s.scope(ctx, auth.VerbCreate)
	if err != nil {
		return 0, err
	}
	memories := make([]*database.Memory, 0, len(inputs))
	for _, input := range inputs {
		if err := validateVector(input.Vector); err != nil {
			return 0, err
		}
		memory, err := s.toMemory(input, agentName, userID)
		if err != nil {
			return 0, err
		}
		memories = append(memories, memory)
	}
	if err := s.store.StoreAgentMemories(ctx, memories...); err != nil {
		return 0, serviceerrors.NewInternal("failed to save memory batch", err)
	}
	return len(memories), nil
}

func (s *Service) Search(ctx context.Context, request SearchRequest) ([]SearchResult, error) {
	agentName, userID, err := s.scope(ctx, auth.VerbGet)
	if err != nil {
		return nil, err
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
		agentName,
		userID,
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

func (s *Service) List(ctx context.Context, agentName string) ([]database.Memory, error) {
	agentName, userID, err := s.managementScope(ctx, auth.VerbList, agentName)
	if err != nil {
		return nil, err
	}
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

func (s *Service) Delete(ctx context.Context, agentName string) error {
	agentName, userID, err := s.managementScope(ctx, auth.VerbDelete, agentName)
	if err != nil {
		return err
	}
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

// Users can manage their own memories even when the agent or session no longer
// exists. Actors remain bound to their current session's agent and owner.
func (s *Service) managementScope(ctx context.Context, verb auth.Verb, agentName string) (string, string, error) {
	if session, ok := auth.AuthSessionFrom(ctx); ok && session != nil {
		if principal := session.Principal(); principal.RuntimeBinding == nil && principal.ID != "" {
			return agentName, principal.ID, s.authorize(ctx, verb, agentName)
		}
	}
	return s.scope(ctx, verb)
}

// scope binds memory to the persisted session's agent and owner. Request fields
// and forwarded headers never select another identity's memory namespace.
func (s *Service) scope(ctx context.Context, verb auth.Verb) (string, string, error) {
	if s.store == nil {
		return "", "", serviceerrors.NewInternal("memory store is not configured", nil)
	}
	session, err := runtimeauth.Session(ctx, s.store)
	if err != nil {
		return "", "", err
	}
	if session.GetAgent().GetNamespace() == "" || session.GetAgent().GetName() == "" || session.Creator == "" {
		return "", "", serviceerrors.NewPermissionDenied("actor has no memory scope", nil)
	}
	agentName := session.Agent.Namespace + "/" + session.Agent.Name
	return agentName, session.Creator, s.authorize(ctx, verb, agentName)
}

func (s *Service) authorize(ctx context.Context, verb auth.Verb, agentName string) error {
	// Share authority belongs only to Session reads, including for direct callers.
	if _, shared := auth.ShareContextFrom(ctx); shared {
		return serviceerrors.NewPermissionDenied("Session shares do not grant Memory access", nil)
	}
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok {
		return serviceerrors.NewUnauthenticated("Authenticated identity is required", nil)
	}
	if err := s.authorizer.Check(ctx, session.Principal(), verb, auth.Resource{Type: "Memory", Name: agentName}); err != nil {
		return serviceerrors.NewPermissionDenied("Not authorized to access memory", err)
	}
	return nil
}

func (s *Service) toMemory(input Input, agentName, userID string) (*database.Memory, error) {
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
		AgentName: agentName,
		UserID:    userID,
		Content:   input.Content,
		Embedding: pgvector.NewVector(input.Vector),
		Metadata:  string(metadata),
		ExpiresAt: &expiresAt,
	}, nil
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
