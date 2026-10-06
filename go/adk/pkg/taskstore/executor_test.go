package taskstore

import (
	"context"
	"errors"
	"iter"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	sdktaskstore "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestNativeInvocationOutlivesDisconnectDuringInitialSave(t *testing.T) {
	for _, runtime := range []tracing.Runtime{tracing.RuntimeCodex, tracing.RuntimeClaude} {
		t.Run(string(runtime), func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			observer, disconnect := context.WithCancel(t.Context())
			defer disconnect()
			ctx, invocation := tracing.StartInvocation(observer, provider.Tracer("test"), "invoke_agent", nil)
			// The SDK retains request values while detaching execution cancellation.
			ctx = context.WithoutCancel(ctx)
			state := &execution{ready: make(chan struct{})}
			ctx = context.WithValue(ctx, executionKey{}, state)
			input := &a2asrv.ExecutorContext{TaskID: "task", ContextID: "conversation"}
			native := a2asrv.AgentExecutorFunc(func(ctx context.Context, input *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
				return func(yield func(a2a.Event, error) bool) {
					require.Empty(t, exporter.GetSpans(), "native execution must inherit the unfinished invocation")
					require.True(t, tracing.InvocationFromContext(ctx).Adopt())
					ended, err := invocation.End(ctx, tracing.Result{TaskState: string(a2a.TaskStateCompleted)})
					require.NoError(t, err)
					require.True(t, ended)
					yield(a2a.NewStatusUpdateEvent(input, a2a.TaskStateCompleted, nil), nil)
				}
			})
			wrapper := (&Store{}).WrapExecutor(native, runtime, nil)
			initial := true
			for _, err := range wrapper.Execute(ctx, input) {
				require.NoError(t, err)
				if initial {
					initial = false
					// Disconnect before the initial yield even returns, then complete
					// the delayed save. The transport must no longer own this span.
					disconnect()
					ended, err := invocation.EndTransport(ctx, tracing.Result{Disposition: tracing.DispositionAbandoned})
					require.NoError(t, err)
					require.False(t, ended)
					require.Empty(t, exporter.GetSpans())
					recordSave(ctx, &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}, 1)
				}
			}
			spans := exporter.GetSpans()
			require.Len(t, spans, 1)
			require.Contains(t, spans[0].Attributes, attribute.String(tracing.AttributeTaskID, "task"))
			require.Contains(t, spans[0].Attributes, attribute.String(tracing.AttributeTaskState, string(a2a.TaskStateCompleted)))
		})
	}
}

