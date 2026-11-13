package driver

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

// resultSink reports each tool result to onResult after recording it.
type resultSink struct {
	*recordingSink
	onResult func(runtime.ToolResult)
}

func (s resultSink) ToolResult(event runtime.ToolResult) error {
	if err := s.recordingSink.ToolResult(event); err != nil {
		return err
	}
	s.onResult(event)
	return nil
}

// modelCallSink reports each model call to onCall after recording it. Cancel
// discards the other events, so this is how a test observes the turn's
// progress past the replayed call.
type modelCallSink struct {
	*recordingSink
	onCall func()
}

func (s modelCallSink) ModelCall(event runtime.ModelCall) {
	s.recordingSink.ModelCall(event)
	s.onCall()
}

func TestDeferredReplayCancelsAfterItsCallSettles(t *testing.T) {
	t.Run("Given a replay whose call has its result, when the turn is canceled during later work, assert that it stops promptly and records the cancellation", func(t *testing.T) {
		b := newTestBroker(t)
		r := testRecord()
		require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
		// The replay returns the deferred call's result, then keeps working, as
		// Claude does while it waits on its next model call or runs a tool.
		dir, executable := writeFakeClaude(t, `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"11111111-1111-4111-8111-111111111111"}'
printf '%s\n' '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"call","content":"ok","is_error":false}]}}'
exec sleep 30
`)
		d := NewProcessDriver(ProcessConfig{Executable: executable, Workspace: dir, ApprovalBroker: b, MaxEventBytes: 1 << 20, MaxStderrBytes: 1024, InterruptGrace: 50 * time.Millisecond})
		_, _, pending, err := d.RestorePending()
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		sink := resultSink{recordingSink: &recordingSink{}, onResult: func(result runtime.ToolResult) {
			if result.ID != r.Tool.ID {
				return
			}
			// Claude's hooks consume the decision before the call runs.
			b.mu.Lock()
			saved, err := loadDeferred(b.directory, 1<<20)
			if err == nil {
				saved.Phase = phaseReleased
				err = saveDeferred(b.directory, 1<<20, saved)
			}
			b.mu.Unlock()
			require.NoError(t, err)
			cancel()
		}}
		started := time.Now()
		_, err = pending.Resume(ctx, &runtime.ApprovalDecision{ID: r.RequestID, Approved: true}, sink)

		assertSettledCancellation(t, b, err, started)
	})

	t.Run("Given a replay whose call has not returned yet, when the turn is canceled and the result then arrives, assert that it stops promptly and records the cancellation", func(t *testing.T) {
		b := newTestBroker(t)
		r := testRecord()
		require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
		// The result is held back until cancellation has denied the rest of the
		// turn, which is the ordering under test rather than a race to lose.
		dir, executable := writeFakeClaude(t, fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '{"type":"system","subtype":"init","session_id":"11111111-1111-4111-8111-111111111111"}'
while ! grep -q '"canceling":true' "%s" 2>/dev/null; do sleep 0.02; done
printf '%%s\n' '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"call","content":"ok","is_error":false}]}}'
exec sleep 30
`, filepath.Join(b.directory, "claude-pending.json")))
		d := NewProcessDriver(ProcessConfig{Executable: executable, Workspace: dir, ApprovalBroker: b, MaxEventBytes: 1 << 20, MaxStderrBytes: 1024, InterruptGrace: 50 * time.Millisecond})
		_, _, pending, err := d.RestorePending()
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() {
			time.Sleep(200 * time.Millisecond)
			// Claude's hooks consume the decision before the call runs.
			b.mu.Lock()
			saved, err := loadDeferred(b.directory, 1<<20)
			if err == nil {
				saved.Phase = phaseReleased
				err = saveDeferred(b.directory, 1<<20, saved)
			}
			b.mu.Unlock()
			if err != nil {
				t.Errorf("mark decision consumed: %v", err)
			}
			cancel()
		}()
		started := time.Now()
		_, err = pending.Resume(ctx, &runtime.ApprovalDecision{ID: r.RequestID, Approved: true}, &recordingSink{})

		assertSettledCancellation(t, b, err, started)
	})

	t.Run("Given a canceled replay whose stop decision has settled, when the cancel request's own context ends, assert that it records the cancellation", func(t *testing.T) {
		b := newTestBroker(t)
		r := testRecord()
		require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
		// Cancel replays the call to be denied, then keeps the process alive, as
		// Claude does while it writes its final message. The model call that
		// follows the result is what the test waits for: events reach the sink in
		// order, so it cannot be seen before the denied result was consumed.
		dir, executable := writeFakeClaude(t, `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"11111111-1111-4111-8111-111111111111"}'
printf '%s\n' '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"call","content":"denied","is_error":true}]}}'
printf '%s\n' '{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_1","model":"claude-opus-5-5","usage":{"input_tokens":5}}}}'
printf '%s\n' '{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}}'
exec sleep 30
`)
		d := NewProcessDriver(ProcessConfig{Executable: executable, Workspace: dir, ApprovalBroker: b, MaxEventBytes: 1 << 20, MaxStderrBytes: 1024, InterruptGrace: 50 * time.Millisecond})
		_, _, pending, err := d.RestorePending()
		require.NoError(t, err)

		// The CancelTask request's own context ends, which is what the a2a
		// executor passes here; its other callers pass context.Background.
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var once sync.Once
		sink := modelCallSink{recordingSink: &recordingSink{}, onCall: func() {
			once.Do(func() {
				// Claude's hooks consume the stop decision before the call returns.
				b.mu.Lock()
				saved, err := loadDeferred(b.directory, 1<<20)
				if err == nil {
					saved.Phase = phaseReleased
					err = saveDeferred(b.directory, 1<<20, saved)
				}
				b.mu.Unlock()
				require.NoError(t, err)
				cancel()
			})
		}}
		started := time.Now()
		err = pending.Cancel(ctx, sink)

		require.NoError(t, err)
		require.Less(t, time.Since(started), 5*time.Second)
		assertCanceledJournal(t, b)
	})
}

func assertSettledCancellation(t *testing.T, b *ApprovalBroker, err error, started time.Time) {
	t.Helper()
	require.ErrorIs(t, err, context.Canceled)
	var cleanupErr *runtime.CancellationError
	require.False(t, errors.As(err, &cleanupErr), "cancellation reported a cleanup failure: %v", err)
	require.Less(t, time.Since(started), 5*time.Second)
	assertCanceledJournal(t, b)
}

// assertCanceledJournal asserts the tombstone a restored harness reads as a
// resolved replay rather than an abandoned session.
func assertCanceledJournal(t *testing.T, b *ApprovalBroker) {
	t.Helper()
	saved, err := loadDeferred(b.directory, 1<<20)
	require.NoError(t, err)
	require.Equal(t, phaseCanceled, saved.Phase)
	restored := NewProcessDriver(ProcessConfig{ApprovalBroker: b, MaxEventBytes: 1 << 20})
	taskID, contextID, restoredPending, err := restored.RestorePending()
	require.NoError(t, err)
	require.Empty(t, taskID)
	require.Empty(t, contextID)
	require.Nil(t, restoredPending)
}
