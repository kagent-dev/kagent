package grpcserver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"

	"github.com/kagent-dev/kagent/go/api/authorization"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	memorysvc "github.com/kagent-dev/kagent/go/core/internal/service/memory"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type memoryRPCPolicy struct{ denied atomic.Bool }

func (p *memoryRPCPolicy) Check(_ context.Context, principal auth.Principal, verb auth.Verb, resource auth.Resource) error {
	if p.denied.Load() || principal.ID == auth.PrincipalID("trusted-proxy:", "blocked") || resource.Type != "Memory" || resource.Namespace != "" {
		return errors.New("denied")
	}
	if principal.ID == auth.PrincipalID("trusted-proxy:", "viewer") && verb != auth.VerbGet && verb != auth.VerbList {
		return errors.New("read only")
	}
	return nil
}

type memoryRPCStore struct {
	mu      sync.Mutex
	rows    map[string][]database.Memory
	calls   int
	session *apiv1.Session
}

var _ memorysvc.Store = (*memoryRPCStore)(nil)

func (s *memoryRPCStore) StoreAgentMemories(_ context.Context, memories ...*database.Memory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	for _, memory := range memories {
		memory.ID = fmt.Sprintf("memory-%d", s.calls)
		key := memory.AgentName + "/" + memory.UserID
		s.rows[key] = append(s.rows[key], *memory)
	}
	return nil
}
func (s *memoryRPCStore) ListAgentMemories(_ context.Context, agent, user string) ([]database.Memory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return append([]database.Memory(nil), s.rows[agent+"/"+user]...), nil
}
func (s *memoryRPCStore) SearchAgentMemory(_ context.Context, agent, user string, _ pgvector.Vector, _ int) ([]database.AgentMemorySearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	var rows []database.AgentMemorySearchResult
	for _, memory := range s.rows[agent+"/"+user] {
		rows = append(rows, database.AgentMemorySearchResult{Memory: memory, Score: 1})
	}
	return rows, nil
}
func (s *memoryRPCStore) DeleteAgentMemory(_ context.Context, agent, user string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	delete(s.rows, agent+"/"+user)
	return nil
}
func (s *memoryRPCStore) GetSessionForRuntime(_ context.Context, id, uid string) (*apiv1.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil || s.session.Id != id || uid != "actor-uid" {
		return nil, database.ErrNotFound
	}
	return s.session, nil
}
func (s *memoryRPCStore) callCount() int { s.mu.Lock(); defer s.mu.Unlock(); return s.calls }
func memoryVector() []float32            { v := make([]float32, memorysvc.VectorDimension); v[0] = 1; return v }

