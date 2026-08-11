package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memoryStore struct {
	stored       []*database.Memory
	searchResult []database.AgentMemorySearchResult
	searchLimit  int
	listResult   []database.Memory
	err          error
	deletedAgent string
	deletedUser  string
}

func (store *memoryStore) StoreAgentMemory(_ context.Context, memory *database.Memory) error {
	if store.err != nil {
		return store.err
	}
	memory.ID = "memory-1"
	store.stored = append(store.stored, memory)
	return nil
}

func (store *memoryStore) StoreAgentMemories(_ context.Context, memories []*database.Memory) error {
	if store.err != nil {
		return store.err
	}
	store.stored = append(store.stored, memories...)
	return nil
}

func (store *memoryStore) SearchAgentMemory(_ context.Context, _, _ string, _ pgvector.Vector, limit int) ([]database.AgentMemorySearchResult, error) {
	store.searchLimit = limit
	return store.searchResult, store.err
}

func (store *memoryStore) ListAgentMemories(context.Context, string, string) ([]database.Memory, error) {
	return store.listResult, store.err
}

func (store *memoryStore) DeleteAgentMemory(_ context.Context, agentName, userID string) error {
	store.deletedAgent = agentName
	store.deletedUser = userID
	return store.err
}

func TestAddDefaultsMetadataAndTTL(t *testing.T) {
	now := time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC)
	store := &memoryStore{}
	service := NewService(store, WithClock(func() time.Time { return now }))

	id, err := service.Add(t.Context(), Input{
		AgentName: "agent",
		UserID:    "user",
		Content:   "remember this",
		Vector:    vector(VectorDimension, 0.25),
	})
	require.NoError(t, err)
	assert.Equal(t, "memory-1", id)
	require.Len(t, store.stored, 1)
	assert.Equal(t, "{}", store.stored[0].Metadata)
	assert.Equal(t, now.Add(DefaultTTLDays*24*time.Hour), *store.stored[0].ExpiresAt)
	assert.Len(t, store.stored[0].Embedding.Slice(), VectorDimension)
}

func TestAddAndBatchValidation(t *testing.T) {
	service := NewService(&memoryStore{})

	_, err := service.Add(t.Context(), Input{AgentName: "agent", UserID: "user", Vector: vector(16, 1)})
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
	assert.EqualError(t, err, "vector must have exactly 768 dimensions, got 16")

	_, err = service.Add(t.Context(), Input{AgentName: "agent", UserID: "user", Vector: vector(VectorDimension, 1), Metadata: []byte("{")})
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))

	_, err = service.AddBatch(t.Context(), nil)
	assert.EqualError(t, err, "Empty batch")

	inputs := make([]Input, MaxBatchSize+1)
	_, err = service.AddBatch(t.Context(), inputs)
	assert.EqualError(t, err, "batch size 51 exceeds maximum allowed size of 50")
}

func TestSearchDefaultsFiltersAndNormalizesMetadata(t *testing.T) {
	createdAt := time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC)
	store := &memoryStore{searchResult: []database.AgentMemorySearchResult{
		{Memory: database.Memory{ID: "high", Content: "keep", Metadata: `{"source":"test"}`, CreatedAt: createdAt}, Score: 0.9},
		{Memory: database.Memory{ID: "invalid", Content: "normalize", Metadata: "{"}, Score: 0.8},
		{Memory: database.Memory{ID: "low", Content: "drop"}, Score: 0.4},
	}}
	service := NewService(store)

	results, err := service.Search(t.Context(), SearchRequest{
		AgentName: "agent",
		UserID:    "user",
		Vector:    vector(VectorDimension, 0.25),
		MinScore:  0.5,
	})
	require.NoError(t, err)
	assert.Equal(t, DefaultSearchLimit, store.searchLimit)
	require.Len(t, results, 2)
	assert.Equal(t, "high", results[0].ID)
	assert.JSONEq(t, `{"source":"test"}`, string(results[0].Metadata))
	assert.JSONEq(t, `{}`, string(results[1].Metadata))
}

func TestListDeleteAndStoreErrors(t *testing.T) {
	store := &memoryStore{listResult: []database.Memory{{ID: "memory-1"}}}
	service := NewService(store)

	memories, err := service.List(t.Context(), "agent", "user")
	require.NoError(t, err)
	assert.Equal(t, "memory-1", memories[0].ID)
	require.NoError(t, service.Delete(t.Context(), "agent", "user"))
	assert.Equal(t, "agent", store.deletedAgent)
	assert.Equal(t, "user", store.deletedUser)

	store.err = errors.New("database unavailable")
	_, err = service.Search(t.Context(), SearchRequest{AgentName: "agent", UserID: "user", Vector: vector(VectorDimension, 1)})
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInternal))
	assert.ErrorContains(t, err, "database unavailable")
}

func vector(length int, value float32) []float32 {
	result := make([]float32, length)
	for index := range result {
		result[index] = value
	}
	return result
}
