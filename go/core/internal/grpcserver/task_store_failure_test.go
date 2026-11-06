package grpcserver

import (
	"context"
	"errors"
	"iter"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	sdktaskstore "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"

	kagenta2a "github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	runtimetaskstore "github.com/kagent-dev/kagent/go/adk/pkg/taskstore"
	"github.com/kagent-dev/kagent/go/core/internal/database"
)

type runtimeFailureExecutor struct {
	a2asrv.AgentExecutor
	cleanupStarted chan a2a.TaskID
	cleanupRelease chan struct{}
}

type failureDuringSave struct {
	sdktaskstore.Store
	started chan struct{}
}

func (s *failureDuringSave) Update(ctx context.Context, update *sdktaskstore.UpdateRequest) (sdktaskstore.TaskVersion, error) {
	if update.Task.Status.State == a2a.TaskStateWorking {
		close(s.started)
		<-ctx.Done()
	}
	return s.Store.Update(ctx, update)
}

func (e *runtimeFailureExecutor) Cleanup(ctx context.Context, input *a2asrv.ExecutorContext, result a2a.SendMessageResult, err error) {
	e.cleanupStarted <- input.TaskID
	<-e.cleanupRelease
	if cleaner, ok := e.AgentExecutor.(a2asrv.AgentExecutionCleaner); ok {
		cleaner.Cleanup(ctx, input, result, err)
	}
}

// Reuse the real controller, runtime credentials, and PostgreSQL fixture. Each
// failure must become durable without permitting suspension during native cleanup.
func testRuntimeFailuresSettle(t *testing.T, store *database.Client, runtimeStore *runtimetaskstore.Store, sessionID string) {
	t.Helper()
	root, err := adkagent.New(adkagent.Config{Name: "empty-text", Run: func(adkagent.InvocationContext) iter.Seq2[*adksession.Event, error] {
		return func(func(*adksession.Event, error) bool) {
			t.Error("empty text must fail conversion before invoking the agent")
		}
	}})
	require.NoError(t, err)
	adk, err := kagenta2a.NewKAgentExecutor(kagenta2a.KAgentExecutorConfig{
		AppName: "empty-text", SessionService: adksession.InMemoryService(), Logger: slog.New(slog.DiscardHandler),
		RunnerConfig: runner.Config{AppName: "empty-text", Agent: root},
	})
	require.NoError(t, err)
	workingSave := make(chan struct{})
	for _, test := range []struct {
		name     string
		executor a2asrv.AgentExecutor
		active   a2a.TaskState
		stream   bool
	}{
		{name: "empty text", executor: adk, active: a2a.TaskStateSubmitted},
		{name: "empty text streaming", executor: adk, active: a2a.TaskStateSubmitted, stream: true},
		{name: "executor error", active: a2a.TaskStateWorking, executor: a2asrv.AgentExecutorFunc(func(_ context.Context, input *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				if yield(a2a.NewStatusUpdateEvent(input, a2a.TaskStateWorking, nil), nil) {
					<-workingSave
					yield(nil, errors.New("executor failed"))
				}
			}
		})},
		{name: "executor panic", active: a2a.TaskStateSubmitted, executor: a2asrv.AgentExecutorFunc(func(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(func(a2a.Event, error) bool) { panic("executor panicked") }
		})},
	} {
		if !t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			native := &runtimeFailureExecutor{AgentExecutor: test.executor, cleanupStarted: make(chan a2a.TaskID, 1), cleanupRelease: make(chan struct{})}
			release := sync.OnceFunc(func() { close(native.cleanupRelease) })
			wrapped := runtimeStore.WrapExecutor(native, "", nil)
			var sdkStore sdktaskstore.Store = runtimeStore
			if test.active == a2a.TaskStateWorking {
				// Fail while an intermediate save is in flight, not merely while
				// the consumer is waiting for another event.
				sdkStore = &failureDuringSave{Store: runtimeStore, started: workingSave}
			}
			handler := a2asrv.NewHandler(wrapped, a2asrv.WithTaskStore(sdkStore), a2asrv.WithCallInterceptors(wrapped))
			message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(""))
			message.ContextID = sessionID
			var result a2a.SendMessageResult
			var sendErr error
			done := make(chan struct{})
			go func() {
				request := &a2a.SendMessageRequest{Message: message}
				if test.stream {
					for event, err := range handler.SendStreamingMessage(ctx, request) {
						if err != nil {
							sendErr = err
							break
						}
						result, _ = event.(a2a.SendMessageResult)
					}
				} else {
					result, sendErr = handler.SendMessage(ctx, request)
				}
				close(done)
			}()
			defer func() {
				release()
				cancel()
				<-done
			}()
			var taskID a2a.TaskID
			select {
			case taskID = <-native.cleanupStarted:
			case <-ctx.Done():
				t.Fatal("native cleanup did not start")
			}
			visible, err := store.GetSessionTask(ctx, sessionID, string(taskID), nil)
			require.NoError(t, err)
			require.Equal(t, test.active, visible.Status.State, "failure must not be published before cleanup")
			staged, version, err := store.GetVersionedSessionTask(ctx, sessionID, string(taskID))
			require.NoError(t, err)
			require.Equal(t, a2a.TaskStateFailed, staged.Status.State, "the failure must survive execution cancellation")
			_, err = store.ClaimSessionQuiescence(ctx)
			require.ErrorIs(t, err, database.ErrNotFound, "the actor must not suspend during cleanup")
			release()
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("failed task was not returned")
			}
			require.NoError(t, sendErr)
			task, ok := result.(*a2a.Task)
			require.True(t, ok)
			require.Equal(t, a2a.TaskStateFailed, task.Status.State)
			settled, err := store.GetSettledSessionTask(ctx, sessionID, string(taskID), nil)
			require.NoError(t, err)
			require.Equal(t, a2a.TaskStateFailed, settled.Status.State)
			require.Len(t, settled.History, 1)
			require.Equal(t, message.ID, settled.History[0].ID)
			_, settledVersion, err := store.GetVersionedSessionTask(ctx, sessionID, string(taskID))
			require.NoError(t, err)
			require.Equal(t, version, settledVersion, "settlement must publish the saved failure without another mutation")
			pending, err := store.HasPendingSessionQuiescence(ctx)
			require.NoError(t, err)
			require.True(t, pending, "a settled failure must schedule suspension")
		}) {
			return
		}
	}
	work, err := store.ClaimSessionQuiescence(t.Context())
	require.NoError(t, err)
	require.Equal(t, sessionID, work.Session.Id)
	require.Equal(t, a2a.TaskStateFailed, work.State)
}
