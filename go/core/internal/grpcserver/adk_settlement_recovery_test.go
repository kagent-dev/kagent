package grpcserver

import (
	"context"
	"errors"
	"iter"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	adka2a "github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	runtimetaskstore "github.com/kagent-dev/kagent/go/adk/pkg/taskstore"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/taskstore"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

type unavailableSettlementStore struct {
	*database.Client
	fail     atomic.Bool
	attempts atomic.Int32
}

func (s *unavailableSettlementStore) SettleSessionTask(ctx context.Context, sessionID, taskID string, version int64) error {
	s.attempts.Add(1)
	if s.fail.Load() {
		return status.Error(codes.Unavailable, "injected settlement outage")
	}
	return s.Client.SettleSessionTask(ctx, sessionID, taskID, version)
}

type settlementProbeModel struct{ calls atomic.Int32 }

var _ model.LLM = (*settlementProbeModel)(nil)
var _ taskstore.Store = (*unavailableSettlementStore)(nil)

type cleanupObservedExecutor struct {
	a2asrv.AgentExecutor
	a2asrv.AgentExecutionCleaner
	cleaned chan struct{}
}

var _ a2asrv.AgentExecutor = (*cleanupObservedExecutor)(nil)
var _ a2asrv.AgentExecutionCleaner = (*cleanupObservedExecutor)(nil)

func (e *cleanupObservedExecutor) Cleanup(ctx context.Context, input *a2asrv.ExecutorContext, result a2a.SendMessageResult, err error) {
	e.AgentExecutionCleaner.Cleanup(ctx, input, result, err)
	close(e.cleaned)
}

func (*settlementProbeModel) Name() string { return "settlement-probe" }

func (m *settlementProbeModel) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		m.calls.Add(1)
		yield(&model.LLMResponse{Content: genai.NewContentFromText("finished", genai.RoleModel)}, nil)
	}
}

