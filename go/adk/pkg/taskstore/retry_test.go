package taskstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/telemetry"
)

// scriptedTaskStore records each request and answers with respond, which gets
// the method and the 1-based call number.
type scriptedTaskStore struct {
	runtimev1.UnimplementedTaskStoreServiceServer
	mu       sync.Mutex
	requests map[string][]proto.Message
	respond  func(ctx context.Context, method string, call int) error
}

func (s *scriptedTaskStore) handle(ctx context.Context, method string, request proto.Message) error {
	s.mu.Lock()
	s.requests[method] = append(s.requests[method], proto.Clone(request))
	call, respond := len(s.requests[method]), s.respond
	s.mu.Unlock()
	if respond == nil {
		return nil
	}
	return respond(ctx, method, call)
}

func (s *scriptedTaskStore) received(method string) []proto.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]proto.Message(nil), s.requests[method]...)
}

func (s *scriptedTaskStore) CreateTask(ctx context.Context, request *runtimev1.TaskStoreServiceCreateTaskRequest) (*runtimev1.TaskStoreServiceCreateTaskResponse, error) {
	if err := s.handle(ctx, "CreateTask", request); err != nil {
		return nil, err
	}
	return &runtimev1.TaskStoreServiceCreateTaskResponse{Version: 7}, nil
}

func (s *scriptedTaskStore) UpdateTask(ctx context.Context, request *runtimev1.TaskStoreServiceUpdateTaskRequest) (*runtimev1.TaskStoreServiceUpdateTaskResponse, error) {
	if err := s.handle(ctx, "UpdateTask", request); err != nil {
		return nil, err
	}
	return &runtimev1.TaskStoreServiceUpdateTaskResponse{Version: 7}, nil
}

func (s *scriptedTaskStore) GetTask(ctx context.Context, request *runtimev1.TaskStoreServiceGetTaskRequest) (*runtimev1.TaskStoreServiceGetTaskResponse, error) {
	if err := s.handle(ctx, "GetTask", request); err != nil {
		return nil, err
	}
	task, err := pbconv.ToProtoTask(workingTask())
	if err != nil {
		return nil, err
	}
	return &runtimev1.TaskStoreServiceGetTaskResponse{Stored: &runtimev1.StoredTask{Task: task, Version: 7}}, nil
}

func (s *scriptedTaskStore) ListTasks(ctx context.Context, request *runtimev1.TaskStoreServiceListTasksRequest) (*runtimev1.TaskStoreServiceListTasksResponse, error) {
	if err := s.handle(ctx, "ListTasks", request); err != nil {
		return nil, err
	}
	return &runtimev1.TaskStoreServiceListTasksResponse{Result: &a2apb.ListTasksResponse{}}, nil
}

func (s *scriptedTaskStore) SettleTask(ctx context.Context, request *runtimev1.TaskStoreServiceSettleTaskRequest) (*runtimev1.TaskStoreServiceSettleTaskResponse, error) {
	if err := s.handle(ctx, "SettleTask", request); err != nil {
		return nil, err
	}
	return &runtimev1.TaskStoreServiceSettleTaskResponse{}, nil
}

func newScriptedStore(t *testing.T, respond func(context.Context, string, int) error, budget, attempt time.Duration) (*Store, *scriptedTaskStore) {
	t.Helper()
	api := &scriptedTaskStore{requests: map[string][]proto.Message{}}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	runtimev1.RegisterTaskStoreServiceServer(server, api)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	client, err := controllerclient.New(controllerclient.Config{
		APIURL: "http://api.test", DialOptions: []grpc.DialOption{
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	// Connect first, so connection setup does not eat into a test's deadlines.
	_, err = client.TaskStoreService().ListTasks(t.Context(), &runtimev1.TaskStoreServiceListTasksRequest{})
	require.NoError(t, err)
	api.mu.Lock()
	api.requests, api.respond = map[string][]proto.Message{}, respond
	api.mu.Unlock()
	identity := filepath.Join(t.TempDir(), "name")
	require.NoError(t, os.WriteFile(identity, []byte("session-"+uuid.NewString()), 0o600))
	return New(client, identity, &configv1.TaskStoreConfig{OperationBudget: durationpb.New(budget), AttemptTimeout: durationpb.New(attempt)}), api
}

func workingTask() *a2a.Task {
	return &a2a.Task{ID: "task", ContextID: "conversation", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}
}

type capturedLogs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *capturedLogs) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *capturedLogs) records(t *testing.T, message string) []map[string]any {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	var found []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(c.buf.Bytes()))
	for decoder.More() {
		record := map[string]any{}
		require.NoError(t, decoder.Decode(&record))
		if record["msg"] == message {
			found = append(found, record)
		}
	}
	return found
}

