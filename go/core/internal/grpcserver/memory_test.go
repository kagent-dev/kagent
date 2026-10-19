package grpcserver

import (
	"context"
	"encoding/base64"
	"net"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	memoryservice "github.com/kagent-dev/kagent/go/core/internal/service/memory"
)

type userMemoryStore struct {
	*database.Client
	memories []database.Memory
}

var _ memoryservice.Store = (*userMemoryStore)(nil)

func (s *userMemoryStore) ListAgentMemories(_ context.Context, agentName, userID string) ([]database.Memory, error) {
	var result []database.Memory
	for _, memory := range s.memories {
		if memory.AgentName == agentName && memory.UserID == userID {
			result = append(result, memory)
		}
	}
	return result, nil
}

func (s *userMemoryStore) DeleteAgentMemory(_ context.Context, agentName, userID string) error {
	s.memories = slices.DeleteFunc(s.memories, func(memory database.Memory) bool {
		return memory.AgentName == agentName && memory.UserID == userID
	})
	return nil
}

func TestMemoryUsersCanListAndDeleteOnlyTheirOwnData(t *testing.T) {
	store := &userMemoryStore{memories: []database.Memory{
		{ID: "own", AgentName: "team/agent", UserID: "alice", Content: "private memory"},
		{ID: "other-user", AgentName: "team/agent", UserID: "bob"},
		{ID: "other-agent", AgentName: "team/other", UserID: "alice"},
	}}
	listener := bufconn.Listen(DefaultMaxMessageSize)
	server, err := New(Config{
		Listener:      listener,
		SystemService: testSystemService(),
		Authenticator: runtimeauth.Users(authimpl.NewProxyAuthenticator("sub"), "https://actors.example"),
		MemoryService: memoryservice.NewService(store),
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Start(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	conn, err := grpc.NewClient("passthrough:///memory", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	client := runtimev1.NewMemoryServiceClient(conn)
	list := &runtimev1.MemoryServiceListRequest{AgentName: "team/agent", UserId: "bob"}
	deleteRequest := &runtimev1.MemoryServiceDeleteRequest{AgentName: "team/agent", UserId: "bob"}

	for _, claims := range []string{"", `{"iss":"https://actors.example","sub":"alice"}`} {
		unauthenticated := t.Context()
		if claims != "" {
			unauthenticated = metadata.NewOutgoingContext(unauthenticated, metadata.Pairs("authorization", "Bearer e30."+base64.RawURLEncoding.EncodeToString([]byte(claims))+".signature"))
		}
		_, err = client.List(unauthenticated, list)
		require.Equal(t, codes.Unauthenticated, status.Code(err))
		_, err = client.Delete(unauthenticated, deleteRequest)
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	}
	// The configured proxy authenticates the bearer claims. Neither the request
	// user_id nor a conflicting forwarded user header selects another user's data.
	user := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization",
		"Bearer e30."+base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"alice"}`))+".signature", "x-user-id", "bob"))
	listed, err := client.List(user, list)
	require.NoError(t, err)
	require.Len(t, listed.Memories, 1)
	require.Equal(t, "own", listed.Memories[0].Id)
	_, err = client.Delete(user, deleteRequest)
	require.NoError(t, err)
	listed, err = client.List(user, list)
	require.NoError(t, err)
	require.Empty(t, listed.Memories)
	require.Len(t, store.memories, 2)
	require.Equal(t, "other-user", store.memories[0].ID)
	require.Equal(t, "other-agent", store.memories[1].ID)
}