// Exercise the production Go ADK executor, TaskStore client, gRPC service and
// PostgreSQL together, with an in-memory ADK session and a fixed model response.
// https://github.com/kagent-dev/kagent/issues/3010 case B tracks this failure.
// This verifies DELETE admission, not Actor deletion or same-session recovery.
func TestGoADKDeletionAfterSettlementOutage(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "healthy"
		if fail {
			name = "settlement_outage"
		}
		t.Run(name, func(t *testing.T) {
			dsn := dbtest.StartT(context.WithoutCancel(t.Context()), t)
			dbtest.MigrateT(t, dsn, false)
			db, err := database.Connect(t.Context(), &database.PostgresConfig{Pool: dbtest.PoolConfig(dsn)})
			require.NoError(t, err)
			t.Cleanup(db.Close)
			store := &unavailableSettlementStore{Client: database.NewClient(db, "public")}
			store.fail.Store(fail)
			session := createTaskStoreSession(t, store.Client)
			issuer := jwttest.New(t)
			verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile, substrate.RuntimeJWTAudience)
			require.NoError(t, err)
			runtimeToken := "Bearer " + issuer.Token(t, issuer.Claims("team-a", substrate.ActorName(session.Id), "actor-uid"))
			listener := bufconn.Listen(DefaultMaxMessageSize)
			server, err := New(Config{
				Listener: listener, SystemService: testSystemService(),
				Authenticator:    runtimeauth.NewProvider(&authimpl.InsecureAuthenticator{}, verifier, store, issuer.URL),
				TaskStoreService: taskstore.NewService(store, nil, nil),
			})
			require.NoError(t, err)
			serverCtx, stop := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- server.Start(serverCtx) }()
			t.Cleanup(func() { stop(); require.NoError(t, <-done) })
			controller, err := controllerclient.New(controllerclient.Config{
				APIURL: "http://api.test", DialOptions: []grpc.DialOption{
					grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
					// Model the egress gateway replacing the runtime placeholder
					// before the controller verifies the signed Actor JWT.
					grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
						md, _ := metadata.FromOutgoingContext(ctx)
						md = md.Copy()
						md.Set("authorization", runtimeToken)
						return invoke(metadata.NewOutgoingContext(ctx, md), method, req, reply, cc, opts...)
					}),
				},
			})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, controller.Close()) })
			identityDir := t.TempDir()
			for file, value := range map[string]string{"name": "session-" + session.Id, "atespace": "team-a", "uid": "actor-uid"} {
				require.NoError(t, os.WriteFile(filepath.Join(identityDir, file), []byte(value), 0o600))
			}
			runtimeStore := runtimetaskstore.New(controller, filepath.Join(identityDir, "name"))
			llm := &settlementProbeModel{}
			agent, err := llmagent.New(llmagent.Config{Name: "probe", Model: llm})
			require.NoError(t, err)
			native, err := adka2a.NewKAgentExecutor(adka2a.KAgentExecutorConfig{
				RunnerConfig: runner.Config{AppName: "probe", Agent: agent, SessionService: adksession.InMemoryService()},
				AppName:      "probe", Logger: slog.New(slog.DiscardHandler),
			})
			require.NoError(t, err)
			wrapped := runtimeStore.WrapExecutor(native, "", nil)
			observed := &cleanupObservedExecutor{
				AgentExecutor: wrapped, AgentExecutionCleaner: wrapped, cleaned: make(chan struct{}),
			}
			handler := a2asrv.NewHandler(observed, a2asrv.WithTaskStore(runtimeStore),
				a2asrv.WithCallInterceptors(adka2a.HITLActivationInterceptor(), adka2a.UserIDCallInterceptor(), wrapped))
			runtimeListener := bufconn.Listen(DefaultMaxMessageSize)
			runtimeServer := grpc.NewServer()
			a2agrpc.NewHandler(handler).RegisterWith(runtimeServer)
			runtimeDone := make(chan error, 1)
			go func() { runtimeDone <- runtimeServer.Serve(runtimeListener) }()
			t.Cleanup(func() {
				runtimeServer.Stop()
				if err := <-runtimeDone; !errors.Is(err, grpc.ErrServerStopped) {
					require.NoError(t, err, "runtime A2A server failed")
				}
			})
			conn, err := grpc.NewClient("passthrough:///runtime", grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return runtimeListener.Dial() }))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, conn.Close()) })
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			response, err := a2apb.NewA2AServiceClient(conn).SendMessage(ctx, &a2apb.SendMessageRequest{
				Message: &a2apb.Message{MessageId: uuid.NewString(), ContextId: session.ContextId, Role: a2apb.Role_ROLE_USER,
					Parts: []*a2apb.Part{{Content: &a2apb.Part_Text{Text: "finish one turn"}}}},
			})
			require.NoError(t, err)
			select {
			case <-observed.cleaned:
			case <-ctx.Done():
				t.Fatal("ADK cleanup did not finish", ctx.Err())
			}
			require.EqualValues(t, 1, llm.calls.Load())
			taskID := response.GetTask().GetId()
			require.NotEmpty(t, taskID)
			stored, version, err := store.GetVersionedSessionTask(t.Context(), session.Id, taskID)
			require.NoError(t, err)
			require.Equal(t, a2a.TaskStateCompleted, stored.Status.State)
			public, err := store.GetSessionTask(t.Context(), session.Id, taskID, nil)
			require.NoError(t, err)
			if fail {
				require.EqualValues(t, 4, store.attempts.Load())
				require.Equal(t, a2a.TaskStateWorking, public.Status.State)
				// Restoring availability after Cleanup cannot retry the boundary.
				store.fail.Store(false)
				_, err = store.GetSettledSessionTask(t.Context(), session.Id, taskID, nil)
				require.ErrorIs(t, err, database.ErrConflict)
				require.ErrorIs(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), ""), database.ErrConflict)
				_, err = store.ClaimSessionQuiescence(t.Context())
				require.ErrorIs(t, err, database.ErrNotFound, "idle worker cannot recover an unpublished boundary")
			} else {
				require.EqualValues(t, 1, store.attempts.Load())
				require.Equal(t, a2a.TaskStateCompleted, public.Status.State)
			}
			t.Logf("Go ADK calls=%d; SettleTask attempts=%d; stored=%s; public=%s; cleanup finished", llm.calls.Load(), store.attempts.Load(), stored.Status.State, public.Status.State)
			_, err = store.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
			t.Logf("DELETE admission: %v", err)
			require.NoError(t, err, "explicit deletion must remain available after settlement retries are exhausted")
			privateCtx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", runtimeToken))
			_, err = controller.TaskStoreService().SettleTask(privateCtx, &runtimev1.TaskStoreServiceSettleTaskRequest{SessionId: session.Id, TaskId: taskID, Version: version})
			if fail {
				require.Equal(t, codes.Aborted, status.Code(err), "late cleanup cannot publish after deletion admission")
			} else {
				require.NoError(t, err, "an already published acknowledgement remains idempotent")
			}
		})
	}
}
