package memory

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type memoryStore struct {
	bindingErr   error
	stored       []*database.Memory
	searchResult []database.AgentMemorySearchResult
	searchLimit  int
	listResult   []database.Memory
	err          error
	deletedAgent string
	deletedUser  string
	readAgent    string
	readUser     string
}

func (store *memoryStore) StoreAgentMemories(_ context.Context, memories ...*database.Memory) error {
	if store.err != nil {
		return store.err
	}
	for _, memory := range memories {
		memory.ID = "memory-1"
	}
	store.stored = append(store.stored, memories...)
	return nil
}

func (store *memoryStore) SearchAgentMemory(_ context.Context, agentName, userID string, _ pgvector.Vector, limit int) ([]database.AgentMemorySearchResult, error) {
	store.readAgent, store.readUser = agentName, userID
	store.searchLimit = limit
	return store.searchResult, store.err
}

func (store *memoryStore) ListAgentMemories(_ context.Context, agentName, userID string) ([]database.Memory, error) {
	store.readAgent, store.readUser = agentName, userID
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
	service := NewService(store, auth.NoopAuthorizer{}, WithClock(func() time.Time { return now }))

	id, err := service.Add(memoryContext(t), Input{
		Content: "remember this",
		Vector:  vector(VectorDimension, 0.25),
	})
	require.NoError(t, err)
	assert.Equal(t, "memory-1", id)
	require.Len(t, store.stored, 1)
	assert.Equal(t, "{}", store.stored[0].Metadata)
	assert.Equal(t, now.Add(DefaultTTLDays*24*time.Hour), *store.stored[0].ExpiresAt)
	assert.Len(t, store.stored[0].Embedding.Slice(), VectorDimension)
}

func TestAddAndBatchValidation(t *testing.T) {
	service := NewService(&memoryStore{}, auth.NoopAuthorizer{})

	_, err := service.Add(memoryContext(t), Input{Vector: vector(16, 1)})
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
	assert.EqualError(t, err, "vector must have exactly 768 dimensions, got 16")

	_, err = service.Add(memoryContext(t), Input{Vector: vector(VectorDimension, 1), Metadata: []byte("{")})
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))

	_, err = service.AddBatch(memoryContext(t), nil)
	assert.EqualError(t, err, "Empty batch")

	inputs := make([]Input, MaxBatchSize+1)
	_, err = service.AddBatch(memoryContext(t), inputs)
	assert.EqualError(t, err, "batch size 51 exceeds maximum allowed size of 50")
}

