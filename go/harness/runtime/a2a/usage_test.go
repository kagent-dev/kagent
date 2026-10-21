package a2a

import (
	"context"
	"errors"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

// sonnetCall is one Claude call: 1000 prompt tokens, 800 of them read from the
// cache, and 150 output tokens, 40 of them thinking.
var sonnetCall = runtime.ModelCall{
	ResponseModel: "claude-sonnet-4-5-20250929", InputTokens: 1000, CacheReadTokens: 800, CacheWriteTokens: 100,
	OutputTokens: 150, ReasoningOutputTokens: 40,
}

// leftoverCall is what Claude Code's session totals hold beyond the calls it
// reported, such as a subagent's final output. It names no response model.
var leftoverCall = runtime.ModelCall{RequestModel: "claude-sonnet-4-5", UsageOnly: true, InputTokens: 20, OutputTokens: 5}

func sonnetCounts(calls int64) apia2a.TokenCounts {
	return apia2a.TokenCounts{InputTokens: 1000 * calls, OutputTokens: 110 * calls, ReasoningTokens: 40 * calls, CachedInputTokens: 800 * calls, TotalTokens: 1150 * calls}
}

func statusUsage(t *testing.T, event a2atype.Event) (*a2atype.TaskStatusUpdateEvent, apia2a.Usage, bool) {
	t.Helper()
	status, ok := event.(*a2atype.TaskStatusUpdateEvent)
	require.True(t, ok, "event = %#v, want a status update", event)
	usage, found := apia2a.UsageFromMetadata(status.Metadata)
	return status, usage, found
}

func TestExecuteReportsTheTurnUsageOnItsFinalStatus(t *testing.T) {
	tests := []struct {
		name      string
		outcome   runtime.Outcome
		err       error
		wantState a2atype.TaskState
	}{
		{name: "completed", wantState: a2atype.TaskStateCompleted},
		{name: "failed", outcome: runtime.Outcome{Failure: &runtime.Failure{Message: "Claude Code reached its turn limit"}}, wantState: a2atype.TaskStateFailed},
		{name: "runtime error", err: errors.New("claude exited"), wantState: a2atype.TaskStateFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor, err := New(fakeRunner{run: func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
				sink.ModelCall(sonnetCall)
				sink.ModelCall(leftoverCall)
				return tt.outcome, tt.err
			}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
			require.NoError(t, err)
			events, errs := collect(executor.Execute(t.Context(), requestContext("task-1", "hello")))
			require.Empty(t, errs)

			status, usage, found := statusUsage(t, events[len(events)-1])
			require.Equal(t, tt.wantState, status.Status.State)
			require.True(t, found, "metadata = %#v", status.Metadata)
			leftover := apia2a.TokenCounts{InputTokens: 20, OutputTokens: 5, TotalTokens: 25}
			require.Equal(t, apia2a.Usage{
				TokenCounts: sonnetCounts(1).Add(leftover),
				Models:      []apia2a.ModelUsage{{Model: "claude-sonnet-4-5-20250929", TokenCounts: sonnetCounts(1)}},
			}, usage)
			if tt.wantState == a2atype.TaskStateCompleted {
				require.Nil(t, status.Status.Message, "a completed turn carries its usage without a message")
			}
		})
	}
}

func TestExecuteReportsNoUsageWithoutModelCalls(t *testing.T) {
	executor, err := New(fakeRunner{run: func(context.Context, runtime.Turn, runtime.EventSink) (runtime.Outcome, error) {
		return runtime.Outcome{}, nil
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	require.NoError(t, err)
	events, errs := collect(executor.Execute(t.Context(), requestContext("task-1", "hello")))
	require.Empty(t, errs)
	status, _, found := statusUsage(t, events[len(events)-1])
	require.False(t, found, "metadata = %#v", status.Metadata)
}

// A task that pauses for input and resumes reports its lifetime total: the
// resumed execution starts from the total the input-required status persisted.
func TestExecuteContinuesTheUsageOfAResumedTask(t *testing.T) {
	executor, err := New(fakeRunner{run: func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		sink.ModelCall(sonnetCall)
		return runtime.Outcome{Pending: &fakePendingTurn{
			request: &runtime.ApprovalRequest{ID: "7", CallID: "call-1", Name: "tools.write", Hint: "Allow tools.write?"},
			resume: func(_ context.Context, _ runtime.InputResponse, sink runtime.EventSink) (runtime.Outcome, error) {
				sink.ModelCall(sonnetCall)
				return runtime.Outcome{}, nil
			},
		}}, nil
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	require.NoError(t, err)
	first := requestContext("task-1", "write")
	events, errs := collect(executor.Execute(t.Context(), first))
	require.Empty(t, errs)
	paused, usage, found := statusUsage(t, events[len(events)-1])
	require.Equal(t, a2atype.TaskStateInputRequired, paused.Status.State)
	require.True(t, found)
	require.Equal(t, sonnetCounts(1), usage.TokenCounts)

	decision := a2atype.NewMessage(a2atype.MessageRoleUser)
	decision.TaskID, decision.ContextID = first.TaskID, first.ContextID
	require.NoError(t, apia2a.AttachHITL(decision, apia2a.ToolApprovalResponse{Type: apia2a.HITLTypeToolApprovalResponse, Approvals: []apia2a.ToolApproval{{ID: "7", Approved: true}}}))
	stored := &a2atype.Task{ID: first.TaskID, ContextID: first.ContextID, Status: paused.Status, Metadata: paused.Metadata}
	events, errs = collect(executor.Execute(t.Context(), &a2asrv.ExecutorContext{TaskID: first.TaskID, ContextID: first.ContextID, Message: decision, StoredTask: stored}))
	require.Empty(t, errs)
	completed, usage, found := statusUsage(t, events[len(events)-1])
	require.Equal(t, a2atype.TaskStateCompleted, completed.Status.State)
	require.True(t, found)
	require.Equal(t, apia2a.Usage{
		TokenCounts: sonnetCounts(2),
		Models:      []apia2a.ModelUsage{{Model: "claude-sonnet-4-5-20250929", TokenCounts: sonnetCounts(2)}},
	}, usage)
}

func TestCancelReportsTheUsageOfTheInterruptedExecution(t *testing.T) {
	started := make(chan struct{})
	executor, err := New(fakeRunner{run: func(ctx context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		sink.ModelCall(sonnetCall)
		close(started)
		<-ctx.Done()
		return runtime.Outcome{}, ctx.Err()
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	require.NoError(t, err)
	executed := make(chan struct{})
	go func() {
		defer close(executed)
		_, _ = collect(executor.Execute(context.Background(), requestContext("task-1", "hello")))
	}()
	<-started
	events, errs := collect(executor.Cancel(t.Context(), requestContext("task-1", "ignored")))
	<-executed
	require.Empty(t, errs)
	require.Len(t, events, 1)
	status, usage, found := statusUsage(t, events[0])
	require.Equal(t, a2atype.TaskStateCanceled, status.Status.State)
	require.True(t, found)
	require.Equal(t, sonnetCounts(1), usage.TokenCounts)
}

// A parked task has no running execution: its canceled status repeats the
// persisted total and adds the calls the native cleanup reports.
func TestCancelParkedTaskReportsItsPersistedUsage(t *testing.T) {
	executor, err := New(fakeRunner{run: func(context.Context, runtime.Turn, runtime.EventSink) (runtime.Outcome, error) {
		return runtime.Outcome{Pending: &fakePendingTurn{
			request: &runtime.ApprovalRequest{ID: "7", CallID: "call-1", Name: "tools.write"},
			cancel: func(_ context.Context, sink runtime.ModelCallSink) error {
				sink.ModelCall(sonnetCall)
				return nil
			},
		}}, nil
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	require.NoError(t, err)
	_, errs := collect(executor.Execute(t.Context(), requestContext("task-1", "write")))
	require.Empty(t, errs)

	persisted := map[string]any{}
	require.NoError(t, apia2a.AttachUsage(persisted, apia2a.Usage{TokenCounts: sonnetCounts(1)}))
	cancel := requestContext("task-1", "ignored")
	cancel.StoredTask = &a2atype.Task{ID: cancel.TaskID, ContextID: cancel.ContextID, Metadata: persisted}
	events, errs := collect(executor.Cancel(t.Context(), cancel))
	require.Empty(t, errs)
	require.Len(t, events, 1)
	status, usage, found := statusUsage(t, events[0])
	require.Equal(t, a2atype.TaskStateCanceled, status.Status.State)
	require.True(t, found)
	require.Equal(t, sonnetCounts(2), usage.TokenCounts)
}