func loggingContext(ctx context.Context) (context.Context, *capturedLogs) {
	logs := &capturedLogs{}
	return logging.IntoContext(ctx, slog.New(slog.NewJSONHandler(logs, nil))), logs
}

func executionContext(ctx context.Context) (context.Context, *execution) {
	state := &execution{ready: make(chan struct{})}
	return context.WithValue(ctx, executionKey{}, state), state
}

func unavailable(times int) func(context.Context, string, int) error {
	return func(_ context.Context, _ string, call int) error {
		if call <= times {
			return status.Error(codes.Unavailable, "upstream call failed: Connect: Connection refused")
		}
		return nil
	}
}

func TestRetryReplaysIdenticalRequestsUntilAcknowledged(t *testing.T) {
	for _, test := range []struct {
		method string
		call   func(context.Context, *Store) error
	}{
		{method: "CreateTask", call: func(ctx context.Context, store *Store) error {
			task := workingTask()
			task.Status.State = a2a.TaskStateSubmitted
			version, err := store.Create(ctx, task)
			if err == nil && version != 7 {
				return errors.New("unexpected version")
			}
			return err
		}},
		{method: "UpdateTask", call: func(ctx context.Context, store *Store) error {
			task := workingTask()
			version, err := store.Update(ctx, &sdktaskstore.UpdateRequest{
				Task: task, PrevVersion: 3, Event: a2a.NewStatusUpdateEvent(task, a2a.TaskStateWorking, nil),
			})
			if err == nil && version != 7 {
				return errors.New("unexpected version")
			}
			return err
		}},
		{method: "GetTask", call: func(ctx context.Context, store *Store) error {
			_, err := store.Get(ctx, "task")
			return err
		}},
		{method: "ListTasks", call: func(ctx context.Context, store *Store) error {
			_, err := store.List(ctx, &a2a.ListTasksRequest{})
			return err
		}},
	} {
		t.Run(test.method, func(t *testing.T) {
			store, api := newScriptedStore(t, unavailable(2), 5*time.Second, time.Second)
			ctx, logs := loggingContext(t.Context())
			ctx, state := executionContext(ctx)
			require.NoError(t, test.call(ctx, store))
			requests := api.received(test.method)
			require.Len(t, requests, 3)
			for _, request := range requests[1:] {
				require.True(t, proto.Equal(requests[0], request), "a retry changed the request")
			}
			require.False(t, state.failed.Load(), "an acknowledged retry must not fail the execution")
			recovered := logs.records(t, "task store call succeeded after retries")
			require.Len(t, recovered, 1)
			require.Equal(t, test.method, recovered[0]["operation"])
			require.EqualValues(t, 3, recovered[0]["attempts"])
		})
	}
}

func TestRetryCapsEachAttempt(t *testing.T) {
	hung := make(chan time.Duration, 1)
	store, api := newScriptedStore(t, func(ctx context.Context, _ string, call int) error {
		if call == 1 {
			// Hold the first attempt until the client gives up on it.
			start := time.Now()
			<-ctx.Done()
			hung <- time.Since(start)
			return ctx.Err()
		}
		return nil
	}, 5*time.Second, 100*time.Millisecond)
	_, err := store.Get(t.Context(), "task")
	require.NoError(t, err)
	require.Len(t, api.received("GetTask"), 2)
	require.Less(t, <-hung, 2*time.Second, "the attempt timeout must end a hung call long before the budget")
}

