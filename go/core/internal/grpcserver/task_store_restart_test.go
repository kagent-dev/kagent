package grpcserver

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	sdktaskstore "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	runtimetaskstore "github.com/kagent-dev/kagent/go/adk/pkg/taskstore"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/taskstore"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

// A save made while the controller restarts must keep trying on the same
// client, replay the same request, and land once the controller serves again.
func TestRuntimeTaskStoreSaveOutlastsAControllerRestart(t *testing.T) {
	dsn := dbtest.StartT(context.WithoutCancel(t.Context()), t)
	dbtest.MigrateT(t, dsn, false)
	db, err := database.Connect(t.Context(), &database.PostgresConfig{Pool: dbtest.PoolConfig(dsn)})
	require.NoError(t, err)
	t.Cleanup(db.Close)
	store := database.NewClient(db, "public")
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile, substrate.RuntimeJWTAudience)
	require.NoError(t, err)

	// A runtime dialing the controller itself loses its connection, so
	// WaitForReady holds each call until the controller listens again.
	t.Run("direct", func(t *testing.T) {
		saveThroughControllerRestart(t, store, issuer, verifier, false)
	})
	// A deployed runtime reaches the controller through the egress proxy. Its
	// connection to the proxy stays READY, and every attempt fails at once with
	// Unavailable, so only the retry budget can wait for the controller.
	t.Run("through a reachable proxy", func(t *testing.T) {
		saveThroughControllerRestart(t, store, issuer, verifier, true)
	})
}

func saveThroughControllerRestart(t *testing.T, store *database.Client, issuer *jwttest.Issuer, verifier *substrate.ActorJWTVerifier, viaProxy bool) {
	session := createTaskStoreSession(t, store)
	runtimeToken := "Bearer " + issuer.Token(t, issuer.Claims("team-a", substrate.ActorName(session.Id), "actor-uid"))

	// Stopping closes the listener, so every dial is refused until the next start.
	var listener atomic.Pointer[bufconn.Listener]
	startController := func() (stop func()) {
		serving := bufconn.Listen(DefaultMaxMessageSize)
		server, err := New(Config{
			Listener: serving, SystemService: testSystemService(),
			Authenticator:    runtimeauth.NewProvider(&authimpl.InsecureAuthenticator{}, verifier, store, issuer.URL),
			TaskStoreService: taskstore.NewService(store, nil, make(chan struct{}, 1)),
		})
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- server.Start(ctx) }()
		listener.Store(serving)
		return sync.OnceFunc(func() {
			cancel()
			require.NoError(t, <-done)
		})
	}
	stopController := startController()
	t.Cleanup(func() { stopController() })
	dial := func(ctx context.Context, _ string) (net.Conn, error) { return listener.Load().DialContext(ctx) }
	if viaProxy {
		dial = startTaskStoreProxy(t, dial)
	}

	var mu sync.Mutex
	var updates []proto.Message
	controller, err := controllerclient.New(controllerclient.Config{
		APIURL: "http://api.test", DialOptions: []grpc.DialOption{
			grpc.WithContextDialer(dial),
			grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
				if method == runtimev1.TaskStoreService_UpdateTask_FullMethodName {
					mu.Lock()
					updates = append(updates, proto.Clone(req.(proto.Message)))
					mu.Unlock()
				}
				// Stands in for the egress proxy, which swaps the placeholder for
				// the actor's token.
				md, _ := metadata.FromOutgoingContext(ctx)
				md = md.Copy()
				md.Set("authorization", runtimeToken)
				return invoke(metadata.NewOutgoingContext(ctx, md), method, req, reply, cc, opts...)
			}),
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, controller.Close()) })
	identityPath := filepath.Join(t.TempDir(), "name")
	require.NoError(t, os.WriteFile(identityPath, []byte("session-"+session.Id), 0o600))
	runtime := runtimetaskstore.New(controller, identityPath)

	task := &a2a.Task{ID: a2a.TaskID(uuid.NewString()), ContextID: session.ContextId, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}
	created, err := runtime.Create(t.Context(), task)
	require.NoError(t, err)

	stopController()
	// The controller requires the saved task to carry the event's status.
	event := a2a.NewStatusUpdateEvent(task, a2a.TaskStateCompleted, nil)
	completed := *task
	completed.Status = event.Status
	type result struct {
		version sdktaskstore.TaskVersion
		err     error
	}
	saved := make(chan result, 1)
	go func() {
		version, err := runtime.Update(t.Context(), &sdktaskstore.UpdateRequest{
			Task: &completed, PrevVersion: created, Event: event,
		})
		saved <- result{version, err}
	}()
	// Four attempts, the client's former limit, end in under a second.
	require.Never(t, func() bool { return len(saved) > 0 }, 2*time.Second, 20*time.Millisecond,
		"the save must stay pending while the controller is down")
	stopController = startController()

	var got result
	select {
	case got = <-saved:
	case <-time.After(20 * time.Second):
		t.Fatal("the save did not land after the controller returned")
	}
	require.NoError(t, got.err)
	require.Greater(t, got.version, created)
	stored, version, err := store.GetVersionedSessionTask(t.Context(), session.Id, string(task.ID))
	require.NoError(t, err)
	require.EqualValues(t, got.version, version, "the runtime advances to the version the controller committed")
	require.Equal(t, a2a.TaskStateCompleted, stored.Status.State)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, updates, "the save must reach the restarted controller")
	for _, request := range updates[1:] {
		require.True(t, proto.Equal(updates[0], request), "every attempt must replay the same save")
	}
}

