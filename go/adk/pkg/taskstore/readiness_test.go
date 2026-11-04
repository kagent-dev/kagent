package taskstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	sdktaskstore "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

type readinessServer struct {
	runtimev1.UnimplementedTaskStoreServiceServer
	task   *a2apb.Task
	settle func(context.Context)

	mu sync.Mutex
	// unavailable counts the calls still to reject the way the egress proxy
	// does while the controller restarts: Unavailable, over a healthy connection.
	unavailable int
}

func (s *readinessServer) rejectCalls(count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unavailable = count
}

func (s *readinessServer) proxy(ctx context.Context, request any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	s.mu.Lock()
	reject := s.unavailable > 0
	if reject {
		s.unavailable--
	}
	s.mu.Unlock()
	if reject {
		return nil, status.Error(codes.Unavailable, "upstream call failed: Connect: Connection refused (os error 111)")
	}
	return handler(ctx, request)
}

func (*readinessServer) CreateTask(context.Context, *runtimev1.TaskStoreServiceCreateTaskRequest) (*runtimev1.TaskStoreServiceCreateTaskResponse, error) {
	return &runtimev1.TaskStoreServiceCreateTaskResponse{Version: 1}, nil
}

func (*readinessServer) UpdateTask(context.Context, *runtimev1.TaskStoreServiceUpdateTaskRequest) (*runtimev1.TaskStoreServiceUpdateTaskResponse, error) {
	return &runtimev1.TaskStoreServiceUpdateTaskResponse{Version: 2}, nil
}

func (s *readinessServer) GetTask(context.Context, *runtimev1.TaskStoreServiceGetTaskRequest) (*runtimev1.TaskStoreServiceGetTaskResponse, error) {
	return &runtimev1.TaskStoreServiceGetTaskResponse{Stored: &runtimev1.StoredTask{Task: s.task, Version: 2}}, nil
}

func (s *readinessServer) ListTasks(context.Context, *runtimev1.TaskStoreServiceListTasksRequest) (*runtimev1.TaskStoreServiceListTasksResponse, error) {
	return &runtimev1.TaskStoreServiceListTasksResponse{Result: &a2apb.ListTasksResponse{Tasks: []*a2apb.Task{s.task}}}, nil
}

func (s *readinessServer) SettleTask(ctx context.Context, _ *runtimev1.TaskStoreServiceSettleTaskRequest) (*runtimev1.TaskStoreServiceSettleTaskResponse, error) {
	if s.settle != nil {
		s.settle(ctx)
	}
	return &runtimev1.TaskStoreServiceSettleTaskResponse{}, nil
}

func newReadinessStore(t *testing.T, api *readinessServer, available *atomic.Bool, interceptor grpc.UnaryClientInterceptor) *Store {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.UnaryInterceptor(api.proxy))
	runtimev1.RegisterTaskStoreServiceServer(server, api)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		require.NoError(t, listener.Close())
	})
	options := []grpc.DialOption{
		grpc.WithConnectParams(grpc.ConnectParams{Backoff: backoff.Config{BaseDelay: time.Millisecond, Multiplier: 1, MaxDelay: time.Millisecond}, MinConnectTimeout: time.Second}),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			if available != nil && !available.Load() {
				return nil, errors.New("controller transport unavailable")
			}
			return listener.DialContext(ctx)
		}),
	}
	if interceptor != nil {
		options = append(options, grpc.WithUnaryInterceptor(interceptor))
	}
	client, err := controllerclient.New(controllerclient.Config{APIURL: "http://controller.test", DialOptions: options})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	identity := filepath.Join(t.TempDir(), "name")
	require.NoError(t, os.WriteFile(identity, []byte("session-"+uuid.NewString()), 0o600))
	return New(client, identity)
}