func TestSearchDefaultsFiltersAndNormalizesMetadata(t *testing.T) {
	createdAt := time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC)
	store := &memoryStore{searchResult: []database.AgentMemorySearchResult{
		{Memory: database.Memory{ID: "high", Content: "keep", Metadata: `{"source":"test"}`, CreatedAt: createdAt}, Score: 0.9},
		{Memory: database.Memory{ID: "invalid", Content: "normalize", Metadata: "{"}, Score: 0.8},
		{Memory: database.Memory{ID: "low", Content: "drop"}, Score: 0.4},
	}}
	service := NewService(store, auth.NoopAuthorizer{})

	results, err := service.Search(memoryContext(t), SearchRequest{
		Vector:   vector(VectorDimension, 0.25),
		MinScore: 0.5,
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
	service := NewService(store, auth.NoopAuthorizer{})

	memories, err := service.List(memoryContext(t), "")
	require.NoError(t, err)
	assert.Equal(t, "memory-1", memories[0].ID)
	require.NoError(t, service.Delete(memoryContext(t), ""))
	assert.Equal(t, "team-a/agent", store.deletedAgent)
	assert.Equal(t, "user", store.deletedUser)

	store.err = errors.New("database unavailable")
	_, err = service.Search(memoryContext(t), SearchRequest{Vector: vector(VectorDimension, 1)})
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

func (store *memoryStore) GetSessionForRuntime(_ context.Context, id, _ string) (*apiv1.Session, error) {
	if store.bindingErr != nil {
		return nil, store.bindingErr
	}
	return &apiv1.Session{Id: id, Creator: "user", Agent: &apiv1.ResourceReference{Namespace: "team-a", Name: "agent"}, A2AAuthority: substrate.ActorHost("team-a", substrate.ActorName(id), "")}, nil
}

type testActorVerifier struct{}

func (testActorVerifier) Verify(context.Context, string) (substrate.ActorIdentity, error) {
	return substrate.ActorIdentity{ActorName: "session-00000000-0000-0000-0000-000000000001", Atespace: "team-a", ActorUID: "actor"}, nil
}

func memoryContext(t *testing.T) context.Context {
	t.Helper()
	session, err := runtimeauth.NewAuthenticator(testActorVerifier{}).Authenticate(t.Context(), http.Header{"Authorization": {"Bearer test"}, "X-User-Id": {"forged"}, "X-Agent-Name": {"forged"}}, nil)
	require.NoError(t, err)
	require.Equal(t, auth.PrincipalID("substrate-actor", "team-a/actor"), session.Principal().ID)
	return auth.AuthSessionTo(t.Context(), session)
}

func TestActorMemoryUsesSessionScope(t *testing.T) {
	store := &memoryStore{}
	service := NewService(store, auth.NoopAuthorizer{})
	ctx := memoryContext(t)
	input := Input{Content: "scoped", Vector: vector(VectorDimension, 1)}
	_, err := service.Add(ctx, input)
	require.NoError(t, err)
	_, err = service.AddBatch(ctx, []Input{input})
	require.NoError(t, err)
	require.Len(t, store.stored, 2)
	for _, memory := range store.stored {
		require.Equal(t, "team-a/agent", memory.AgentName)
		require.Equal(t, "user", memory.UserID)
	}
	require.NoError(t, service.Delete(ctx, "other/agent"))
	require.Equal(t, "team-a/agent", store.deletedAgent)
	require.Equal(t, "user", store.deletedUser)
	_, err = service.List(ctx, "other/agent")
	require.NoError(t, err)
	require.Equal(t, "team-a/agent", store.readAgent)
	require.Equal(t, "user", store.readUser)
	_, err = service.Search(ctx, SearchRequest{Vector: input.Vector})
	require.NoError(t, err)
	require.Equal(t, "team-a/agent", store.readAgent)
	require.Equal(t, "user", store.readUser)
}

func TestMemoryRejectsUnboundActors(t *testing.T) {
	for _, test := range []struct {
		name          string
		authenticated bool
		bindingErr    error
	}{
		{name: "missing identity"},
		{name: "deleted or replaced actor", authenticated: true, bindingErr: database.ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &memoryStore{bindingErr: test.bindingErr}
			service := NewService(store, auth.NoopAuthorizer{})
			ctx := t.Context()
			if test.authenticated {
				ctx = memoryContext(t)
			}
			input := Input{Vector: vector(VectorDimension, 1)}
			_, err := service.Add(ctx, input)
			require.Equal(t, codes.PermissionDenied, status.Code(err))
			_, err = service.AddBatch(ctx, []Input{input})
			require.Equal(t, codes.PermissionDenied, status.Code(err))
			_, err = service.Search(ctx, SearchRequest{Vector: input.Vector})
			require.Equal(t, codes.PermissionDenied, status.Code(err))
			_, err = service.List(ctx, "other/agent")
			require.Equal(t, codes.PermissionDenied, status.Code(err))
			err = service.Delete(ctx, "other/agent")
			require.Equal(t, codes.PermissionDenied, status.Code(err))
			require.Empty(t, store.stored)
			require.Empty(t, store.deletedAgent)
		})
	}
}

func TestUserMemoryManagementDoesNotRequireRuntime(t *testing.T) {
	store := &memoryStore{bindingErr: database.ErrNotFound}
	service := NewService(store, auth.NoopAuthorizer{})
	ctx := auth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: auth.Principal{ID: "owner"}})

	for _, agentName := range []string{"team-a/agent", "legacy-display-name"} {
		_, err := service.List(ctx, agentName)
		require.NoError(t, err)
		require.Equal(t, agentName, store.readAgent)
		require.Equal(t, "owner", store.readUser)
		require.NoError(t, service.Delete(ctx, agentName))
		require.Equal(t, agentName, store.deletedAgent)
		require.Equal(t, "owner", store.deletedUser)
	}

	_, err := service.List(ctx, "")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
	require.True(t, serviceerrors.IsCode(service.Delete(ctx, ""), serviceerrors.CodeInvalidArgument))
}

type deniedMemoryAuthorizer struct{ auth.NoopAuthorizer }

func (deniedMemoryAuthorizer) Check(context.Context, auth.Principal, auth.Verb, auth.Resource) error {
	return errors.New("memory access denied")
}

func TestMemoryManagementHonorsConfiguredAuthorization(t *testing.T) {
	store := &memoryStore{}
	service := NewService(store, deniedMemoryAuthorizer{})
	user := auth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: auth.Principal{ID: "user"}})
	for _, ctx := range []context.Context{user, memoryContext(t)} {
		_, err := service.List(ctx, "team-a/agent")
		require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
		err = service.Delete(ctx, "team-a/agent")
		require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
	}
	require.Empty(t, store.deletedAgent)
	require.Empty(t, store.deletedUser)
}

func TestMemorySharesNeverReachPartitionStorage(t *testing.T) {
	user := auth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: auth.Principal{ID: "owner"}})
	for _, ctx := range []context.Context{user, memoryContext(t)} {
		store := &memoryStore{}
		service := NewService(store, auth.NoopAuthorizer{})
		shared := auth.ShareContextTo(ctx, &auth.ShareContext{SessionID: "shared-session", UserID: "owner"})
		operations := []struct {
			name string
			call func() error
		}{
			{"add", func() error { _, err := service.Add(shared, Input{Vector: vector(VectorDimension, 1)}); return err }},
			{"batch", func() error {
				_, err := service.AddBatch(shared, []Input{{Vector: vector(VectorDimension, 1)}})
				return err
			}},
			{"search", func() error {
				_, err := service.Search(shared, SearchRequest{Vector: vector(VectorDimension, 1)})
				return err
			}},
			{"list", func() error { _, err := service.List(shared, "team-a/agent"); return err }},
			{"delete", func() error { return service.Delete(shared, "team-a/agent") }},
		}
		for _, op := range operations {
			t.Run(op.name, func(t *testing.T) {
				err := op.call()
				require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied) || status.Code(err) == codes.PermissionDenied, "%v", err)
			})
		}
		require.Empty(t, store.stored)
		require.Empty(t, store.readAgent)
		require.Empty(t, store.deletedAgent)
	}
}