func TestInvocationOwnershipWhenNativeNeverStarts(t *testing.T) {
	for _, test := range []struct {
		name        string
		runtime     tracing.Runtime
		failed      bool
		canceled    bool
		stopContext bool
		stopYield   bool
		want        tracing.Result
	}{
		{name: "failed initial save", runtime: tracing.RuntimeCodex, failed: true, stopContext: true, want: tracing.Result{Error: "persistence_failure"}},
		{name: "save conflict", runtime: tracing.RuntimeCodex, failed: true, want: tracing.Result{Error: "persistence_failure"}},
		{name: "canceled before native start", runtime: tracing.RuntimeClaude, canceled: true, want: tracing.Result{Disposition: tracing.DispositionCanceled, TaskState: string(a2a.TaskStateCanceled)}},
		{name: "execution context ended", runtime: tracing.RuntimeCodex, stopContext: true, want: tracing.Result{Disposition: tracing.DispositionInterrupted}},
		{name: "initial yield refused", runtime: tracing.RuntimeClaude, stopYield: true, want: tracing.Result{Disposition: tracing.DispositionAbandoned}},
		{name: "ADK retains transport ownership", runtime: tracing.RuntimeADKGo, stopYield: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx, invocation := tracing.StartInvocation(ctx, provider.Tracer("test"), "request", nil)
			state := &execution{ready: make(chan struct{})}
			ctx = context.WithValue(ctx, executionKey{}, state)
			input := &a2asrv.ExecutorContext{TaskID: "task", ContextID: "conversation"}
			native := a2asrv.AgentExecutorFunc(func(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
				t.Fatal("native execution must not start")
				return nil
			})
			wrapper := (&Store{}).WrapExecutor(native, test.runtime, nil)
			wrapper.Execute(ctx, input)(func(_ a2a.Event, err error) bool {
				if err != nil {
					require.ErrorIs(t, err, sdktaskstore.ErrConcurrentModification)
					return false
				}
				if test.failed {
					recordSaveFailure(ctx)
				}
				state.canceled.Store(test.canceled)
				if test.stopContext {
					cancel()
				} else {
					recordSave(ctx, &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}, 1)
				}
				return !test.stopYield
			})
			if test.runtime == tracing.RuntimeADKGo {
				require.Empty(t, exporter.GetSpans(), "the wrapper must not finish ADK's transport span")
				ended, err := invocation.EndTransport(ctx, tracing.Result{})
				require.NoError(t, err)
				require.True(t, ended, "the wrapper must not adopt ADK's transport span")
			} else {
				ended, err := invocation.EndTransport(ctx, tracing.Result{Disposition: tracing.DispositionAbandoned})
				require.NoError(t, err)
				require.False(t, ended)
			}
			spans := exporter.GetSpans()
			require.Len(t, spans, 1)
			if test.runtime.NativeHarness() {
				require.Contains(t, spans[0].Attributes, attribute.String(tracing.AttributeTaskID, "task"))
				require.Contains(t, spans[0].Attributes, attribute.String(tracing.AttributeConversationID, "conversation"))
			}
			if test.want.Disposition != "" {
				require.Contains(t, spans[0].Attributes, attribute.String(tracing.AttributeDisposition, test.want.Disposition))
			}
			if test.want.Error != "" {
				require.Contains(t, spans[0].Attributes, attribute.String(tracing.AttributeErrorType, test.want.Error))
			}
			if test.want.TaskState != "" {
				require.Contains(t, spans[0].Attributes, attribute.String(tracing.AttributeTaskState, test.want.TaskState))
			}
		})
	}
}

type settlementServer struct {
	apiv1alpha1.UnimplementedTaskStoreServiceServer
	exporter *tracetest.InMemoryExporter
	fail     bool
	settled  atomic.Bool
}

func (*settlementServer) UpdateTask(context.Context, *apiv1alpha1.TaskStoreServiceUpdateTaskRequest) (*apiv1alpha1.TaskStoreServiceUpdateTaskResponse, error) {
	return &apiv1alpha1.TaskStoreServiceUpdateTaskResponse{Version: 2}, nil
}

func (s *settlementServer) SettleTask(context.Context, *apiv1alpha1.TaskStoreServiceSettleTaskRequest) (*apiv1alpha1.TaskStoreServiceSettleTaskResponse, error) {
	// The final UpdateTask client span must already be exported when settlement
	// can first make the actor eligible for suspension.
	spans := s.exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "kagent.api.v1alpha1.TaskStoreService/UpdateTask" {
		return nil, status.Error(codes.Internal, "final save span was not flushed before settlement")
	}
	s.settled.Store(true)
	if s.fail {
		return nil, status.Error(codes.FailedPrecondition, "settlement rejected")
	}
	return &apiv1alpha1.TaskStoreServiceSettleTaskResponse{}, nil
}