func TestTaskStoreWaitsForTransportRecovery(t *testing.T) {
	task := &a2a.Task{ID: "task", ContextID: "conversation", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}
	wire, err := pbconv.ToProtoTask(task)
	require.NoError(t, err)
	var available atomic.Bool
	entered, returned := make(chan string, 5), make(chan error, 5)
	store := newReadinessStore(t, &readinessServer{task: wire}, &available,
		func(ctx context.Context, method string, req, reply any, conn *grpc.ClientConn, invoke grpc.UnaryInvoker, options ...grpc.CallOption) error {
			if !strings.Contains(method, "TaskStoreService/") {
				return invoke(ctx, method, req, reply, conn, options...)
			}
			entered <- method
			err := invoke(ctx, method, req, reply, conn, options...)
			returned <- err
			return err
		})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	// A failed dial puts the shared connection in transient failure. The Memory
	// API keeps its normal fail-fast behavior on that same connection.
	_, err = store.client.MemoryService().List(ctx, &runtimev1.MemoryServiceListRequest{})
	require.Equal(t, codes.Unavailable, status.Code(err))

	type result struct {
		operation string
		version   sdktaskstore.TaskVersion
		tasks     int
		err       error
	}
	done := make(chan result, 5)
	go func() {
		version, err := store.Create(ctx, task)
		done <- result{operation: "create", version: version, err: err}
	}()
	go func() {
		version, err := store.Update(ctx, &sdktaskstore.UpdateRequest{Task: task, PrevVersion: 1,
			Event: a2a.NewStatusUpdateEvent(task, a2a.TaskStateCompleted, nil)})
		done <- result{operation: "update", version: version, err: err}
	}()
	go func() {
		stored, err := store.Get(ctx, task.ID)
		outcome := result{operation: "get", err: err}
		if stored != nil {
			outcome.version = stored.Version
		}
		done <- outcome
	}()
	go func() {
		listed, err := store.List(ctx, &a2a.ListTasksRequest{})
		outcome := result{operation: "list", err: err}
		if listed != nil {
			outcome.tasks = len(listed.Tasks)
		}
		done <- outcome
	}()
	go func() {
		wrapper := store.WrapExecutor(a2asrv.AgentExecutorFunc(nil), "", nil)
		state := &execution{ready: make(chan struct{})}
		state.boundary.Store(2)
		settle := context.WithValue(ctx, executionKey{}, state)
		wrapper.track(settle, task.ID)
		wrapper.Cleanup(settle, &a2asrv.ExecutorContext{TaskID: task.ID}, task, nil)
		done <- result{operation: "settle"}
	}()
	for range 5 {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("TaskStore calls did not start")
		}
	}
	select {
	case err := <-returned:
		t.Fatalf("TaskStore RPC returned before transport recovery: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	available.Store(true)
	for range 5 {
		select {
		case outcome := <-done:
			require.NoError(t, outcome.err, outcome.operation)
			switch outcome.operation {
			case "create":
				require.Equal(t, sdktaskstore.TaskVersion(1), outcome.version)
			case "update", "get":
				require.Equal(t, sdktaskstore.TaskVersion(2), outcome.version)
			case "list":
				require.Equal(t, 1, outcome.tasks)
			}
		case <-ctx.Done():
			t.Fatal("TaskStore calls did not recover")
		}
	}
	for range 5 {
		require.NoError(t, <-returned, "each queued RPC must reach the service after reconnecting")
	}
}

func TestSettlementSurvivesFlushDeadline(t *testing.T) {
	settlementDeadline := make(chan time.Time, 1)
	store := newReadinessStore(t, &readinessServer{settle: func(ctx context.Context) {
		deadline, _ := ctx.Deadline()
		settlementDeadline <- deadline
	}}, nil, nil)
	var firstDeadline, secondDeadline time.Time
	flushes := 0
	wrapper := store.WrapExecutor(a2asrv.AgentExecutorFunc(nil), "", func(ctx context.Context) error {
		flushes++
		require.NoError(t, ctx.Err(), "each flush gets a fresh context after observer cancellation")
		deadline, ok := ctx.Deadline()
		require.True(t, ok, "flush must remain bounded")
		if flushes == 1 {
			firstDeadline = deadline
			<-ctx.Done()
			return ctx.Err()
		}
		secondDeadline = deadline
		return nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	state := &execution{ready: make(chan struct{})}
	state.boundary.Store(2)
	ctx = context.WithValue(ctx, executionKey{}, state)
	wrapper.track(ctx, "task")
	cancel()
	wrapper.Cleanup(ctx, &a2asrv.ExecutorContext{TaskID: "task"}, nil, nil)
	require.Equal(t, 2, flushes)
	require.True(t, receive(t, settlementDeadline).After(firstDeadline), "an expired flush must not consume settlement's deadline")
	require.True(t, secondDeadline.After(firstDeadline), "the second flush must have its own deadline")
}

func completedUpdate() *sdktaskstore.UpdateRequest {
	task := &a2a.Task{ID: "task", ContextID: "conversation", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}
	return &sdktaskstore.UpdateRequest{Task: task, PrevVersion: 1, Event: a2a.NewStatusUpdateEvent(task, a2a.TaskStateCompleted, nil)}
}

// The egress proxy answers Unavailable at once while the controller restarts,
// over a connection that stays READY, so WaitForReady never waits. The save
// must keep retrying, past the former four attempts, until the controller
// answers again.
func TestTaskStoreRetriesUnavailableFromAReachableProxy(t *testing.T) {
	api := &readinessServer{}
	var mu sync.Mutex
	var updates []proto.Message
	var notReady atomic.Int32
	store := newReadinessStore(t, api, nil,
		func(ctx context.Context, method string, req, reply any, conn *grpc.ClientConn, invoke grpc.UnaryInvoker, options ...grpc.CallOption) error {
			if method == runtimev1.TaskStoreService_UpdateTask_FullMethodName {
				if conn.GetState() != connectivity.Ready {
					notReady.Add(1)
				}
				mu.Lock()
				updates = append(updates, proto.Clone(req.(proto.Message)))
				mu.Unlock()
			}
			return invoke(ctx, method, req, reply, conn, options...)
		})
	// A running runtime's connection to the proxy is already READY.
	_, err := store.Update(t.Context(), completedUpdate())
	require.NoError(t, err)
	mu.Lock()
	updates = nil
	mu.Unlock()
	notReady.Store(0)

	api.rejectCalls(6)
	version, err := store.Update(t.Context(), completedUpdate())
	require.NoError(t, err, "the save must outlast the restart")
	require.Equal(t, sdktaskstore.TaskVersion(2), version)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, updates, 7, "six rejected attempts, then the one that lands")
	require.Zero(t, notReady.Load(), "the connection stayed READY, so only the retry loop could wait")
	for _, request := range updates[1:] {
		require.True(t, proto.Equal(updates[0], request), "every attempt must replay the same save")
	}
}

// Settlement reaches the controller through the same proxy.
func TestSettlementRetriesUnavailableFromAReachableProxy(t *testing.T) {
	settled := make(chan struct{}, 1)
	api := &readinessServer{settle: func(context.Context) { settled <- struct{}{} }}
	store := newReadinessStore(t, api, nil, nil)
	wrapper := store.WrapExecutor(a2asrv.AgentExecutorFunc(nil), "", nil)
	state := &execution{ready: make(chan struct{})}
	state.boundary.Store(2)
	ctx := context.WithValue(t.Context(), executionKey{}, state)
	wrapper.track(ctx, "task")
	api.rejectCalls(6)
	wrapper.Cleanup(ctx, &a2asrv.ExecutorContext{TaskID: "task"}, nil, nil)
	require.Len(t, settled, 1, "settlement must land once the controller answers")
}

// Running out of budget returns the last RPC error, which names the cause,
// and logs the exhaustion once.
func TestTaskStoreRetryBudgetReturnsTheLastError(t *testing.T) {
	api := &readinessServer{}
	store := newReadinessStore(t, api, nil, nil)
	store.budget = 500 * time.Millisecond
	api.rejectCalls(math.MaxInt)
	var logs bytes.Buffer
	ctx := logging.IntoContext(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil)))
	_, err := store.Update(ctx, completedUpdate())
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.ErrorContains(t, err, "upstream call failed")
	var exhausted struct {
		Message  string `json:"msg"`
		Attempts int    `json:"attempts"`
		Error    string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(logs.Bytes(), &exhausted), "exactly one log record")
	require.Equal(t, "taskstore retries exhausted", exhausted.Message)
	require.Positive(t, exhausted.Attempts)
	require.Contains(t, exhausted.Error, "upstream call failed")
}