func memoryUserContext(t *testing.T, user string) context.Context {
	t.Helper()
	// The trusted proxy authenticates these claims before forwarding to Kagent.
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	token := "Bearer " + header + "." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"sub":%q}`, user))) + "." + base64.RawURLEncoding.EncodeToString([]byte("signature"))
	return metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", token, "x-user-id", "forged-header"))
}

func TestHumanMemoryPermissionsAndPartitionsThroughGeneratedClient(t *testing.T) {
	store := &memoryRPCStore{rows: map[string][]database.Memory{
		"agent/" + auth.PrincipalID("trusted-proxy:", "alice"): {{ID: "alice-memory", Content: "private"}},
		"agent/" + auth.PrincipalID("trusted-proxy:", "bob"):   {{ID: "bob-memory", Content: "bob private"}},
		"other/" + auth.PrincipalID("trusted-proxy:", "alice"): {{ID: "other-agent", Content: "other Agent"}},
	}}
	policy := &memoryRPCPolicy{}
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile)
	require.NoError(t, err)
	connection := newConfigurationConnection(t, nil, func(config *Config) {
		config.Authenticator = runtimeauth.NewProvider(authimpl.NewProxyAuthenticator("sub"), verifier, store, issuer.URL)
		config.MemoryService = memorysvc.NewService(store, runtimeauth.NewAuthorizer(policy, store))
		config.ShareStore = &testShareStore{sessionShare: &apiv1.SessionShare{SessionId: testSessionID.String()}, ownerUserID: "alice"}
	})
	client := runtimev1.NewMemoryServiceClient(connection)
	caller := func(user string) context.Context {
		return memoryUserContext(t, user)
	}
	// Authenticated users select only their own partition, including with no live Session.
	rows, err := client.List(caller("alice"), &runtimev1.MemoryServiceListRequest{AgentName: "agent"})
	require.NoError(t, err)
	require.Len(t, rows.Memories, 1)
	require.Equal(t, "alice-memory", rows.Memories[0].Id)
	operations := []struct {
		name string
		call func(context.Context) error
	}{
		{"list", func(ctx context.Context) error {
			_, err := client.List(ctx, &runtimev1.MemoryServiceListRequest{AgentName: "agent"})
			return err
		}},
		{"delete", func(ctx context.Context) error {
			_, err := client.Delete(ctx, &runtimev1.MemoryServiceDeleteRequest{AgentName: "agent"})
			return err
		}},
	}
	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			before := store.callCount()
			assertGRPCCode(t, op.call(caller("blocked")), codes.PermissionDenied)
			assertGRPCCode(t, op.call(metadata.AppendToOutgoingContext(caller("alice"), "x-share-token", "share")), codes.PermissionDenied)
			require.Equal(t, before, store.callCount())
		})
	}
	_, err = client.List(caller("viewer"), &runtimev1.MemoryServiceListRequest{AgentName: "agent"})
	require.NoError(t, err)
	assertGRPCCode(t, operations[1].call(caller("viewer")), codes.PermissionDenied)
	before := store.callCount()
	_, err = client.AddSession(caller("alice"), &runtimev1.MemoryServiceAddSessionRequest{Memory: &runtimev1.SessionMemoryInput{Content: "single", Vector: memoryVector()}})
	assertGRPCCode(t, err, codes.PermissionDenied)
	_, err = client.AddSessionBatch(caller("alice"), &runtimev1.MemoryServiceAddSessionBatchRequest{Items: []*runtimev1.SessionMemoryInput{{Vector: memoryVector()}}})
	assertGRPCCode(t, err, codes.PermissionDenied)
	_, err = client.Search(caller("alice"), &runtimev1.MemoryServiceSearchRequest{Vector: memoryVector()})
	assertGRPCCode(t, err, codes.PermissionDenied)
	require.Equal(t, before, store.callCount())
	require.NoError(t, operations[1].call(caller("alice")))
	rows, err = client.List(caller("bob"), &runtimev1.MemoryServiceListRequest{AgentName: "agent"})
	require.NoError(t, err)
	require.Len(t, rows.Memories, 1)
	require.Equal(t, "bob-memory", rows.Memories[0].Id)
	rows, err = client.List(caller("alice"), &runtimev1.MemoryServiceListRequest{AgentName: "agent"})
	require.NoError(t, err)
	require.Empty(t, rows.Memories)
	rows, err = client.List(caller("alice"), &runtimev1.MemoryServiceListRequest{AgentName: "other"})
	require.NoError(t, err)
	require.Len(t, rows.Memories, 1, "deleting one partition must preserve other Agents")
	require.Equal(t, "other-agent", rows.Memories[0].Id)
}

func TestRuntimeMemoryUsesAcceptedActorBindingInsteadOfHumanClaims(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	store := &memoryRPCStore{rows: map[string][]database.Memory{
		"team/assistant/victim": {{ID: "other-user"}},
		"forged/agent/owner":    {{ID: "other-agent"}},
	}, session: &apiv1.Session{Id: id, Creator: "owner", Agent: &apiv1.ResourceReference{Namespace: "team", Name: "assistant"}, A2AAuthority: substrate.ActorHost("team", substrate.ActorName(id), "")}}
	policy := &memoryRPCPolicy{}
	policy.denied.Store(true) // Accepted runtime authority survives later human grant removal.
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile)
	require.NoError(t, err)
	connection := newConfigurationConnection(t, nil, func(config *Config) {
		config.Authenticator = runtimeauth.NewProvider(authimpl.NewProxyAuthenticator("sub"), verifier, store, issuer.URL)
		config.MemoryService = memorysvc.NewService(store, runtimeauth.NewAuthorizer(policy, store))
	})
	client := runtimev1.NewMemoryServiceClient(connection)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	for _, credential := range []string{"", "Bearer " + header + "." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"iss":%q,"sub":"owner"}`, issuer.URL))) + "." + base64.RawURLEncoding.EncodeToString([]byte("signature"))} {
		ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", credential))
		_, err = client.List(ctx, &runtimev1.MemoryServiceListRequest{AgentName: "team/assistant"})
		assertGRPCCode(t, err, codes.Unauthenticated)
		_, err = client.Delete(ctx, &runtimev1.MemoryServiceDeleteRequest{AgentName: "team/assistant"})
		assertGRPCCode(t, err, codes.Unauthenticated)
	}
	require.Zero(t, store.callCount())
	actor := func(identity string) context.Context {
		parts := strings.Split(identity, "/")
		token := issuer.Token(t, issuer.Claims(parts[0], parts[1], parts[2]))
		return metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", "Bearer "+token, "x-user-id", "forged", "x-agent-name", "other"))
	}
	valid := "team/session-" + id + "/actor-uid"
	ctx := actor(valid)
	_, err = apiv1.NewSystemServiceClient(connection).GetVersion(ctx, &apiv1.GetVersionRequest{})
	require.NoError(t, err)
	_, err = client.AddSession(ctx, &runtimev1.MemoryServiceAddSessionRequest{Memory: &runtimev1.SessionMemoryInput{Content: "accepted", Vector: memoryVector()}})
	require.NoError(t, err)
	_, err = client.AddSessionBatch(ctx, &runtimev1.MemoryServiceAddSessionBatchRequest{Items: []*runtimev1.SessionMemoryInput{{Content: "batch", Vector: memoryVector()}}})
	require.NoError(t, err)
	found, err := client.Search(ctx, &runtimev1.MemoryServiceSearchRequest{Vector: memoryVector()})
	require.NoError(t, err)
	require.Len(t, found.Memories, 2)
	require.Equal(t, "accepted", found.Memories[0].Content)

	listed, err := client.List(ctx, &runtimev1.MemoryServiceListRequest{AgentName: "forged/agent"})
	require.NoError(t, err)
	require.Len(t, listed.Memories, 2)
	require.Contains(t, store.rows, "team/assistant/owner")
	_, err = client.Delete(ctx, &runtimev1.MemoryServiceDeleteRequest{AgentName: "forged/agent"})
	require.NoError(t, err)
	listed, err = client.List(ctx, &runtimev1.MemoryServiceListRequest{})
	require.NoError(t, err)
	require.Empty(t, listed.Memories)
	require.Len(t, store.rows["team/assistant/victim"], 1, "actor deletion must preserve other users")
	require.Len(t, store.rows["forged/agent/owner"], 1, "actor deletion must preserve other Agents")
	before := store.callCount()
	_, err = client.AddSessionBatch(ctx, &runtimev1.MemoryServiceAddSessionBatchRequest{Items: []*runtimev1.SessionMemoryInput{{Content: "valid", Vector: memoryVector()}, {Content: "invalid", Vector: []float32{1}}}})
	assertGRPCCode(t, err, codes.InvalidArgument)
	_, err = client.AddSession(ctx, &runtimev1.MemoryServiceAddSessionRequest{})
	assertGRPCCode(t, err, codes.InvalidArgument)
	require.Equal(t, before, store.callCount())
	for _, identity := range []string{"other/session-" + id + "/actor-uid", "team/session-" + id + "/stale-uid", "team/session-22222222-2222-4222-8222-222222222222/actor-uid"} {
		_, err = client.Search(actor(identity), &runtimev1.MemoryServiceSearchRequest{Vector: memoryVector()})
		assertGRPCCode(t, err, codes.Unauthenticated)
		_, err = client.List(actor(identity), &runtimev1.MemoryServiceListRequest{AgentName: "team/assistant"})
		assertGRPCCode(t, err, codes.Unauthenticated)
		_, err = client.Delete(actor(identity), &runtimev1.MemoryServiceDeleteRequest{AgentName: "team/assistant"})
		assertGRPCCode(t, err, codes.Unauthenticated)
	}
	_, err = client.Search(metadata.AppendToOutgoingContext(ctx, "x-share-token", "share"), &runtimev1.MemoryServiceSearchRequest{Vector: memoryVector()})
	assertGRPCCode(t, err, codes.Unauthenticated)
	_, err = client.List(metadata.AppendToOutgoingContext(ctx, "x-share-token", "share"), &runtimev1.MemoryServiceListRequest{AgentName: "team/assistant"})
	assertGRPCCode(t, err, codes.Unauthenticated)
	_, err = client.Search(memoryUserContext(t, "owner"), &runtimev1.MemoryServiceSearchRequest{Vector: memoryVector()})
	assertGRPCCode(t, err, codes.PermissionDenied)
	require.Equal(t, before, store.callCount())
	store.mu.Lock()
	store.session = nil
	store.mu.Unlock()
	_, err = client.Search(ctx, &runtimev1.MemoryServiceSearchRequest{Vector: memoryVector()})
	assertGRPCCode(t, err, codes.Unauthenticated)
	require.Equal(t, before, store.callCount())
}

func (*memoryRPCPolicy) Scope(context.Context, auth.Principal, auth.Verb, string) (authorization.AuthorizationScope, error) {
	return authorization.AuthorizationScope{Kind: authorization.ScopeNone}, nil
}
