package memory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func (store *memoryStore) DeleteMemoryEntry(_ context.Context, agentName, userID, _ string) error {
	store.deletedAgent, store.deletedUser = agentName, userID
	return store.err
}

func TestManagedMemoryUsesAuthenticatedOwner(t *testing.T) {
	store := &memoryStore{bindingErr: database.ErrNotFound}
	service := NewService(store, auth.NoopAuthorizer{})
	ctx := auth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: auth.Principal{ID: "owner"}})
	input := Input{AgentName: "team-a/knowledge", Content: "shared finding", Vector: vector(VectorDimension, 1)}
	_, err := service.Add(ctx, input)
	require.NoError(t, err)
	_, err = service.AddBatch(ctx, []Input{input})
	require.NoError(t, err)
	for _, record := range store.stored {
		require.Equal(t, "team-a/knowledge", record.AgentName)
		require.Equal(t, "owner", record.UserID)
	}
	_, err = service.Search(ctx, SearchRequest{AgentName: input.AgentName, Vector: vector(VectorDimension, 1)})
	require.NoError(t, err)
	require.Equal(t, input.AgentName, store.readAgent)
	require.Equal(t, "owner", store.readUser)
	require.NoError(t, service.DeleteEntry(ctx, input.AgentName, "selected"))
	require.Equal(t, "owner", store.deletedUser)
	store.err = database.ErrNotFound
	require.True(t, serviceerrors.IsCode(service.DeleteEntry(ctx, input.AgentName, "foreign"), serviceerrors.CodeNotFound))
}