// A proxy can take a while to report a dead upstream, so the budget can end
// during an attempt. That attempt's own deadline error must not replace the
// proxy's error from the attempt before it.
func TestTaskStoreRetryBudgetEndingMidAttemptKeepsTheCause(t *testing.T) {
	api := &readinessServer{}
	store := newReadinessStore(t, api, nil,
		func(ctx context.Context, method string, req, reply any, conn *grpc.ClientConn, invoke grpc.UnaryInvoker, options ...grpc.CallOption) error {
			select {
			case <-time.After(300 * time.Millisecond):
			case <-ctx.Done():
				return status.FromContextError(ctx.Err()).Err()
			}
			return invoke(ctx, method, req, reply, conn, options...)
		})
	store.budget = time.Second
	api.rejectCalls(math.MaxInt)
	_, err := store.Update(t.Context(), completedUpdate())
	require.Equal(t, codes.Unavailable, status.Code(err), "unexpected error: %v", err)
	require.ErrorContains(t, err, "upstream call failed")
}

// A caller that cancels or runs out of time ends the operation itself. That
// is not retry exhaustion: the caller's error comes back and nothing is logged.
func TestTaskStoreRetryReturnsTheCallersEnd(t *testing.T) {
	for name, test := range map[string]struct {
		end  func(context.Context) (context.Context, context.CancelFunc)
		want error
	}{
		"canceled": {want: context.Canceled, end: func(ctx context.Context) (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(ctx)
			time.AfterFunc(300*time.Millisecond, cancel)
			return ctx, cancel
		}},
		"deadline": {want: context.DeadlineExceeded, end: func(ctx context.Context) (context.Context, context.CancelFunc) {
			return context.WithTimeout(ctx, 300*time.Millisecond)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			api := &readinessServer{}
			store := newReadinessStore(t, api, nil, nil)
			api.rejectCalls(math.MaxInt)
			var logs bytes.Buffer
			ctx, cancel := test.end(logging.IntoContext(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil))))
			defer cancel()
			_, err := store.Update(ctx, completedUpdate())
			// The end lands in a retry wait or, rarely, during an RPC.
			require.True(t, errors.Is(err, test.want) || status.Code(err) == status.FromContextError(test.want).Code(), "unexpected error: %v", err)
			require.Empty(t, logs.String())
		})
	}
}

func TestRetryJitterWaitsBetweenHalfAndAllOfTheDelay(t *testing.T) {
	for range 1000 {
		wait := jitter(time.Second)
		require.GreaterOrEqual(t, wait, 500*time.Millisecond)
		require.LessOrEqual(t, wait, time.Second)
	}
}
