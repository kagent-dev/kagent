package grpcserver

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"iter"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"google.golang.org/protobuf/proto"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/limiter"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	runtimetaskstore "github.com/kagent-dev/kagent/go/adk/pkg/taskstore"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/a2agateway"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/taskstore"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type lostRuntimeSaveResponse struct {
	*database.Client
	lost          atomic.Bool
	lostCreate    atomic.Bool
	delayedCreate chan string
	releaseCreate chan struct{}
}

func (s *lostRuntimeSaveResponse) UpdateAgentInstanceTask(ctx context.Context, id string, version int64, hash []byte, task *a2a.Task, event a2a.Event) (int64, error) {
	next, err := s.Client.UpdateAgentInstanceTask(ctx, id, version, hash, task, event)
	if err == nil && s.lost.CompareAndSwap(false, true) {
		return 0, status.Error(codes.Unavailable, "lost response after committing runtime save")
	}
	return next, err
}

func (s *lostRuntimeSaveResponse) CreateRuntimeTask(ctx context.Context, id string, hash []byte, task *a2a.Task) (int64, error) {
	if len(task.History) > 0 && task.History[0].Parts[0].Text() == "reject-create" {
		return 0, status.Error(codes.FailedPrecondition, "injected initial-save failure")
	}
	version, err := s.Client.CreateRuntimeTask(ctx, id, hash, task)
	if err == nil && len(task.History) > 0 && task.History[0].Parts[0].Text() == "late" {
		s.delayedCreate <- string(task.ID)
		<-s.releaseCreate
	}
	if err == nil && s.lostCreate.CompareAndSwap(false, true) {
		return 0, status.Error(codes.Unavailable, "create response lost after commit")
	}
	return version, err
}

type taskStoreRuntimeDialer struct{ listener *bufconn.Listener }

func (d taskStoreRuntimeDialer) Dial(ctx context.Context, _ *apiv1alpha1.AgentInstance) (*a2aclient.Client, error) {
	return a2aclient.NewFromEndpoints(ctx, []*a2a.AgentInterface{{
		URL: "127.0.0.1:1234", ProtocolBinding: a2a.TransportProtocolGRPC, ProtocolVersion: a2a.Version,
	}}, a2agrpc.WithGRPCTransport(grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return d.listener.Dial() })))
}

type runtimeCancelableExecutor struct {
	a2asrv.AgentExecutor
	cleanupStarted chan struct{}
	cleanupRelease chan struct{}
	cleanupOnce    sync.Once
	cancelStarted  chan a2a.TaskID
}

func (e *runtimeCancelableExecutor) Cancel(_ context.Context, input *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		e.cancelStarted <- input.TaskID
		yield(a2a.NewStatusUpdateEvent(input, a2a.TaskStateCanceled, nil), nil)
	}
}

func (e *runtimeCancelableExecutor) Cleanup(_ context.Context, input *a2asrv.ExecutorContext, _ a2a.SendMessageResult, _ error) {
	if input.Message == nil {
		e.cleanupOnce.Do(func() { close(e.cleanupStarted) })
		<-e.cleanupRelease
	}
}

// Exercise the real SDK -> private gRPC -> PostgreSQL path. The public observer
// is disconnected before completion, and a successful save response is lost.
// Neither fault may cause execution or artifact appends to repeat.
func TestRuntimeTaskStoreThroughGRPC(t *testing.T) {
	for _, insecureIdentity := range []bool{false, true} {
		t.Run(fmt.Sprintf("insecure_identity=%t", insecureIdentity), func(t *testing.T) {
			testRuntimeTaskStoreThroughGRPC(t, insecureIdentity)
		})
	}
}

