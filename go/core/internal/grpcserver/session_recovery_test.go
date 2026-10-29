package grpcserver

import (
	"context"
	"crypto/sha256"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type revertTestControl struct {
	ateapipb.ControlClient
	mu     sync.Mutex
	actors map[string]*ateapipb.Actor
	revert func(*ateapipb.Actor) error
}

func (c *revertTestControl) GetActor(_ context.Context, request *ateapipb.GetActorRequest, _ ...grpc.CallOption) (*ateapipb.Actor, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	actor := c.actors[request.GetActor().GetName()]
	if actor == nil {
		return nil, status.Error(codes.NotFound, "actor not found")
	}
	return proto.CloneOf(actor), nil
}

func (c *revertTestControl) RevertActor(_ context.Context, request *ateapipb.RevertActorRequest, _ ...grpc.CallOption) (*ateapipb.RevertActorResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	actor := c.actors[request.GetActor().GetName()]
	if c.revert != nil {
		if err := c.revert(actor); err != nil {
			return nil, err
		}
	}
	actor.Status.State = ateapipb.ActorState_ACTOR_STATE_SUSPENDED
	return &ateapipb.RevertActorResponse{Actor: proto.CloneOf(actor)}, nil
}

func TestRevertSessionThroughGRPC(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	dsn := dbtest.StartT(context.WithoutCancel(t.Context()), t)
	dbtest.MigrateT(t, dsn, false)
	db, err := database.Connect(t.Context(), &database.PostgresConfig{Pool: dbtest.PoolConfig(dsn)})
	require.NoError(t, err)
	t.Cleanup(db.Close)
	store := database.NewClient(db, "public")
	control := &revertTestControl{actors: map[string]*ateapipb.Actor{}}
	actors := &substrate.Client{ControlClient: control}
	workflow := sessionsvc.NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute)
	listener := bufconn.Listen(DefaultMaxMessageSize)
	server, err := New(Config{
		Listener: listener, Authenticator: &authimpl.InsecureAuthenticator{},
		SystemService: testSystemService(), ShareStore: store,
		SessionService: sessionsvc.NewService(store, &auth.NoopAuthorizer{}, workflow),
	})
	require.NoError(t, err)
	ctx, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Start(ctx) }()
	t.Cleanup(func() { stop(); require.NoError(t, <-done) })
	connection, err := grpc.NewClient("passthrough:///bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	client := apiv1.NewSessionServiceClient(connection)
	owner := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "alice"))
	visitor := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "bob"))
	_, err = client.RevertSession(owner, &apiv1.RevertSessionRequest{SessionId: "invalid"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	for _, test := range []struct {
		name         string
		task         a2a.TaskState
		noSnapshot   bool
		lostResponse bool
	}{
		{name: "interrupted turn", task: a2a.TaskStateWorking},
		{name: "completed turn", task: a2a.TaskStateCompleted},
		{name: "idle session"},
		{name: "published input wait", task: a2a.TaskStateInputRequired},
		{name: "published auth wait", task: a2a.TaskStateAuthRequired},
		{name: "first-turn crash", task: a2a.TaskStateWorking, noSnapshot: true},
		{name: "revert response lost", task: a2a.TaskStateWorking, lostResponse: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := createTaskStoreSession(t, store)
			name := substrate.ActorName(session.Id)
			control.actors[name] = &ateapipb.Actor{
				Metadata:      &ateapipb.ResourceMetadata{Atespace: "team-a", Name: name, Uid: "actor-uid"},
				ActorTemplate: &ateapipb.ObjectRef{Atespace: "team-a", Name: "assistant-kagent-revision"},
				Status: &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_RUNNING,
					Snapshots: []*ateapipb.Snapshot{{Generation: 1, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, DurableSnapshot: &ateapipb.SnapshotStorage{Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/last-good"}}}}},
			}
			var task *a2a.Task
			if test.task != "" {
				message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("original input"))
				message.ContextID = session.ContextId
				task = a2a.NewSubmittedTask(message, message)
				task.Status.State = test.task
				hash := sha256.Sum256([]byte(task.ID))
				version, err := store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, "")
				require.NoError(t, err)
				if test.task == a2a.TaskStateInputRequired || test.task == a2a.TaskStateAuthRequired {
					require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
				}
			}
			observeRecovery := func(needed bool) *apiv1.Session {
				t.Helper()
				response, err := client.GetSession(owner, &apiv1.GetSessionRequest{SessionId: session.Id})
				require.NoError(t, err)
				require.Equal(t, new(needed), response.Session.GetRecovery().Needed)
				return response.Session
			}
			require.Empty(t, observeRecovery(false).GetRecovery().FailedMessages)
			request := &apiv1.RevertSessionRequest{SessionId: session.Id}
			if test.noSnapshot {
				control.actors[name].Status.Snapshots = nil
			}
			share, err := client.CreateSessionShare(owner, &apiv1.CreateSessionShareRequest{
				SessionId: session.Id,
			})
			require.NoError(t, err)
			readOnly := metadata.AppendToOutgoingContext(visitor, "x-share-token", share.Token)
			_, err = client.RevertSession(readOnly, request)
			require.Equal(t, codes.PermissionDenied, status.Code(err))
			_, err = client.RevertSession(visitor, request)
			require.Equal(t, codes.NotFound, status.Code(err))
			_, err = client.RevertSession(owner, request)
			require.Equal(t, codes.FailedPrecondition, status.Code(err), "healthy runtimes cannot revert")
			control.actors[name].Status.State = ateapipb.ActorState_ACTOR_STATE_CRASHED
			crashed := observeRecovery(true)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, crashed.State)
			require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE, crashed.Operation)
			require.Nil(t, crashed.Failure)
			require.Empty(t, crashed.GetRecovery().FailedMessages)
			before := proto.CloneOf(control.actors[name])
			control.actors[name].Metadata.Uid = "replacement-uid"
			_, err = client.RevertSession(owner, request)
			require.Equal(t, codes.Unavailable, status.Code(err), "a same-name replacement is not this runtime")
			control.actors[name].Metadata.Uid = before.Metadata.Uid
			delete(control.actors, name)
			_, err = client.RevertSession(owner, request)
			require.Equal(t, codes.Unavailable, status.Code(err), "a missing runtime cannot authorize recovery")
			control.actors[name] = proto.CloneOf(before)
			calls := 0
			failOnce := true
			control.revert = func(actor *ateapipb.Actor) error {
				calls++
				if failOnce {
					failOnce = false
					if test.lostResponse {
						actor.Status.State = ateapipb.ActorState_ACTOR_STATE_SUSPENDED
					}
					return status.Error(codes.Unavailable, "temporary revert outage")
				}
				return nil
			}
			_, err = client.RevertSession(owner, request)
			require.Equal(t, codes.Unavailable, status.Code(err))
			failed, err := client.GetSession(owner, &apiv1.GetSessionRequest{SessionId: session.Id})
			require.NoError(t, err)
			require.Equal(t, database.SessionRuntimeCrashed, failed.Session.Failure.Reason)
			require.Equal(t, new(true), failed.Session.GetRecovery().Needed)
			var wantFailed []*apiv1.SessionMessageReference
			if task != nil {
				wantFailed = []*apiv1.SessionMessageReference{{TaskId: string(task.ID), MessageId: task.History[0].ID}}
			}
			require.Equal(t, wantFailed, failed.Session.GetRecovery().FailedMessages)
			ready, err := client.RevertSession(owner, request)
			require.NoError(t, err)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, ready.Session.State)
			require.Nil(t, ready.Session.Failure)
			require.Equal(t, wantFailed, observeRecovery(false).GetRecovery().FailedMessages)
			if test.lostResponse {
				require.Equal(t, 1, calls, "a lost response cannot repeat the revert")
			} else {
				require.Equal(t, 2, calls)
			}
			after := control.actors[name]
			require.Equal(t, before.Metadata.Uid, after.Metadata.Uid)
			require.True(t, proto.Equal(substrate.LatestDurableActorSnapshot(before), substrate.LatestDurableActorSnapshot(after)))
			require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, after.Status.State)
			if task != nil {
				stored, err := store.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
				require.NoError(t, err)
				want := test.task
				if want == a2a.TaskStateWorking || want == a2a.TaskStateInputRequired || want == a2a.TaskStateAuthRequired {
					want = a2a.TaskStateFailed
				}
				require.Equal(t, want, stored.Status.State)
			}
			require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), uuid.NewString()))
		})
	}
}