func TestRetryStopsWhenTheBudgetRunsOut(t *testing.T) {
	const budget = 1200 * time.Millisecond
	store, api := newScriptedStore(t, unavailable(1<<30), budget, 500*time.Millisecond)
	ctx, logs := loggingContext(t.Context())
	ctx, state := executionContext(ctx)
	task := workingTask()
	start := time.Now()
	_, err := store.Update(ctx, &sdktaskstore.UpdateRequest{
		Task: task, PrevVersion: 3, Event: a2a.NewStatusUpdateEvent(task, a2a.TaskStateWorking, nil),
	})
	elapsed := time.Since(start)
	// The budget can cut the last attempt short.
	require.Contains(t, []codes.Code{codes.Unavailable, codes.DeadlineExceeded}, status.Code(err))
	require.GreaterOrEqual(t, elapsed, budget, "the loop keeps retrying until the budget ends")
	require.Less(t, elapsed, budget+time.Second, "the loop must not outlive its budget")
	require.True(t, state.failed.Load(), "an unacknowledged save still fails the execution")

	attempts := len(api.received("UpdateTask"))
	require.GreaterOrEqual(t, attempts, 2)
	exhausted := logs.records(t, "task store retries exhausted")
	require.Len(t, exhausted, 1)
	record := exhausted[0]
	require.Equal(t, "UpdateTask", record["operation"])
	require.Equal(t, "task", record["task_id"])
	require.NotEmpty(t, record["session_id"])
	require.Equal(t, status.Code(err).String(), record["code"])
	// An attempt that times out before reaching the server still counts.
	logged, ok := record["attempts"].(float64)
	require.True(t, ok)
	require.GreaterOrEqual(t, int(logged), attempts)
	require.Contains(t, record, "elapsed")
	require.Contains(t, record, "budget")
}

func TestRetryStopsOnOtherStatuses(t *testing.T) {
	for _, code := range []codes.Code{
		codes.Aborted, codes.FailedPrecondition, codes.InvalidArgument, codes.NotFound, codes.AlreadyExists,
		codes.PermissionDenied, codes.Unauthenticated, codes.Internal, codes.Unknown, codes.ResourceExhausted,
	} {
		t.Run(code.String(), func(t *testing.T) {
			rejection := status.Error(code, "rejected")
			store, api := newScriptedStore(t, func(context.Context, string, int) error { return rejection }, 5*time.Second, time.Second)
			task := workingTask()
			_, err := store.Update(t.Context(), &sdktaskstore.UpdateRequest{
				Task: task, PrevVersion: 3, Event: a2a.NewStatusUpdateEvent(task, a2a.TaskStateWorking, nil),
			})
			require.Len(t, api.received("UpdateTask"), 1)
			require.Equal(t, sdkError(rejection).Error(), err.Error(), "the SDK classification must not change")
		})
	}
}