func testRuntimeTaskStoreThroughGRPC(t *testing.T, insecureIdentity bool) {
	t.Setenv(apia2a.InsecureTaskStoreAuthEnv, strconv.FormatBool(insecureIdentity))
	dsn := dbtest.StartT(context.WithoutCancel(t.Context()), t)
	dbtest.MigrateT(t, dsn, false)
	db, err := database.Connect(t.Context(), &database.PostgresConfig{URL: dsn})
	require.NoError(t, err)
	t.Cleanup(db.Close)
	store := &lostRuntimeSaveResponse{Client: database.NewClient(db), delayedCreate: make(chan string, 1), releaseCreate: make(chan struct{})}
	instance := createTaskStoreInstance(t, store.Client)
	id := instance.Id
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	const issuer = "https://substrate.test"
	authority, err := taskstore.NewAuthenticator(issuer, func(*jwt.Token) (any, error) { return &key.PublicKey, nil })
	require.NoError(t, err)
	if insecureIdentity {
		authority = taskstore.NewInsecureAuthenticator()
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": issuer, "aud": "kagent-task-store", "exp": time.Now().Add(time.Hour).Unix(),
		"ate.dev": map[string]any{"atespace": "team-a", "actorName": "ai-" + id, "actorUID": "actor-uid"},
	}).SignedString(key)
	require.NoError(t, err)
	listener := bufconn.Listen(DefaultMaxMessageSize)
	tasks := taskstore.NewService(store)
	server, err := New(Config{
		Listener: listener, Registerer: prometheus.NewRegistry(), SystemService: testSystemService(),
		Authenticator: &authimpl.UnsecureAuthenticator{}, RuntimeAuthenticator: authority,
		TaskStoreService: tasks,
	})
	require.NoError(t, err)
	serverCtx, stopServer := context.WithCancel(t.Context())
	t.Cleanup(stopServer)
	done := make(chan error, 1)
	go func() { done <- server.Start(serverCtx) }()
	t.Cleanup(func() {
		stopServer()
		require.NoError(t, <-done)
	})
	controller, err := controllerclient.New(controllerclient.Config{
		APIURL: "http://api.test", DialOptions: []grpc.DialOption{
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
			// Test fixture for Substrate egress injection. The adapter holds only
			// a placeholder; the real gateway must derive identity from mTLS.
			grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
				md, _ := metadata.FromOutgoingContext(ctx)
				if values := md.Get("authorization"); len(values) == 1 && values[0] == "Bearer substrate-actor" {
					md = md.Copy()
					md.Set("authorization", "Bearer "+token)
					ctx = metadata.NewOutgoingContext(ctx, md)
				}
				return invoke(ctx, method, req, reply, cc, opts...)
			}),
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, controller.Close()) })
	private := controller.TaskStoreService()
	read := &apiv1alpha1.TaskStoreServiceGetTaskRequest{AgentInstanceId: id, TaskId: "absent"}
	_, err = private.GetTask(t.Context(), read)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	forged := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "alice", "x-agent-name", id))
	_, err = private.GetTask(forged, read)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	authenticated := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", "Bearer "+token))
	if insecureIdentity {
		authenticated = metadata.NewOutgoingContext(t.Context(), metadata.Pairs(apia2a.InsecureRuntimeIdentityHeader, "team-a/ai-"+id+"/actor-uid"))
	} else {
		_, err = private.GetTask(metadata.NewOutgoingContext(t.Context(), metadata.Pairs(apia2a.InsecureRuntimeIdentityHeader, "team-a/ai-"+id+"/actor-uid")), read)
		require.Equal(t, codes.Unauthenticated, status.Code(err), "signed identity mode must ignore unsigned headers")
	}
	_, err = private.GetTask(authenticated, &apiv1alpha1.TaskStoreServiceGetTaskRequest{AgentInstanceId: uuid.NewString(), TaskId: "absent"})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	_, err = private.GetTask(metadata.AppendToOutgoingContext(authenticated, "x-share-token", "share"), read)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	for _, test := range []struct {
		name  string
		claim string
		value any
		want  codes.Code
	}{
		{"issuer", "iss", "https://untrusted.test", codes.Unauthenticated},
		{"audience", "aud", "another-service", codes.Unauthenticated},
		{"expired", "exp", time.Now().Add(-time.Minute).Unix(), codes.Unauthenticated},
		{"replaced actor", "actorUID", "replacement-uid", codes.NotFound},
		{"wrong atespace", "atespace", "another-team", codes.PermissionDenied},
	} {
		if insecureIdentity && test.claim != "actorUID" && test.claim != "atespace" {
			continue // Signature validation belongs to signed identity mode.
		}
		t.Run(test.name, func(t *testing.T) {
			claims := jwt.MapClaims{"iss": issuer, "aud": "kagent-task-store", "exp": time.Now().Add(time.Hour).Unix(),
				"ate.dev": map[string]any{"atespace": "team-a", "actorName": "ai-" + id, "actorUID": "actor-uid"}}
			if test.claim == "actorUID" || test.claim == "atespace" {
				claims["ate.dev"].(map[string]any)[test.claim] = test.value
			} else {
				claims[test.claim] = test.value
			}
			badToken, err := jwt.NewWithClaims(jwt.SigningMethodES256, claims).SignedString(key)
			require.NoError(t, err)
			badIdentity := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", "Bearer "+badToken))
			if insecureIdentity {
				actor := claims["ate.dev"].(map[string]any)
				badIdentity = metadata.NewOutgoingContext(t.Context(), metadata.Pairs(apia2a.InsecureRuntimeIdentityHeader, fmt.Sprintf("%s/%s/%s", actor["atespace"], actor["actorName"], actor["actorUID"])))
			}
			_, err = private.GetTask(badIdentity, read)
			require.Equal(t, test.want, status.Code(err))
		})
	}
	_, err = private.CreateTask(authenticated, &apiv1alpha1.TaskStoreServiceCreateTaskRequest{AgentInstanceId: id, Task: &a2apb.Task{}})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	identityPath := filepath.Join(t.TempDir(), "name")
	require.NoError(t, os.WriteFile(identityPath, []byte("ai-"+id), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(identityPath), "atespace"), []byte("team-a"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(identityPath), "uid"), []byte("actor-uid"), 0o600))
	runtimeStore := runtimetaskstore.New(controller, identityPath)
	release := make(chan struct{})
	var executions atomic.Int32
	executor := a2asrv.AgentExecutorFunc(func(ctx context.Context, exec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			if exec.Message.TaskID != "" && (exec.StoredTask == nil || exec.StoredTask.Status.State != a2a.TaskStateInputRequired) {
				yield(nil, fmt.Errorf("native continuation lost its waiting state"))
				return
			}
			executions.Add(1)
			if !yield(a2a.NewStatusUpdateEvent(exec, a2a.TaskStateWorking, nil), nil) {
				return
			}
			if exec.Message.Parts[0].Text() == "cancel" {
				<-ctx.Done()
				return
			}
			if exec.Message.Parts[0].Text() == "park" {
				yield(a2a.NewStatusUpdateEvent(exec, a2a.TaskStateInputRequired,
					a2a.NewMessageForTask(a2a.MessageRoleAgent, exec, a2a.NewTextPart("continue?"))), nil)
				return
			}
			artifact := a2a.NewArtifactEvent(exec, a2a.NewTextPart("before"))
			if !yield(artifact, nil) {
				return
			}
			select {
			case <-release:
			case <-ctx.Done():
				return
			}
			appendEvent := a2a.NewArtifactEvent(exec, a2a.NewTextPart("after"))
			appendEvent.Artifact.ID, appendEvent.Append = artifact.Artifact.ID, true
			if !yield(appendEvent, nil) {
				return
			}
			yield(a2a.NewStatusUpdateEvent(exec, a2a.TaskStateCompleted, nil), nil)
		}
	})
	native := &runtimeCancelableExecutor{AgentExecutor: executor, cleanupStarted: make(chan struct{}), cleanupRelease: make(chan struct{}), cancelStarted: make(chan a2a.TaskID, 2)}
	wrapped := runtimeStore.WrapExecutor(native)
	handler := a2asrv.NewHandler(wrapped, a2asrv.WithTaskStore(runtimeStore), a2asrv.WithCallInterceptors(wrapped),
		a2asrv.WithConcurrencyConfig(limiter.ConcurrencyConfig{MaxExecutions: 1}))
	runtimeListener := bufconn.Listen(DefaultMaxMessageSize)
	runtimeServer := grpc.NewServer()
	a2agrpc.NewHandler(handler).RegisterWith(runtimeServer)
	go func() { _ = runtimeServer.Serve(runtimeListener) }()
	t.Cleanup(runtimeServer.Stop)
	// Independent public gateways share only PostgreSQL and the runtime. Each
	// observer owns a separate upstream connection; neither gateway ingests events.
	gateways := make([]*grpc.ClientConn, 2)
	for i := range gateways {
		publicListener := bufconn.Listen(DefaultMaxMessageSize)
		public, err := New(Config{
			Listener: publicListener, Registerer: prometheus.NewRegistry(), SystemService: testSystemService(),
			Authenticator: &authimpl.UnsecureAuthenticator{},
			A2AHandler:    a2agateway.New(store, &auth.NoopAuthorizer{}, taskStoreRuntimeDialer{runtimeListener}, "http://gateway.test"),
		})
		require.NoError(t, err)
		publicCtx, stop := context.WithCancel(t.Context())
		publicDone := make(chan error, 1)
		go func() { publicDone <- public.Start(publicCtx) }()
		t.Cleanup(func() { stop(); require.NoError(t, <-publicDone) })
		gateways[i], err = grpc.NewClient("passthrough:///gateway", grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return publicListener.Dial() }))
		require.NoError(t, err)
		t.Cleanup(func() { _ = gateways[i].Close() })
	}
	publicCtx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs(apia2a.AgentInstanceIDHeader, id, "x-user-id", "alice"))
	observer, stopObserver := context.WithTimeout(publicCtx, 10*time.Second)
	defer stopObserver()
	input := &a2apb.SendMessageRequest{Message: &a2apb.Message{
		MessageId: "input-1", Role: a2apb.Role_ROLE_USER,
		Parts: []*a2apb.Part{{Content: &a2apb.Part_Text{Text: "work"}}},
	}}
	stream, err := a2apb.NewA2AServiceClient(gateways[0]).SendStreamingMessage(observer, input)
	require.NoError(t, err)
	var taskID string
	for {
		event, err := stream.Recv()
		require.NoError(t, err)
		if update := event.GetArtifactUpdate(); update != nil {
			taskID = update.TaskId
			break
		}
	}
	require.NoError(t, gateways[0].Close())
	// A subscription through another replica recovers the committed artifact
	// before following updates. Public sends are not replay operations.
	second := a2apb.NewA2AServiceClient(gateways[1])
	concurrent := proto.CloneOf(input)
	concurrent.Message.MessageId = "concurrent-input"
	_, err = second.SendMessage(publicCtx, concurrent)
	require.Equal(t, codes.FailedPrecondition, status.Code(err), "active work returns a protocol-level busy response")
	readActive, err := second.GetTask(publicCtx, &a2apb.GetTaskRequest{Id: taskID})
	require.NoError(t, err)
	require.Equal(t, taskID, readActive.Id)
	resumed, err := second.SubscribeToTask(publicCtx, &a2apb.SubscribeToTaskRequest{Id: taskID})
	require.NoError(t, err)
	initial, err := resumed.Recv()
	require.NoError(t, err)
	require.Equal(t, taskID, initial.GetTask().GetId())
	require.Len(t, initial.GetTask().GetArtifacts(), 1)
	close(release)
	for {
		event, err := resumed.Recv()
		require.NoError(t, err)
		if event.GetTask().GetStatus().GetState() == a2apb.TaskState_TASK_STATE_COMPLETED {
			break
		}
	}
	require.Eventually(t, func() bool {
		task, err := store.GetAgentInstanceTask(t.Context(), id, taskID, nil)
		return err == nil && task.Status.State == a2a.TaskStateCompleted
	}, 5*time.Second, 10*time.Millisecond)
	stored, _, err := store.GetVersionedAgentInstanceTask(t.Context(), id, taskID)
	require.NoError(t, err)
	require.Len(t, stored.Artifacts, 1)
	var output string
	for _, part := range stored.Artifacts[0].Parts {
		output += string(part.Content.(a2a.Text))
	}
	require.Equal(t, "beforeafter", output)
	require.EqualValues(t, 1, executions.Load())
	readBack, err := second.GetTask(publicCtx, &a2apb.GetTaskRequest{Id: taskID})
	require.NoError(t, err)
	require.Equal(t, a2apb.TaskState_TASK_STATE_COMPLETED, readBack.Status.State)
	require.True(t, store.lost.Load())
	require.True(t, store.lostCreate.Load())
	parkInput := proto.CloneOf(input)
	parkInput.Message.MessageId = "input-park"
	parkInput.Message.Parts[0].Content = &a2apb.Part_Text{Text: "park"}
	parked, err := second.SendMessage(publicCtx, parkInput)
	require.NoError(t, err)
	require.Equal(t, a2apb.TaskState_TASK_STATE_INPUT_REQUIRED, parked.GetTask().GetStatus().GetState())
	replyInput := proto.CloneOf(input)
	replyInput.Message.MessageId = "input-reply"
	replyInput.Message.TaskId = parked.GetTask().GetId()
	reply, err := second.SendStreamingMessage(publicCtx, replyInput)
	require.NoError(t, err)
	var replyCompleted bool
	for !replyCompleted {
		event, err := reply.Recv()
		require.NoError(t, err)
		replyCompleted = event.GetTask().GetStatus().GetState() == a2apb.TaskState_TASK_STATE_COMPLETED
	}
	beforeReplyRetry := executions.Load()
	_, err = second.SendMessage(publicCtx, replyInput)
	require.Error(t, err, "a completed task cannot be continued")
	require.Equal(t, beforeReplyRetry, executions.Load())

	// Cancellation's native cleanup runs after the execution cleanup in the
	// pinned SDK. Neither observer may expose CANCELED before both finish.
	cancelInput := proto.CloneOf(input)
	cancelInput.Message.MessageId = "input-cancel"
	cancelInput.Message.Parts[0].Content = &a2apb.Part_Text{Text: "cancel"}
	cancelStream, err := second.SendStreamingMessage(publicCtx, cancelInput)
	require.NoError(t, err)
	working, err := cancelStream.Recv()
	require.NoError(t, err)
	cancelID := working.GetTask().GetId()
	require.NotEmpty(t, cancelID)
	result := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(publicCtx, 5*time.Second)
		defer cancel()
		_, err := second.CancelTask(ctx, &a2apb.CancelTaskRequest{Id: cancelID})
		result <- err
	}()
	select {
	case <-native.cleanupStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("native cancellation cleanup did not run")
	}
	require.Never(t, func() bool {
		visible, err := second.GetTask(publicCtx, &a2apb.GetTaskRequest{Id: cancelID})
		require.NoError(t, err)
		return visible.Status.State == a2apb.TaskState_TASK_STATE_CANCELED
	}, 150*time.Millisecond, 10*time.Millisecond)
	close(native.cleanupRelease)
	require.NoError(t, <-result)
	canceled, err := second.GetTask(publicCtx, &a2apb.GetTaskRequest{Id: cancelID})
	require.NoError(t, err)
	require.Equal(t, a2apb.TaskState_TASK_STATE_CANCELED, canceled.Status.State)

	// Cancellation after Create commits but before its response reaches the SDK
	// must prevent native work from starting.
	lateInput := proto.CloneOf(input)
	lateInput.Message.MessageId = "input-late"
	lateInput.Message.Parts[0].Content = &a2apb.Part_Text{Text: "late"}
	beforeLate := executions.Load()
	lateResult := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(publicCtx, 5*time.Second)
		defer cancel()
		_, err := second.SendMessage(ctx, lateInput)
		lateResult <- err
	}()
	var lateID string
	select {
	case lateID = <-store.delayedCreate:
	case <-time.After(5 * time.Second):
		t.Fatal("initial Create did not commit")
	}
	go func() {
		ctx, cancel := context.WithTimeout(publicCtx, 5*time.Second)
		defer cancel()
		_, err := second.CancelTask(ctx, &a2apb.CancelTaskRequest{Id: lateID})
		result <- err
	}()
	for canceled := range native.cancelStarted {
		if string(canceled) == lateID {
			break
		}
	}
	require.Equal(t, beforeLate, executions.Load())
	close(store.releaseCreate)
	require.NoError(t, <-result)
	<-lateResult // The interrupted send must not execute.
	require.Equal(t, beforeLate, executions.Load())
	failed := proto.CloneOf(input)
	failed.Message.MessageId = "input-rejected-create"
	failed.Message.Parts[0].Content = &a2apb.Part_Text{Text: "reject-create"}
	beforeFailed := executions.Load()
	_, err = second.SendMessage(publicCtx, failed)
	require.Error(t, err)
	require.Equal(t, beforeFailed, executions.Load(), "a failed Create must not start native work")
	runtimeServer.Stop()
	_, err = second.GetTask(publicCtx, &a2apb.GetTaskRequest{Id: taskID})
	require.NoError(t, err)
	if python := os.Getenv("KAGENT_TEST_PYTHON"); python != "" {
		t.Run("python SDK with PostgreSQL", func(t *testing.T) {
			// Run the Python adapter against this same authenticated API and
			// PostgreSQL instance. Only native work and egress JWT injection are
			// controlled fixtures. Enable with the repository venv's interpreter.
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			go func() { _ = server.server.Serve(listener) }()
			t.Cleanup(func() { _ = listener.Close() })
			root, err := filepath.Abs("../../../..")
			require.NoError(t, err)
			paths, err := filepath.Glob(filepath.Join(root, "python/packages/*/src"))
			require.NoError(t, err)
			store.lost.Store(false)
			store.lostCreate.Store(false)
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			command := exec.CommandContext(ctx, python, filepath.Join(root, "python/packages/kagent-core/tests/task_store_postgres_probe.py"))
			command.Env = append(os.Environ(),
				"PYTHONPATH="+strings.Join(paths, string(os.PathListSeparator)),
				"KAGENT_TASKSTORE_TEST_ENDPOINT="+listener.Addr().String(),
				"KAGENT_TASKSTORE_TEST_IDENTITY="+identityPath,
				"KAGENT_TASKSTORE_TEST_CONTEXT="+instance.ContextId,
				"KAGENT_TASKSTORE_TEST_TOKEN="+token,
			)
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			t.Logf("%s", output)
			require.True(t, store.lost.Load())
			require.True(t, store.lostCreate.Load())
		})
	}
	t.Run("settlement publishes without a lifecycle worker", func(t *testing.T) {
		finished := &a2apb.Task{Id: uuid.NewString(), ContextId: instance.ContextId, Status: &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_SUBMITTED}}
		created, err := private.CreateTask(authenticated, &apiv1alpha1.TaskStoreServiceCreateTaskRequest{AgentInstanceId: id, Task: finished})
		require.NoError(t, err)
		finished.Status.State = a2apb.TaskState_TASK_STATE_COMPLETED
		saved, err := private.UpdateTask(authenticated, &apiv1alpha1.TaskStoreServiceUpdateTaskRequest{
			AgentInstanceId: id, Task: finished, ExpectedVersion: created.Version,
		})
		require.NoError(t, err)
		_, err = private.SettleTask(authenticated, &apiv1alpha1.TaskStoreServiceSettleTaskRequest{
			AgentInstanceId: id, TaskId: finished.Id, Version: saved.Version,
		})
		require.NoError(t, err)
		visible, err := store.GetSettledAgentInstanceTask(t.Context(), id, finished.Id, nil)
		require.NoError(t, err)
		require.Equal(t, a2a.TaskStateCompleted, visible.Status.State)
		_, err = private.SettleTask(authenticated, &apiv1alpha1.TaskStoreServiceSettleTaskRequest{
			AgentInstanceId: id, TaskId: finished.Id, Version: saved.Version,
		})
		require.NoError(t, err)
	})
}

