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

// revisionRepointPage bounds the sessions one listing of the sweep returns; a
// sweep walks the pages until the last.
const revisionRepointPage = 100

type repointStore interface {
	ListSessionsOnSupersededRevisions(context.Context, string, int) ([]*apiv1alpha1.Session, error)
}

type repointWorkflow interface {
	RepointQuiesced(context.Context, *apiv1alpha1.Session) (*apiv1alpha1.Session, error)
}

// RevisionRepointWorker moves the quiesced runtimes of sessions whose revision
// their agent has superseded onto the current one.
//
// A turn moves its own runtime before it wakes it, so a conversation that is
// written to takes its agent's current revision by itself. A conversation
// nobody writes to again keeps its revision pinned, and with it the
// ActorTemplate rendered for it, with everything an older release put there:
// the runtime revision GC collects a revision only once no session references
// it. The sweep moves those runtimes while they are suspended, at the interval
// the controller is given, through the same repoint a turn uses.
type RevisionRepointWorker struct {
	store     repointStore
	workflow  repointWorkflow
	interval  time.Duration
	repointed metric.Int64Counter
}

var _ manager.Runnable = (*RevisionRepointWorker)(nil)
var _ manager.LeaderElectionRunnable = (*RevisionRepointWorker)(nil)
var _ repointStore = (*database.Client)(nil)
var _ repointWorkflow = (*ActorWorkflow)(nil)

func NewRevisionRepointWorker(store repointStore, workflow repointWorkflow, interval time.Duration) (*RevisionRepointWorker, error) {
	if interval < 0 {
		return nil, fmt.Errorf("revision repoint interval must be nonnegative")
	}
	repointed, err := otel.Meter("github.com/kagent-dev/kagent/go/core/internal/service/session").Int64Counter(
		"kagent.session.runtime_repointed", metric.WithDescription("Session runtimes the revision sweep moved onto their agent's current revision."), metric.WithUnit("{session}"))
	if err != nil {
		return nil, fmt.Errorf("create revision repoint counter: %w", err)
	}
	return &RevisionRepointWorker{store: store, workflow: workflow, interval: interval, repointed: repointed}, nil
}

func (*RevisionRepointWorker) NeedLeaderElection() bool { return true }

// Start sweeps on the interval. Zero disables the sweep.
func (w *RevisionRepointWorker) Start(ctx context.Context) error {
	if w.interval == 0 {
		<-ctx.Done()
		return nil
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		w.sweep(ctx)
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	return nil
}

// sweep visits every settled session on a superseded revision once, by id, a
// page at a time. A session the sweep could not move stays in the listing and
// is visited again by the next sweep, never by this one.
func (w *RevisionRepointWorker) sweep(ctx context.Context) {
	afterID := ""
	for ctx.Err() == nil {
		listCtx, cancel := context.WithTimeout(ctx, time.Minute)
		sessions, err := w.store.ListSessionsOnSupersededRevisions(listCtx, afterID, revisionRepointPage)
		cancel()
		if err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "list sessions on superseded revisions", "error", err)
			return
		}
		for _, session := range sessions {
			if ctx.Err() != nil {
				return
			}
			w.repoint(ctx, session)
		}
		if len(sessions) < revisionRepointPage {
			return
		}
		afterID = sessions[len(sessions)-1].GetId()
	}
}

// repoint moves one runtime. A turn that reserved the session, or a lifecycle
// operation that claimed it since the listing, wins: the record refuses the
// move, and the next sweep looks again.
func (w *RevisionRepointWorker) repoint(ctx context.Context, session *apiv1alpha1.Session) {
	ctx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancel()
	moved, err := w.workflow.RepointQuiesced(ctx, session)
	switch {
	case errors.Is(err, database.ErrConflict):
		logging.FromContext(ctx).DebugContext(ctx, "session left for its lifecycle operation; its runtime stays on the superseded revision for now",
			"session_id", session.GetId(), "revision", session.GetPreparedRevision())
	case err != nil:
		logging.FromContext(ctx).ErrorContext(ctx, "move the session runtime to its agent's current revision",
			"session_id", session.GetId(), "revision", session.GetPreparedRevision(), "error", err)
	case moved.GetPreparedRevision() != session.GetPreparedRevision():
		w.repointed.Add(ctx, 1)
		logging.FromContext(ctx).InfoContext(ctx, "moved the session runtime to its agent's current revision",
			"session_id", session.GetId(), "revision", moved.GetPreparedRevision(), "superseded_revision", session.GetPreparedRevision())
	}
}