func TestRetryStopsWhenTheCallerCancels(t *testing.T) {
	canceled := func(err error) bool { return errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled }
	t.Run("during a wait", func(t *testing.T) {
		failed := make(chan struct{}, 1)
		store, api := newScriptedStore(t, func(context.Context, string, int) error {
			failed <- struct{}{}
			return status.Error(codes.Unavailable, "controller restarting")
		}, 10*time.Second, time.Second)
		ctx, cancel := context.WithCancel(t.Context())
		// The first wait is at least 100 ms. On a loaded machine the cancel can
		// still land before the response arrives, which is the attempt path.
		go func() {
			<-failed
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()
		start := time.Now()
		_, err := store.Get(ctx, "task")
		require.True(t, canceled(err), "got %v", err)
		require.Less(t, time.Since(start), 900*time.Millisecond, "cancellation must interrupt the wait")
		require.Len(t, api.received("GetTask"), 1)
	})
	t.Run("during an attempt", func(t *testing.T) {
		started := make(chan struct{}, 1)
		store, api := newScriptedStore(t, func(ctx context.Context, _ string, _ int) error {
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		}, 10*time.Second, 5*time.Second)
		ctx, cancel := context.WithCancel(t.Context())
		go func() {
			<-started
			cancel()
		}()
		start := time.Now()
		_, err := store.Get(ctx, "task")
		require.True(t, canceled(err), "got %v", err)
		require.Less(t, time.Since(start), 2*time.Second)
		require.Len(t, api.received("GetTask"), 1)
	})
}

// The caller must not see DeadlineExceeded before its deadline has passed.
func TestRetryHonorsAnEarlierCallerDeadline(t *testing.T) {
	for _, deadline := range []time.Duration{80 * time.Millisecond, 200 * time.Millisecond} {
		t.Run(deadline.String(), func(t *testing.T) {
			store, _ := newScriptedStore(t, unavailable(1<<30), 10*time.Second, time.Second)
			ctx, logs := loggingContext(t.Context())
			// Start the clock first, so elapsed covers the whole deadline.
			start := time.Now()
			ctx, cancel := context.WithTimeout(ctx, deadline)
			defer cancel()
			_, err := store.Get(ctx, "task")
			elapsed := time.Since(start)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.GreaterOrEqual(t, elapsed, deadline, "a live caller deadline must not be reported as expired")
			require.Less(t, elapsed, deadline+time.Second, "the caller's deadline must cap the budget")
			require.Empty(t, logs.records(t, "task store retries exhausted"), "the budget records nothing for a caller's own deadline")
		})
	}
}

func TestRetryWaitDoublesToTheCeiling(t *testing.T) {
	for failures, want := range map[int]time.Duration{
		1: 200 * time.Millisecond, 2: 400 * time.Millisecond, 3: 800 * time.Millisecond,
		4: 1600 * time.Millisecond, 5: 2 * time.Second, 6: 2 * time.Second, 10000: 2 * time.Second,
	} {
		require.Equal(t, want, retryWait(failures), "after %d failures", failures)
	}
	for _, wait := range []time.Duration{0, time.Nanosecond, 200 * time.Millisecond, 2 * time.Second} {
		seen := map[time.Duration]bool{}
		for range 1000 {
			got := jitter(wait)
			require.GreaterOrEqual(t, got, wait/2)
			require.LessOrEqual(t, got, wait)
			seen[got] = true
		}
		if wait >= 200*time.Millisecond {
			require.Greater(t, len(seen), 1, "waits must be spread, not fixed")
		}
	}
}

func TestNewAppliesTaskStoreSettings(t *testing.T) {
	defaults := [2]time.Duration{DefaultOperationBudget, DefaultAttemptTimeout}
	for _, test := range []struct {
		name     string
		settings *configv1.TaskStoreConfig
		want     [2]time.Duration
	}{
		{name: "absent", want: defaults},
		{name: "empty", settings: &configv1.TaskStoreConfig{}, want: defaults},
		{name: "set", settings: &configv1.TaskStoreConfig{OperationBudget: durationpb.New(2 * time.Minute), AttemptTimeout: durationpb.New(500 * time.Millisecond)}, want: [2]time.Duration{2 * time.Minute, 500 * time.Millisecond}},
		{name: "budget only", settings: &configv1.TaskStoreConfig{OperationBudget: durationpb.New(45 * time.Second)}, want: [2]time.Duration{45 * time.Second, DefaultAttemptTimeout}},
		{name: "not positive", settings: &configv1.TaskStoreConfig{OperationBudget: durationpb.New(0), AttemptTimeout: durationpb.New(-time.Second)}, want: defaults},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := New(nil, "", test.settings)
			require.Equal(t, test.want, [2]time.Duration{store.operationBudget, store.attemptTimeout})
		})
	}
}