// Create through the lifecycle store so the fixture exercises the same resource
// pins, authority and actor identity invariants as a real runtime instance.
func createTaskStoreInstance(t *testing.T, store *database.Client) *apiv1alpha1.AgentInstance {
	t.Helper()
	revision := database.RuntimeRevision{
		Revision: "revision-1", Namespace: "team-a", AgentTemplateName: "assistant", AgentTemplateUID: "template-uid",
		HarnessName: "kagent", HarnessUID: "harness-uid", SourceSnapshot: []byte("{}"),
		AgentCard: &a2apb.AgentCard{Name: "assistant"}, EgressDestinations: []string{},
		ActorTemplateAtespace: "team-a", ActorTemplateName: "assistant-kagent-revision", ActorTemplateUID: "actor-template-uid",
	}
	require.NoError(t, store.UpsertAgentTemplateHarnessPair(t.Context(), database.AgentTemplateHarnessPair{
		Namespace: revision.Namespace, AgentTemplateName: revision.AgentTemplateName, AgentTemplateUID: revision.AgentTemplateUID,
		HarnessName: revision.HarnessName, HarnessUID: revision.HarnessUID, DesiredRevision: revision.Revision,
	}))
	require.NoError(t, store.RecordRuntimeRevision(t.Context(), revision, true))
	instance, _, err := store.CreateAgentInstance(t.Context(), &apiv1alpha1.AgentInstance{
		Id: uuid.NewString(), Creator: "alice",
		Harness:       &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "kagent"},
		AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"},
	}, uuid.NewString())
	require.NoError(t, err)
	operation, err := store.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE)
	require.NoError(t, err)
	executor := uuid.New()
	claimed, err := store.ClaimAgentInstanceOperation(t.Context(), instance.Id, operation.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	instance, err = store.FinishAgentInstanceOperation(t.Context(), instance.Id, operation.ID, executor,
		substrate.ActorHost("team-a", substrate.ActorName(instance.Id), ""), "actor-uid", "")
	require.NoError(t, err)
	return instance
}