func TestSettlementFlushesFinalSaveAndSettlement(t *testing.T) {
	for _, test := range []struct {
		name       string
		failFlush  bool
		failSettle bool
	}{
		{name: "success"},
		{name: "flush error does not prevent settlement", failFlush: true},
		{name: "failed settlement is still flushed", failSettle: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(time.Hour)))
			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(provider)
			t.Cleanup(func() {
				otel.SetTracerProvider(previous)
				require.NoError(t, provider.Shutdown(context.Background()))
			})
			api := &settlementServer{exporter: exporter, fail: test.failSettle}
			listener := bufconn.Listen(1 << 20)
			server := grpc.NewServer()
			apiv1alpha1.RegisterTaskStoreServiceServer(server, api)
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(server.Stop)
			client, err := controllerclient.New(controllerclient.Config{
				APIURL: "http://api.test", DialOptions: []grpc.DialOption{
					grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
				},
			})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			dir := t.TempDir()
			for field, value := range map[string]string{"name": "session-" + uuid.NewString(), "atespace": "team-a", "uid": "actor-uid"} {
				require.NoError(t, os.WriteFile(filepath.Join(dir, field), []byte(value), 0o600))
			}
			store := New(client, filepath.Join(dir, "name"))
			flushes := 0
			wrapper := store.WrapExecutor(a2asrv.AgentExecutorFunc(nil), "", func(ctx context.Context) error {
				flushes++
				require.NoError(t, ctx.Err(), "cleanup must survive observer cancellation")
				require.NoError(t, provider.ForceFlush(ctx))
				if test.failFlush {
					return errors.New("export failed")
				}
				return nil
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx = context.WithValue(ctx, executionKey{}, &execution{ready: make(chan struct{})})
			task := &a2a.Task{ID: "task", ContextID: "conversation", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}
			input := &a2asrv.ExecutorContext{TaskID: task.ID, ContextID: task.ContextID}
			wrapper.track(ctx, task.ID)
			_, err = store.Update(ctx, &sdktaskstore.UpdateRequest{
				Task: task, PrevVersion: 1, Event: a2a.NewStatusUpdateEvent(input, a2a.TaskStateCompleted, nil),
			})
			require.NoError(t, err)
			require.Empty(t, exporter.GetSpans())
			cancel()
			wrapper.Cleanup(ctx, input, task, nil)
			require.True(t, api.settled.Load())
			require.Equal(t, 2, flushes)
			spans := exporter.GetSpans()
			require.Len(t, spans, 2)
			require.Equal(t, "kagent.api.v1alpha1.TaskStoreService/SettleTask", spans[1].Name)
		})
	}
}

// recordingStore reports SDK saves to the executor, as Store does over gRPC,
// and keeps each task's saved states by its first message.
type recordingStore struct {
	*sdktaskstore.InMemory
	mu     *sync.Mutex
	states map[a2a.Text][]a2a.TaskState
}

func (s recordingStore) Create(ctx context.Context, task *a2a.Task) (sdktaskstore.TaskVersion, error) {
	version, err := s.InMemory.Create(ctx, task)
	s.record(ctx, task, version, err)
	return version, err
}

func (s recordingStore) Update(ctx context.Context, update *sdktaskstore.UpdateRequest) (sdktaskstore.TaskVersion, error) {
	version, err := s.InMemory.Update(ctx, update)
	s.record(ctx, update.Task, version, err)
	return version, err
}

func (s recordingStore) record(ctx context.Context, task *a2a.Task, version sdktaskstore.TaskVersion, err error) {
	if err == nil {
		recordSave(ctx, task, int64(version))
		s.mu.Lock()
		key := task.History[0].Parts[0].Content.(a2a.Text)
		s.states[key] = append(s.states[key], task.Status.State)
		s.mu.Unlock()
	}
}

func (s recordingStore) saved(key a2a.Text) []a2a.TaskState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.states[key])
}

type beforeHook struct {
	a2asrv.PassthroughCallInterceptor
	run func(context.Context, *a2asrv.Request)
}

func (h beforeHook) Before(ctx context.Context, _ *a2asrv.CallContext, request *a2asrv.Request) (context.Context, any, error) {
	h.run(ctx, request)
	return ctx, nil, nil
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the runtime")
		panic("unreachable")
	}
}

