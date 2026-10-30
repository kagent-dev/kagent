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
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	runtimetaskstore "github.com/kagent-dev/kagent/go/adk/pkg/taskstore"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/taskstore"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

// A save made while the controller restarts must wait for it on the same
// client, replay the same request, and land once the controller serves again.
func TestRuntimeTaskStoreSaveOutlastsAControllerRestart(t *testing.T) {
	dsn := dbtest.StartT(context.WithoutCancel(t.Context()), t)
	dbtest.MigrateT(t, dsn, false)
	db, err := database.Connect(t.Context(), &database.PostgresConfig{Pool: dbtest.PoolConfig(dsn)})
	require.NoError(t, err)
	t.Cleanup(db.Close)
	store := database.NewClient(db, "public")
	session := createTaskStoreSession(t, store)
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile, substrate.RuntimeJWTAudience)
	require.NoError(t, err)
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

	var mu sync.Mutex
	var updates []proto.Message
	controller, err := controllerclient.New(controllerclient.Config{
		APIURL: "http://api.test", DialOptions: []grpc.DialOption{
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.Load().DialContext(ctx) }),
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
	runtime := runtimetaskstore.New(controller, identityPath, &configv1.TaskStoreConfig{
		OperationBudget: durationpb.New(30 * time.Second), AttemptTimeout: durationpb.New(time.Second),
	})

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
	// The previous client gave up after four attempts, in under a second.
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
	require.Greater(t, len(updates), 1, "the save must have failed while the controller was down")
	for _, request := range updates[1:] {
		require.True(t, proto.Equal(updates[0], request), "every attempt must replay the same save")
	}
}