// startTaskStoreProxy stands in for the egress proxy. It accepts the runtime's
// connection whatever the controller's state and forwards each TaskStore call.
// While the controller is down, the forward fails and the runtime receives
// Unavailable over its healthy connection, as it does from AgentGateway.
func startTaskStoreProxy(t *testing.T, dialController func(context.Context, string) (net.Conn, error)) func(context.Context, string) (net.Conn, error) {
	t.Helper()
	upstream, err := grpc.NewClient("passthrough:///controller",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialController),
		// The proxy connects to the controller for each request, so it reaches
		// a returning controller at once rather than after a reconnect backoff.
		grpc.WithConnectParams(grpc.ConnectParams{Backoff: backoff.Config{BaseDelay: 10 * time.Millisecond, Multiplier: 1, MaxDelay: 10 * time.Millisecond}, MinConnectTimeout: time.Second}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, upstream.Close()) })
	serving := bufconn.Listen(DefaultMaxMessageSize)
	server := grpc.NewServer()
	runtimev1.RegisterTaskStoreServiceServer(server, &taskStoreProxy{upstream: runtimev1.NewTaskStoreServiceClient(upstream)})
	go func() { _ = server.Serve(serving) }()
	t.Cleanup(server.Stop)
	return func(ctx context.Context, _ string) (net.Conn, error) { return serving.DialContext(ctx) }
}

type taskStoreProxy struct {
	runtimev1.UnimplementedTaskStoreServiceServer
	upstream runtimev1.TaskStoreServiceClient
}

// forwardAuthorization passes on the actor's token, the only metadata the
// controller authenticates.
func forwardAuthorization(ctx context.Context) context.Context {
	md, _ := metadata.FromIncomingContext(ctx)
	return metadata.NewOutgoingContext(ctx, metadata.MD{"authorization": md.Get("authorization")})
}

func (p *taskStoreProxy) CreateTask(ctx context.Context, request *runtimev1.TaskStoreServiceCreateTaskRequest) (*runtimev1.TaskStoreServiceCreateTaskResponse, error) {
	return p.upstream.CreateTask(forwardAuthorization(ctx), request)
}

func (p *taskStoreProxy) UpdateTask(ctx context.Context, request *runtimev1.TaskStoreServiceUpdateTaskRequest) (*runtimev1.TaskStoreServiceUpdateTaskResponse, error) {
	return p.upstream.UpdateTask(forwardAuthorization(ctx), request)
}

func (p *taskStoreProxy) GetTask(ctx context.Context, request *runtimev1.TaskStoreServiceGetTaskRequest) (*runtimev1.TaskStoreServiceGetTaskResponse, error) {
	return p.upstream.GetTask(forwardAuthorization(ctx), request)
}

func (p *taskStoreProxy) ListTasks(ctx context.Context, request *runtimev1.TaskStoreServiceListTasksRequest) (*runtimev1.TaskStoreServiceListTasksResponse, error) {
	return p.upstream.ListTasks(forwardAuthorization(ctx), request)
}

func (p *taskStoreProxy) SettleTask(ctx context.Context, request *runtimev1.TaskStoreServiceSettleTaskRequest) (*runtimev1.TaskStoreServiceSettleTaskResponse, error) {
	return p.upstream.SettleTask(forwardAuthorization(ctx), request)
}
