package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type stalledStore interface {
	ListStalledSessionTasks(context.Context, time.Time, int) ([]database.StalledSessionTask, error)
	InterruptSessionTask(context.Context, string, string, time.Time, string) (*database.SessionTaskInterruption, error)
	GetSessionByID(context.Context, string) (*apiv1alpha1.Session, error)
}

type lostRuntimeRecorder interface {
	FailLostRuntime(context.Context, *apiv1alpha1.Session) (*apiv1alpha1.Failure, error)
}

// StalledTurnWorker ends turns whose runtime stopped saving events. The runtime
// persists a turn's events itself, so a submitted or working task that records
// nothing for longer than the timeout has a runtime that died or hung; the
// session keeps it as active work and refuses every later message until it ends.
type StalledTurnWorker struct {
	store        stalledStore
	sessions     lostRuntimeRecorder
	timeout      time.Duration
	pollInterval time.Duration
	interrupted  metric.Int64Counter
}

var _ manager.Runnable = (*StalledTurnWorker)(nil)
var _ manager.LeaderElectionRunnable = (*StalledTurnWorker)(nil)
var _ stalledStore = (*database.Client)(nil)
var _ lostRuntimeRecorder = (*Service)(nil)

func NewStalledTurnWorker(store stalledStore, sessions lostRuntimeRecorder, timeout, pollInterval time.Duration) (*StalledTurnWorker, error) {
	if timeout < 0 {
		return nil, fmt.Errorf("stalled turn timeout must be nonnegative")
	}
	if pollInterval <= 0 {
		return nil, fmt.Errorf("stalled turn poll interval must be positive")
	}
	interrupted, err := otel.Meter("github.com/kagent-dev/kagent/go/core/internal/service/session").Int64Counter(
		"kagent.session.turn_interrupted", metric.WithDescription("Turns ended by the stalled turn sweep."), metric.WithUnit("{task}"))
	if err != nil {
		return nil, fmt.Errorf("create stalled turn counter: %w", err)
	}
	return &StalledTurnWorker{store: store, sessions: sessions, timeout: timeout, pollInterval: pollInterval, interrupted: interrupted}, nil
}

func (*StalledTurnWorker) NeedLeaderElection() bool { return true }

// Start sweeps on the poll interval. Zero disables the sweep.
func (w *StalledTurnWorker) Start(ctx context.Context) error {
	if w.timeout == 0 {
		<-ctx.Done()
		return nil
	}
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		w.sweep(ctx, time.Now())
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	return nil
}

// sweep ends every turn stalled at now. Interrupted turns leave the list, so
// a full page is followed by another read until the list is short or empty.
func (w *StalledTurnWorker) sweep(ctx context.Context, now time.Time) {
	cutoff := now.Add(-w.timeout)
	for ctx.Err() == nil {
		stalled, err := w.store.ListStalledSessionTasks(ctx, cutoff, 100)
		if err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "list stalled turns", "error", err)
			return
		}
		ended := 0
		for _, task := range stalled {
			err := w.interrupt(ctx, task, cutoff)
			if err == nil {
				ended++
			} else if !errors.Is(err, database.ErrConflict) && !errors.Is(err, database.ErrFailedPrecondition) && !errors.Is(err, database.ErrNotFound) && ctx.Err() == nil {
				logging.FromContext(ctx).ErrorContext(ctx, "interrupt stalled turn", "session_id", task.SessionID, "task_id", task.TaskID, "error", err)
			}
		}
		if len(stalled) < 100 || ended == 0 {
			return
		}
	}
}

// interrupt ends one stalled turn. When the runtime is also lost, the Actor
// crashed or gone, the session records the loss so the person is told to start
// a new conversation instead of sending into a runtime that cannot answer.
func (w *StalledTurnWorker) interrupt(ctx context.Context, task database.StalledSessionTask, cutoff time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancel()
	outcome, err := w.store.InterruptSessionTask(ctx, task.SessionID, task.TaskID, cutoff, w.interruptionMessage())
	if err != nil {
		return err
	}
	w.interrupted.Add(ctx, 1)
	log := logging.FromContext(ctx)
	if outcome.Settled {
		log.InfoContext(ctx, "settled a stalled turn's saved outcome", "session_id", task.SessionID, "task_id", task.TaskID, "state", outcome.State, "silent_for", time.Since(task.LastEventAt))
		return nil
	}
	log.InfoContext(ctx, "interrupted stalled turn", "session_id", task.SessionID, "task_id", task.TaskID, "was", task.State, "silent_for", time.Since(task.LastEventAt))
	session, err := w.store.GetSessionByID(ctx, task.SessionID)
	if err != nil {
		return err
	}
	failure, err := w.sessions.FailLostRuntime(ctx, session)
	if err != nil {
		log.WarnContext(ctx, "check session runtime after interrupting a stalled turn", "session_id", task.SessionID, "error", err)
		return nil
	}
	if failure != nil {
		log.InfoContext(ctx, "stalled turn's runtime is lost", "session_id", task.SessionID, "reason", failure.GetReason(), "message", failure.GetMessage())
	}
	return nil
}

func (w *StalledTurnWorker) interruptionMessage() string {
	return fmt.Sprintf("turn interrupted: the runtime recorded no progress for %s", w.timeout)
}