func TestSendAdmissionReleasesWhenCallEndsBeforeExecution(t *testing.T) {
	ended := (&Store{}).WrapExecutor(a2asrv.AgentExecutorFunc(nil), "", nil)
	call, end := context.WithCancel(t.Context())
	call, callCtx := a2asrv.NewCallContext(call, nil)
	_, _, err := ended.Before(call, callCtx, &a2asrv.Request{Payload: &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser)}})
	require.NoError(t, err)
	end()
	require.Eventually(t, func() bool {
		ended.mu.Lock()
		defer ended.mu.Unlock()
		return ended.admitted == nil
	}, 5*time.Second, time.Millisecond, "a call that ends before execution must free the slot")

	for _, test := range []struct {
		name string
		// park first leaves a task waiting for input; A then replies to it.
		park bool
		key  a2a.Text
		want []a2a.TaskState
	}{
		{name: "new task is canceled", key: "A", want: []a2a.TaskState{a2a.TaskStateSubmitted, a2a.TaskStateCanceled}},
		// The SDK saves the reply into history before the runtime's working event.
		{name: "reply returns its task to waiting", park: true, key: "park", want: []a2a.TaskState{a2a.TaskStateSubmitted,
			a2a.TaskStateInputRequired, a2a.TaskStateInputRequired, a2a.TaskStateWorking, a2a.TaskStateInputRequired}},
	} {
		t.Run(test.name, func(t *testing.T) {
			text := func(message *a2a.Message) a2a.Text { return message.Parts[0].Content.(a2a.Text) }
			started, release := make(chan a2a.Text, 4), make(chan struct{})
			wrapper := (&Store{}).WrapExecutor(a2asrv.AgentExecutorFunc(func(_ context.Context, input *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
				return func(yield func(a2a.Event, error) bool) {
					started <- text(input.Message)
					switch text(input.Message) {
					case "park":
						yield(a2a.NewStatusUpdateEvent(input, a2a.TaskStateInputRequired, nil), nil)
						return
					case "B":
						<-release
					}
					yield(a2a.NewStatusUpdateEvent(input, a2a.TaskStateCompleted, nil), nil)
				}
			}), "", nil)
			tasks := recordingStore{sdktaskstore.NewInMemory(nil), &sync.Mutex{}, map[a2a.Text][]a2a.TaskState{}}
			var handler a2asrv.RequestHandler
			send := func(ctx context.Context, message *a2a.Message) (a2a.SendMessageResult, error) {
				return handler.SendMessage(ctx, &a2a.SendMessageRequest{Message: message})
			}
			newMessage := func(body a2a.Text) *a2a.Message {
				return a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(string(body)))
			}
			doneB := make(chan error, 1)
			// Free A's slot after admission, as its call ending would, then let B take
			// and hold it while the SDK starts A. Calling release directly avoids
			// AfterFunc's goroutine and the SDK aborting a canceled reply.
			hook := beforeHook{run: func(ctx context.Context, request *a2asrv.Request) {
				if send, ok := request.Payload.(*a2a.SendMessageRequest); !ok || text(send.Message) != "A" {
					return
				}
				wrapper.release(ctx.Value(executionKey{}).(*execution))
				go func() {
					_, err := send(t.Context(), newMessage("B"))
					doneB <- err
				}()
				require.Equal(t, a2a.Text("B"), receive(t, started))
			}}
			handler = a2asrv.NewHandler(wrapper, a2asrv.WithTaskStore(tasks), a2asrv.WithCallInterceptors(wrapper, hook))

			reply := newMessage("A")
			if test.park {
				parked, err := send(t.Context(), newMessage("park"))
				require.NoError(t, err)
				require.Equal(t, a2a.Text("park"), receive(t, started))
				reply.TaskID, reply.ContextID = parked.TaskInfo().TaskID, parked.TaskInfo().ContextID
			}
			_, _ = send(t.Context(), reply)
			_, err := send(t.Context(), newMessage("busy"))
			require.ErrorIs(t, err, a2a.ErrUnsupportedOperation, "simultaneous sends must not both run")
			require.Eventually(t, func() bool { return slices.Equal(tasks.saved(test.key), test.want) },
				5*time.Second, 10*time.Millisecond, "the superseded send must reach a boundary without native work")
			close(release)
			require.NoError(t, receive(t, doneB))
			require.Eventually(t, func() bool {
				_, err := send(t.Context(), newMessage("C"))
				return err == nil
			}, 5*time.Second, 10*time.Millisecond)
			require.Equal(t, a2a.Text("C"), receive(t, started))
			require.Empty(t, started, "the superseded send must never start native work")
		})
	}
}