// Settlement runs after the caller is gone and retries for its own budget.
func TestSettlementRetriesWithinTheOperationBudget(t *testing.T) {
	settle := func(t *testing.T, respond func(context.Context, string, int) error, budget, attempt time.Duration, flush func(context.Context) error) (*scriptedTaskStore, *capturedLogs, time.Duration) {
		t.Helper()
		store, api := newScriptedStore(t, respond, budget, attempt)
		wrapper := store.WrapExecutor(a2asrv.AgentExecutorFunc(nil), "", flush)
		ctx, logs := loggingContext(t.Context())
		ctx, cancel := context.WithCancel(ctx)
		ctx, _ = executionContext(ctx)
		task := &a2a.Task{ID: "task", ContextID: "conversation", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}
		input := &a2asrv.ExecutorContext{TaskID: task.ID, ContextID: task.ContextID}
		wrapper.track(ctx, task.ID)
		_, err := store.Update(ctx, &sdktaskstore.UpdateRequest{
			Task: task, PrevVersion: 1, Event: a2a.NewStatusUpdateEvent(input, a2a.TaskStateCompleted, nil),
		})
		require.NoError(t, err)
		cancel() // the client disconnected; cleanup still owns settlement
		start := time.Now()
		wrapper.Cleanup(ctx, input, task, nil)
		return api, logs, time.Since(start)
	}

	t.Run("controller returns", func(t *testing.T) {
		api, logs, _ := settle(t, func(_ context.Context, method string, call int) error {
			if method == "SettleTask" && call <= 2 {
				return status.Error(codes.Unavailable, "controller restarting")
			}
			return nil
		}, 5*time.Second, time.Second, nil)
		settles := api.received("SettleTask")
		require.Len(t, settles, 3, "settlement must retry through the outage")
		for _, request := range settles[1:] {
			require.True(t, proto.Equal(settles[0], request), "settlement must replay the exact request")
		}
		require.Equal(t, int64(7), settles[0].(*runtimev1.TaskStoreServiceSettleTaskRequest).GetVersion())
		require.Empty(t, logs.records(t, "task store retries exhausted"))
	})
	t.Run("controller stays away", func(t *testing.T) {
		const budget = 1200 * time.Millisecond
		api, logs, elapsed := settle(t, func(_ context.Context, method string, _ int) error {
			if method == "SettleTask" {
				return status.Error(codes.Unavailable, "controller gone")
			}
			return nil
		}, budget, 500*time.Millisecond, nil)
		require.GreaterOrEqual(t, len(api.received("SettleTask")), 2)
		require.GreaterOrEqual(t, elapsed, budget, "settlement must keep its budget after the caller cancels")
		require.Less(t, elapsed, budget+time.Second, "settlement must stop at its budget")
		exhausted := logs.records(t, "task store retries exhausted")
		require.Len(t, exhausted, 1)
		require.Equal(t, "SettleTask", exhausted[0]["operation"])
		require.Len(t, logs.records(t, "settle runtime task boundary"), 1)
	})
	// A slow flush must not spend settlement's budget.
	t.Run("slow telemetry preflush", func(t *testing.T) {
		flushes := 0
		api, logs, _ := settle(t, func(context.Context, string, int) error { return nil }, 300*time.Millisecond, 300*time.Millisecond, func(context.Context) error {
			flushes++
			if flushes == 1 {
				time.Sleep(time.Second)
			}
			return nil
		})
		require.Len(t, api.received("SettleTask"), 1)
		require.Empty(t, logs.records(t, "task store retries exhausted"))
		require.Empty(t, logs.records(t, "settle runtime task boundary"))
	})
	// A flush that blocks until its context ends stops at its own deadline,
	// and settlement still gets its whole budget afterwards.
	t.Run("flush that waits for its deadline", func(t *testing.T) {
		const budget = 1200 * time.Millisecond
		var remaining []time.Duration
		api, logs, elapsed := settle(t, func(_ context.Context, method string, _ int) error {
			if method == "SettleTask" {
				return status.Error(codes.Unavailable, "controller gone")
			}
			return nil
		}, budget, 500*time.Millisecond, func(ctx context.Context) error {
			deadline, ok := ctx.Deadline()
			require.True(t, ok, "every flush needs a deadline")
			remaining = append(remaining, time.Until(deadline))
			if len(remaining) > 1 {
				return nil
			}
			<-ctx.Done()
			return ctx.Err()
		})
		require.Len(t, remaining, 2, "one flush before settlement and one after")
		for _, left := range remaining {
			require.Positive(t, left, "each flush gets a fresh deadline, not one already spent")
			require.LessOrEqual(t, left, telemetry.FlushTimeout)
		}
		require.GreaterOrEqual(t, elapsed, telemetry.FlushTimeout+budget, "the flush must not spend settlement's budget")
		require.Len(t, logs.records(t, "flush telemetry before task settlement"), 1)
		require.GreaterOrEqual(t, len(api.received("SettleTask")), 2)
		exhausted := logs.records(t, "task store retries exhausted")
		require.Len(t, exhausted, 1)
		require.Equal(t, "SettleTask", exhausted[0]["operation"])
		// The budget can cut the last attempt short.
		require.Contains(t, []any{codes.Unavailable.String(), codes.DeadlineExceeded.String()}, exhausted[0]["code"])
	})
}
