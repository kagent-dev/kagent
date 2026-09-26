package taskstore

import (
	"context"
	"errors"
	"iter"
	"net"
	"os"
	"path/filepath"
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
