package session

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type workflowStore interface {
	ClaimSessionQuiescence(context.Context, time.Duration, time.Duration, []string) (*database.SessionQuiescence, error)
	RenewSessionQuiescence(context.Context, *database.SessionQuiescence, time.Duration) error
	HasPendingSessionQuiescence(context.Context) (bool, error)
	FinishSessionQuiescence(context.Context, *database.SessionQuiescence, *database.SessionTaskSnapshot) error
	ReleaseSessionQuiescence(context.Context, *database.SessionQuiescence, *apiv1alpha1.QuiescenceFailure) error
	NextQuiescenceFencingGeneration(context.Context) (int64, error)
	GetSessionForRuntime(context.Context, string, string) (*apiv1alpha1.Session, error)
	GetSessionCheckpointSnapshot(context.Context, string, string) (*database.SessionTaskSnapshot, string, error)
	GetRuntimeRevision(context.Context, string) (*database.RuntimeRevision, error)
	GetCurrentRuntimeRevision(context.Context, string) (*database.RuntimeRevision, error)
	RepointSessionOperation(context.Context, string, uuid.UUID, uuid.UUID, string) error
	RepointSession(context.Context, string, string, string) (*apiv1alpha1.Session, error)
	BeginSessionOperation(context.Context, string, apiv1alpha1.RuntimeOperation) (*database.SessionOperation, error)
	ClaimSessionOperation(context.Context, string, uuid.UUID, uuid.UUID) (bool, error)
	ReleaseRuntimeOperation(context.Context, string, uuid.UUID, uuid.UUID) error
	CancelSessionOperation(context.Context, string, uuid.UUID, uuid.UUID) (*apiv1alpha1.Session, error)
	FinishSessionOperation(context.Context, string, uuid.UUID, uuid.UUID, string, string, string) (*apiv1alpha1.Session, error)
	FailSessionOperation(context.Context, string, uuid.UUID, uuid.UUID, *apiv1alpha1.Failure) (*apiv1alpha1.Session, error)
	GetSessionOperation(context.Context, string, uuid.UUID) (*database.SessionOperation, error)
	GetCurrentSessionOperation(context.Context, string) (*database.SessionOperation, error)
	HasSessionTasks(context.Context, string) (bool, error)
}

type actorClient interface {
	substrate.LifecycleClient
	PauseActor(context.Context, string, string) (*ateapipb.Actor, error)
	RepointActor(context.Context, string, string, string, string) (*ateapipb.Actor, error)
	RevertActor(context.Context, string, string) (*ateapipb.Actor, error)
	ReplaceActorEgressPolicy(context.Context, string, string, *ateapipb.EgressPolicy) error
	ListAllWorkers(context.Context) ([]*ateapipb.Worker, error)
}

// ActorWorkflow runs the imperative Substrate operations behind Session
// lifecycle RPCs. Only the claiming caller issues lifecycle mutations; others
// observe current completion or receive a pending/superseded-operation error.
type ActorWorkflow struct {
	store            workflowStore
	actors           actorClient
	recoveryInterval time.Duration
	quiescenceWake   chan struct{}
	pausedRuntimeTTL time.Duration
	deferred         deferrals
	// settling tracks the claims whose failed Pause or Quiesce is resolved from
	// the Actor's state; settleDelay is the first wait between its reads.
	settling    sync.WaitGroup
	settleDelay time.Duration
	// claimLease is how long a claim outlives its holder's last renewal before
	// another worker takes it over; the holder renews it every third of it.
	claimLease time.Duration
	// busyWait bounds how long a refused turn waits for its Actor's resume
	// while Substrate answers it later (resumeLater); busyRetry is the first
	// wait between resumes, of the turn's and of a lifecycle attempt's.
	busyWait  time.Duration
	busyRetry time.Duration
	// crashWait bounds how long TurnCrash asks again about an Actor still
	// RUNNING under a broken turn, every crashRetry.
	crashWait  time.Duration
	crashRetry time.Duration
	// directories tells the owner of a deleted session's volume that the
	// session's directory may go.
	directories DirectoryReleaser
}

// DirectoryReleaser tells the owner of a session's volume source that the
// session's directory may be deleted: the session was. kagent never deletes
// the directory or the volume itself.
type DirectoryReleaser interface {
	ReleaseSessionDirectory(context.Context, *apiv1alpha1.Session) error
}

// maxBusyRetry caps the wait between resumes Substrate answers later.
const maxBusyRetry = 5 * time.Second

// resumeLater reports an answer to ResumeActor that means not now, not never:
// no free worker while the pool rolls (ResourceExhausted), a resume a
// departing worker or a restarting ate-api cut short (Unavailable; the Actor
// is back at its snapshot) or another operation holding the Actor (Aborted),
// such as a repoint onto a newer template. Each clears on its own, and the
// same resume goes through once it has. Substrate's router parks a routed
// request on the same answers.
func resumeLater(err error) bool {
	switch status.Code(err) {
	case codes.ResourceExhausted, codes.Unavailable, codes.Aborted:
		return true
	}
	return false
}

// awaitResume runs resume until it succeeds, fails with an answer other than
// resumeLater, or ctx ends, waiting between runs from busyRetry, doubling up
// to maxBusyRetry. The answer of the last run is returned when ctx ends.
func (w *ActorWorkflow) awaitResume(ctx context.Context, resume func() error) error {
	start := time.Now()
	for delay := w.busyRetry; ; delay = min(2*delay, maxBusyRetry) {
		err := resume()
		if !resumeLater(err) {
			return err
		}
		select {
		case <-ctx.Done():
		case <-time.After(delay):
			if ctx.Err() == nil {
				continue
			}
		}
		return fmt.Errorf("still deferred by Substrate after %s: %w", time.Since(start).Round(100*time.Millisecond), err)
	}
}

type WorkflowOption func(*ActorWorkflow)

// WithPausedRuntimeTTL bounds how long a runtime paused for input stays on its
// node before the idle worker suspends it durably. Zero keeps every pause in
// place until the reply.
func WithPausedRuntimeTTL(ttl time.Duration) WorkflowOption {
	return func(w *ActorWorkflow) { w.pausedRuntimeTTL = ttl }
}

// WithDirectoryReleaser tells releaser about every deleted session that has a
// volume source. Without one, the owner of the volume learns of a deletion
// only by not finding the session.
func WithDirectoryReleaser(releaser DirectoryReleaser) WorkflowOption {
	return func(w *ActorWorkflow) { w.directories = releaser }
}

// quiescenceWake is a shared, single-slot hint channel. Callers must not close it;
// the workflow stops through context cancellation.
func NewActorWorkflow(store workflowStore, actors actorClient, quiescenceWake chan struct{}, recoveryInterval time.Duration, options ...WorkflowOption) *ActorWorkflow {
	workflow := &ActorWorkflow{store: store, actors: actors, recoveryInterval: recoveryInterval, quiescenceWake: quiescenceWake, settleDelay: time.Second, claimLease: 30 * time.Second,
		busyWait: 2 * time.Minute, busyRetry: 500 * time.Millisecond, crashWait: 15 * time.Second, crashRetry: time.Second}
	for _, option := range options {
		option(workflow)
	}
	return workflow
}

// Pause checkpoints the runtime on its current worker without changing the
// Session logical state or persisting A2A task state.
func (w *ActorWorkflow) Pause(ctx context.Context, session *apiv1alpha1.Session) error {
	revision, err := w.store.GetRuntimeRevision(ctx, session.GetPreparedRevision())
	if err != nil {
		return fmt.Errorf("load prepared revision: %w", err)
	}
	atespace, name := revision.ActorTemplateAtespace, substrate.ActorName(session.GetId())
	current, err := w.actors.GetActor(ctx, atespace, name)
	if err != nil {
		return err
	}
	if err := w.verifyActor(ctx, session, revision, current); err != nil {
		return err
	}
	actor, err := w.actors.PauseActor(ctx, atespace, name)
	if err != nil {
		return fmt.Errorf("pause Actor %s/%s: %w", atespace, name, err)
	}
	if !validActorIdentity(actor, revision, name) || actor.GetMetadata().GetUid() != current.GetMetadata().GetUid() || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_PAUSED {
		return fmt.Errorf("pause Actor %s/%s returned status %s", atespace, name, actor.GetStatus().GetState())
	}
	return nil
}

// Quiesce durably suspends the runtime without changing the Session's
// logical READY state and records its external snapshot URI. Only a checkpoint
// retains a copy after the Actor advances.
func (w *ActorWorkflow) Quiesce(ctx context.Context, session *apiv1alpha1.Session) (*database.SessionTaskSnapshot, error) {
	revision, err := w.store.GetRuntimeRevision(ctx, session.GetPreparedRevision())
	if err != nil {
		return nil, fmt.Errorf("load prepared revision: %w", err)
	}
	atespace, name := revision.ActorTemplateAtespace, substrate.ActorName(session.GetId())
	current, err := w.actors.GetActor(ctx, atespace, name)
	if err != nil {
		return nil, err
	}
	if err := w.verifyActor(ctx, session, revision, current); err != nil {
		return nil, err
	}
	actor, err := w.actors.SuspendActor(ctx, atespace, name)
	if err != nil {
		return nil, fmt.Errorf("suspend Actor %s/%s: %w", atespace, name, err)
	}
	if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		return nil, fmt.Errorf("suspend Actor %s/%s returned status %s", atespace, name, actor.GetStatus().GetState())
	}
	if !validActorIdentity(actor, revision, name) || actor.GetMetadata().GetUid() != current.GetMetadata().GetUid() {
		return nil, fmt.Errorf("suspend actor %s/%s returned invalid identity", atespace, name)
	}
	return externalSnapshot(actor)
}

// externalSnapshot is the session task snapshot a suspended Actor reports.
func externalSnapshot(actor *ateapipb.Actor) (*database.SessionTaskSnapshot, error) {
	atespace, name := actor.GetMetadata().GetAtespace(), actor.GetMetadata().GetName()
	snapshot := actor.GetStatus().GetExternalSnapshot()
	if snapshot.GetSnapshotUri() == "" {
		return nil, fmt.Errorf("suspend Actor %s/%s returned no snapshot", atespace, name)
	}
	scope := snapshot.GetContentScope()
	if scope != ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL && scope != ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA {
		return nil, fmt.Errorf("actor %s/%s returned invalid snapshot content scope %s", atespace, name, scope)
	}
	return &database.SessionTaskSnapshot{
		Atespace: atespace, URI: snapshot.GetSnapshotUri(),
		ContentScope: strings.TrimPrefix(scope.String(), "SNAPSHOT_CONTENT_SCOPE_"),
	}, nil
}

// boundaryActor reads the session's Actor after a Pause or Quiesce whose
// outcome is unknown, with the external snapshot it holds when it is this
// session's Actor and suspended with a valid one. A missing Actor is returned
// as nil without an error: nothing will settle the boundary later.
func (w *ActorWorkflow) boundaryActor(ctx context.Context, session *apiv1alpha1.Session) (*ateapipb.Actor, *database.SessionTaskSnapshot, error) {
	revision, err := w.store.GetRuntimeRevision(ctx, session.GetPreparedRevision())
	if err != nil {
		return nil, nil, fmt.Errorf("load prepared revision: %w", err)
	}
	atespace, name := revision.ActorTemplateAtespace, substrate.ActorName(session.GetId())
	actor, err := w.actors.GetActor(ctx, atespace, name)
	if status.Code(err) == codes.NotFound {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get Actor %s/%s: %w", atespace, name, err)
	}
	if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED || !validActorIdentity(actor, revision, name) {
		return actor, nil, nil
	}
	_, err = w.store.GetSessionForRuntime(ctx, session.GetId(), actor.GetMetadata().GetUid())
	if errors.Is(err, database.ErrNotFound) {
		return actor, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("verify runtime actor UID: %w", err)
	}
	snapshot, err := externalSnapshot(actor)
	if err != nil {
		return actor, nil, nil
	}
	return actor, snapshot, nil
}

// PauseNodeLost reports whether the session's Actor is paused on a checkpoint
// that only nodes without a worker still hold. Suspending such an Actor uploads
// the checkpoint through its node, which cannot complete, and Substrate leaves
// the Actor SUSPENDING; the pause is the node-loss handling's to crash, not a
// caller's to touch. A pause whose checkpoint has a durable copy, or that
// records no node, can be suspended from anywhere.
func (w *ActorWorkflow) PauseNodeLost(ctx context.Context, session *apiv1alpha1.Session) (bool, error) {
	revision, err := w.store.GetRuntimeRevision(ctx, session.GetPreparedRevision())
	if err != nil {
		return false, fmt.Errorf("load prepared revision: %w", err)
	}
	atespace, name := revision.ActorTemplateAtespace, substrate.ActorName(session.GetId())
	actor, err := w.actors.GetActor(ctx, atespace, name)
	if err != nil {
		return false, fmt.Errorf("get Actor %s/%s: %w", atespace, name, err)
	}
	if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_PAUSED {
		return false, nil
	}
	local := actor.GetStatus().GetLocalSnapshot()
	node := local.GetNodeName()
	if local.GetDurableCopy().GetSnapshotUri() != "" || node == "" {
		return false, nil
	}
	workers, err := w.actors.ListAllWorkers(ctx)
	if err != nil {
		return false, fmt.Errorf("list workers: %w", err)
	}
	for _, worker := range workers {
		if worker.GetNodeName() == node && worker.GetStatus().GetState() != ateapipb.WorkerState_WORKER_STATE_DRAINING {
			return false, nil
		}
	}
	return true, nil
}

// ErrRuntimeLost reports a lifecycle operation that Substrate answered can
// never succeed: the session records the loss as FAILED and stays deletable.
var ErrRuntimeLost = errors.New("session runtime is lost")

// RuntimeLossError is ErrRuntimeLost with the cause the session records, as
// RuntimeLost names it, and the answer that settled it.
type RuntimeLossError struct {
	Cause string
	Err   error
}

func (e *RuntimeLossError) Error() string { return e.Cause + ": " + e.Err.Error() }

func (e *RuntimeLossError) Is(target error) bool { return target == ErrRuntimeLost }

func (e *RuntimeLossError) Unwrap() error { return e.Err }

// RuntimeLost reports whether the session's Actor crashed and whether its
// runtime can no longer take a turn. The runtime is lost when the Actor is
// gone, or Substrate reports it CRASHED without a durable snapshot to return it
// to, so every later message would fail the way the last one did, unless the
// session holds no task: its runtime kept nothing, and the resume reverts the
// Actor to the snapshot it started from (AwaitRuntime). A crashed
// Actor that holds an external snapshot is not lost: the resume returns it to
// that snapshot (AwaitRuntime), and the conversation goes on from there. The
// cause, empty while the Actor has not crashed, names the Actor and what became
// of it, with the crash's message when Substrate recorded one. An answer
// Substrate cannot give is returned as the error it is: not knowing is not the
// same as lost.
func (w *ActorWorkflow) RuntimeLost(ctx context.Context, session *apiv1alpha1.Session) (string, bool, error) {
	cause, lost, _, err := w.actorCrash(ctx, session)
	return cause, lost, err
}

// TurnCrash is RuntimeLost for a turn whose runtime stream broke: Substrate
// marks the Actor CRASHED only once it notices that the worker went away under
// the turn, so an Actor still RUNNING is asked about again, every crashRetry
// for at most crashWait. An Actor still RUNNING at that bound has not crashed.
func (w *ActorWorkflow) TurnCrash(ctx context.Context, session *apiv1alpha1.Session) (string, bool, error) {
	deadline := time.Now().Add(w.crashWait)
	for {
		cause, lost, state, err := w.actorCrash(ctx, session)
		if err != nil || state != ateapipb.ActorState_ACTOR_STATE_RUNNING || !time.Now().Before(deadline) {
			return cause, lost, err
		}
		select {
		case <-ctx.Done():
			return "", false, ctx.Err()
		case <-time.After(min(w.crashRetry, time.Until(deadline))):
		}
	}
}

// actorCrash is RuntimeLost with the state the Actor was found in.
func (w *ActorWorkflow) actorCrash(ctx context.Context, session *apiv1alpha1.Session) (string, bool, ateapipb.ActorState, error) {
	revision, err := w.store.GetRuntimeRevision(ctx, session.GetPreparedRevision())
	if err != nil {
		return "", false, ateapipb.ActorState_ACTOR_STATE_UNSPECIFIED, fmt.Errorf("load prepared revision: %w", err)
	}
	atespace, name := revision.ActorTemplateAtespace, substrate.ActorName(session.GetId())
	actor, err := w.actors.GetActor(ctx, atespace, name)
	if status.Code(err) == codes.NotFound {
		return fmt.Sprintf("Actor %s/%s not found", atespace, name), true, ateapipb.ActorState_ACTOR_STATE_UNSPECIFIED, nil
	}
	if err != nil {
		return "", false, ateapipb.ActorState_ACTOR_STATE_UNSPECIFIED, fmt.Errorf("get Actor %s/%s: %w", atespace, name, err)
	}
	state := actor.GetStatus().GetState()
	if state != ateapipb.ActorState_ACTOR_STATE_CRASHED {
		return "", false, state, nil
	}
	revertible, err := w.revertible(ctx, session, actor)
	if err != nil {
		return "", false, state, err
	}
	if !revertible {
		return fmt.Sprintf("Actor %s/%s crashed with no snapshot to return to", atespace, name), true, state, nil
	}
	cause := fmt.Sprintf("Actor %s/%s crashed", atespace, name)
	if message := actor.GetStatus().GetCrash().GetMessage(); message != "" {
		cause += ": " + message
	}
	return cause, false, state, nil
}

// revertible reports whether a revert returns the session's crashed Actor to
// a snapshot the conversation goes on from: the external snapshot it holds, the
// last one it suspended to or the template's golden it started from, or, for an
// Actor without one, the snapshot it started from when the session holds no
// task, so its runtime was given nothing to keep.
func (w *ActorWorkflow) revertible(ctx context.Context, session *apiv1alpha1.Session, actor *ateapipb.Actor) (bool, error) {
	if actor.GetStatus().GetExternalSnapshot().GetSnapshotUri() != "" {
		return true, nil
	}
	has, err := w.store.HasSessionTasks(ctx, session.GetId())
	if err != nil {
		return false, fmt.Errorf("check session tasks: %w", err)
	}
	return !has, nil
}

// AwaitRuntime resumes the session's Actor, waiting while Substrate answers
// that it cannot yet (resumeLater): the worker pool rolls, another operation
// holds the Actor, such as a repoint onto a newer template, or Substrate is
// returning the crashed Actor to its snapshot, and each finishes on its own,
// after which the resume goes through. The wait is bounded by busyWait.
//
// Substrate refuses to resume a crashed Actor (FailedPrecondition, the Actor
// still crashed): a sandbox that died under its runtime, a restore whose
// runtime never came up. Such an Actor is reverted (RevertActor) and resumed
// once more when the revert returns it to a snapshot the conversation goes on
// from (revertible): the turn the crash ended is gone, the conversation is
// not. A second crash, or a crashed Actor with nothing to return to, is lost:
// the answer is a RuntimeLossError with the cause for the session to record.
// Any other answer, or an Actor Substrate still defers at the bound, is
// returned as the error it is.
func (w *ActorWorkflow) AwaitRuntime(ctx context.Context, session *apiv1alpha1.Session) error {
	revision, err := w.store.GetRuntimeRevision(ctx, session.GetPreparedRevision())
	if err != nil {
		return fmt.Errorf("load prepared revision: %w", err)
	}
	atespace, name := revision.ActorTemplateAtespace, substrate.ActorName(session.GetId())
	ctx, cancel := context.WithTimeout(ctx, w.busyWait)
	defer cancel()
	for reverted := false; ; reverted = true {
		err = w.awaitResume(ctx, func() error {
			_, err := w.actors.ResumeActor(ctx, atespace, name)
			return err
		})
		if err == nil {
			return nil
		}
		err = fmt.Errorf("resume Actor %s/%s: %w", atespace, name, err)
		if status.Code(err) != codes.FailedPrecondition {
			return err
		}
		actor, getErr := w.actors.GetActor(ctx, atespace, name)
		if getErr != nil || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_CRASHED {
			return err
		}
		crash := actor.GetStatus().GetCrash().GetMessage()
		if !reverted {
			revertible, taskErr := w.revertible(ctx, session, actor)
			if taskErr != nil {
				return errors.Join(err, taskErr)
			}
			if revertible {
				logging.FromContext(ctx).WarnContext(ctx, "the crashed Actor is reverted to its snapshot and resumed again",
					"session_id", session.GetId(), "actor", atespace+"/"+name, "crash", crash)
				if _, revertErr := w.actors.RevertActor(ctx, atespace, name); revertErr != nil {
					return errors.Join(err, fmt.Errorf("revert Actor %s/%s: %w", atespace, name, revertErr))
				}
				continue
			}
		}
		return &RuntimeLossError{
			Cause: fmt.Sprintf("Actor %s/%s crashed and cannot be resumed", atespace, name),
			Err:   fmt.Errorf("%w; crash: %s", err, crash),
		}
	}
}

// Create provisions the persisted session once, using its pinned checkpoint for
// forks. Retries return current state; an uncertain prior creation blocks execution.
func (w *ActorWorkflow) Create(ctx context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	return w.run(ctx, session.GetId(), apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
}

// Suspend returns after the Actor and session are suspended. A retry observes
// the same operation rather than issuing a second mutation.
func (w *ActorWorkflow) Suspend(ctx context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	return w.run(ctx, session.GetId(), apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND)
}

// Resume returns after the Actor is running and the session is ready. Missing
// Actors are errors; Resume never creates replacement compute.
func (w *ActorWorkflow) Resume(ctx context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	return w.run(ctx, session.GetId(), apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME)
}

// Delete closes admission before stopping and deleting compute. It can supersede
// unissued creation, but never deletes a session while a prior call is uncertain.
func (w *ActorWorkflow) Delete(ctx context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	return w.run(ctx, session.GetId(), apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
}

func (w *ActorWorkflow) run(ctx context.Context, sessionID string, requestedKind apiv1alpha1.RuntimeOperation) (*apiv1alpha1.Session, error) {
	ctx, cancelAttempt := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancelAttempt()
	operation, err := w.store.BeginSessionOperation(ctx, sessionID, requestedKind)
	if errors.Is(err, database.ErrConflict) && requestedKind == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE && w.endHeldResume(ctx, sessionID) {
		operation, err = w.store.BeginSessionOperation(ctx, sessionID, requestedKind)
	}
	if err != nil {
		return nil, err
	}
	result, err := w.execute(ctx, operation)
	if err == nil && result.GetState() == apiv1alpha1.RuntimeState_RUNTIME_STATE_READY {
		// A resume can make old unclaimed idle work eligible again.
		w.wakeQuiescence()
	}
	return result, err
}

// endHeldResume ends the session's resume that an earlier attempt left
// pending and no attempt runs now, when its Actor rests suspended where the
// operation found it (resumeUnapplied), and reports whether it did. A resume
// Substrate failed for a transient cause, such as a worker drained under the
// restore, crashes the Actor only until Substrate returns it to rest; its
// attempt finds the Actor crashed and keeps the operation, which would refuse
// the session's delete until another resume settled it.
func (w *ActorWorkflow) endHeldResume(ctx context.Context, sessionID string) bool {
	held, err := w.store.GetCurrentSessionOperation(ctx, sessionID)
	if err != nil || held.Instance.Operation != apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME {
		return false
	}
	revision, err := w.store.GetRuntimeRevision(ctx, held.Instance.GetPreparedRevision())
	if err != nil {
		return false
	}
	executorID := uuid.New()
	claimed, err := w.store.ClaimSessionOperation(ctx, sessionID, held.ID, executorID)
	if err != nil || !claimed {
		return false
	}
	if w.resumeUnapplied(ctx, sessionID, revision) {
		if _, err := w.store.CancelSessionOperation(ctx, sessionID, held.ID, executorID); err == nil {
			return true
		}
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := w.store.ReleaseRuntimeOperation(finishCtx, sessionID, held.ID, executorID); err != nil {
		logging.FromContext(ctx).WarnContext(ctx, "the delete's claim on a held resume was not released; it expires",
			"session_id", sessionID, "operation_id", held.ID, "error", err)
	}
	return false
}

// execute keeps lifecycle preparation separate from the durable issue boundary.
// Multiple callers may prepare using read-only calls; exactly one can authorize
// runtime mutations for a bounded attempt. Errors retain the operation and its
// resource pins so a client retry can continue it, except a failed resume whose
// Actor is found still suspended (resumeUnapplied): its outcome is known, so the
// operation ends and the session takes any next operation. Session admission still
// serializes lifecycle against runtime writes, idle work, and checkpoints.
func (w *ActorWorkflow) execute(ctx context.Context, operation *database.SessionOperation) (_ *apiv1alpha1.Session, err error) {
	sessionID := operation.Instance.Id
	if operation.Instance.Operation == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		return operationOutcome(operation)
	}
	kind := operation.Instance.Operation
	session := operation.Instance
	revision, err := w.store.GetRuntimeRevision(ctx, session.GetPreparedRevision())
	if err != nil {
		return w.failPreparation(ctx, operation, fmt.Errorf("load prepared revision: %w", err))
	}
	var snapshot *database.SessionTaskSnapshot
	var tagName string
	if kind == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE && operation.SourceCheckpointID != nil {
		snapshot, _, err = w.store.GetSessionCheckpointSnapshot(ctx, operation.SourceCheckpointID.String(), session.Creator)
		if err != nil {
			return w.failPreparation(ctx, operation, fmt.Errorf("load pinned checkpoint: %w", err))
		}
		if snapshot == nil || snapshot.URI == "" || snapshot.Atespace == "" || snapshot.ContentScope != "DATA" {
			return w.failPreparation(ctx, operation, fmt.Errorf("fork requires a retained DATA checkpoint"))
		}
		tagName = "checkpoint-" + operation.SourceCheckpointID.String()
	}

	binding := actorBinding(session.Id, revision)
	var creation *substrate.ActorCreation
	if kind == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE {
		policy, err := sessionEgressPolicy(binding.Atespace, revision, session.GetProviders())
		if err != nil {
			return w.failPreparation(ctx, operation, err)
		}
		existing, err := substrate.SessionExistingVolumes(session.Id, session.GetVolumeSource())
		if err != nil {
			return w.failPreparation(ctx, operation, err)
		}
		inherited, err := substrate.ActorEgressPolicy(binding.Atespace, revision.EgressDestinations, revision.Credentials)
		if err != nil {
			return w.failPreparation(ctx, operation, err)
		}
		creation = &substrate.ActorCreation{EgressPolicy: policy, InheritedPolicy: inherited, ExistingVolumes: existing}
		if snapshot != nil {
			creation.Snapshot = &substrate.ActorSnapshot{Tag: &ateapipb.ObjectRef{Atespace: snapshot.Atespace, Name: tagName}, URI: snapshot.URI, ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA}
		}
	}
	// A suspended Actor of a superseded revision resumes on the agent's current
	// one (see repointTarget). An earlier attempt of this operation may have
	// moved the Actor before its record was written: it is then found on the
	// current template and the record follows below.
	var current *database.RuntimeRevision
	var currentPolicy *ateapipb.EgressPolicy
	if kind == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME {
		current, currentPolicy, err = w.repointTarget(ctx, revision, session.GetProviders())
		if err != nil {
			return w.failPreparation(ctx, operation, err)
		}
	}
	if current != nil {
		var actor *ateapipb.Actor
		err := w.untilSettled(ctx, kind, func() (err error) {
			actor, err = w.actors.GetActor(ctx, binding.Atespace, binding.Name)
			return err
		})
		if err != nil {
			return w.failPreparation(ctx, operation, fmt.Errorf("get Actor %s/%s: %w", binding.Atespace, binding.Name, err))
		}
		switch {
		case validActorIdentity(actor, current, binding.Name):
			binding = actorBinding(session.Id, current)
		case actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED:
			current = nil
		}
	}
	prepare := substrate.PrepareActorTransition
	if operation.ExecutorID != uuid.Nil {
		prepare = substrate.PrepareActorRetry
	}
	var transition *substrate.ActorTransition
	err = w.untilSettled(ctx, kind, func() (err error) {
		transition, err = prepare(ctx, w.actors, binding, kind, creation)
		return err
	})
	if err != nil {
		return w.failPreparation(ctx, operation, err)
	}

	if uid := transition.ActorUID(); uid != "" && kind != apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE {
		if _, err := w.store.GetSessionForRuntime(ctx, session.Id, uid); err != nil {
			return w.failPreparation(ctx, operation, fmt.Errorf("verify runtime actor UID: %w", err))
		}
	}

	executorID := uuid.New()
	claimed, err := w.store.ClaimSessionOperation(ctx, sessionID, operation.ID, executorID)
	if err != nil {
		return nil, err
	}
	if !claimed {
		// A superseded generation returns a conflict without runtime work.
		current, err := w.store.GetSessionOperation(ctx, sessionID, operation.ID)
		if err != nil {
			return nil, err
		}
		return operationOutcome(current)
	}
	// recorded is the revision the session records for this operation; the
	// Actor of an unapplied resume is found suspended on its template.
	recorded := revision
	defer func() {
		if err == nil || errors.Is(err, ErrRuntimeLost) {
			return // Completion or the recorded failure already cleared the claim.
		}
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if kind == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME && w.resumeUnapplied(finishCtx, sessionID, recorded) {
			_, cancelErr := w.store.CancelSessionOperation(finishCtx, sessionID, operation.ID, executorID)
			if cancelErr == nil {
				err = fmt.Errorf("lifecycle operation %s ended, the Actor is still suspended: %w", operation.ID, err)
				return
			}
			logging.FromContext(ctx).WarnContext(ctx, "the failed resume keeps its operation: it could not be ended",
				"session_id", sessionID, "operation_id", operation.ID, "error", cancelErr)
		}
		err = errors.Join(fmt.Errorf("lifecycle operation %s remains pending: %w", operation.ID, err),
			w.store.ReleaseRuntimeOperation(finishCtx, sessionID, operation.ID, executorID))
	}()
	if current != nil {
		var repointed *ateapipb.Actor
		err := w.untilSettled(ctx, kind, func() (err error) {
			repointed, err = w.repointActor(ctx, session.Id, current, currentPolicy)
			return err
		})
		if err != nil {
			return nil, err
		}
		if repointed != nil {
			if err := w.store.RepointSessionOperation(ctx, sessionID, operation.ID, executorID, current.Revision); err != nil {
				return nil, err
			}
			recorded = current
			if moved := actorBinding(session.Id, current); moved != binding {
				binding = moved
				err = w.untilSettled(ctx, kind, func() (err error) {
					transition, err = substrate.PrepareActorRetry(ctx, w.actors, binding, kind, creation)
					return err
				})
				if err != nil {
					return nil, err
				}
			}
		}
	}
	if err := w.untilSettled(ctx, kind, func() error { return substrate.ApplyActorTransition(ctx, w.actors, transition) }); err != nil {
		if kind == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME && status.Code(err) == codes.DataLoss {
			// Substrate answers DataLoss when the Actor's snapshot cannot be
			// restored, its own or the golden one it builds on: no retry can
			// resume it, so the outcome is known and the operation must not stay
			// pending, which would refuse every later resume and delete.
			message := fmt.Sprintf("%sresume Actor %s/%s: %v", apia2a.RuntimeLostMessagePrefix, binding.Atespace, binding.Name, status.Convert(err).Message())
			finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			failed, failErr := w.store.FailSessionOperation(finishCtx, sessionID, operation.ID, executorID,
				&apiv1alpha1.Failure{Reason: apia2a.FailureReasonRuntimeLost, Message: message})
			if failErr != nil {
				return nil, errors.Join(fmt.Errorf("resume Actor %s/%s: %w", binding.Atespace, binding.Name, err), failErr)
			}
			return failed, fmt.Errorf("%s: %w", message, ErrRuntimeLost)
		}
		return nil, err
	}
	var authority string
	if kind == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE {
		authority = substrate.ActorHost(binding.Atespace, binding.Name, "")
	}

	// A disconnected client must not discard an already known runtime outcome.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	finished, err := w.store.FinishSessionOperation(finishCtx, sessionID, operation.ID, executorID, authority, transition.ActorUID(), "")
	if err == nil && kind == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE {
		w.releaseDirectory(finishCtx, session)
	}
	return finished, err
}

// untilSettled runs call, one of the operation's ate-api calls, once; a
// resume's call Substrate answers later (resumeLater) is issued again until
// it goes through or the attempt ends. The worker pool or the control plane
// rolling under a session is not the session's failure, and the attempt has
// the time to wait the roll out: while ate-api's pods are replaced, the
// controller's connection answers every call Unavailable until it reaches a
// new one, the reads and the repoint before the resume as much as the resume.
func (w *ActorWorkflow) untilSettled(ctx context.Context, kind apiv1alpha1.RuntimeOperation, call func() error) error {
	if kind != apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME {
		return call()
	}
	return w.awaitResume(ctx, call)
}

// resumeUnapplied reports whether a failed resume left the session's Actor as
// the operation found it: suspended, the session's own, on the template of the
// revision the session records, or gone, so that no resume can take effect.
// The outcome is then known and the operation can end; holding it would refuse
// the session's delete until another resume. An Actor that cannot be read, is
// no longer suspended or has moved keeps the operation for the next attempt to
// settle.
func (w *ActorWorkflow) resumeUnapplied(ctx context.Context, sessionID string, revision *database.RuntimeRevision) bool {
	name := substrate.ActorName(sessionID)
	var actor *ateapipb.Actor
	err := w.awaitResume(ctx, func() (err error) {
		actor, err = w.actors.GetActor(ctx, revision.ActorTemplateAtespace, name)
		return err
	})
	if status.Code(err) == codes.NotFound {
		return true
	}
	if err != nil || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED || !validActorIdentity(actor, revision, name) {
		return false
	}
	_, err = w.store.GetSessionForRuntime(ctx, sessionID, actor.GetMetadata().GetUid())
	return err == nil
}

// releaseDirectory tells the owner of a deleted session's volume that its
// directory may go. The session is deleted whatever the outcome, so a failure
// is logged: the owner also cleans up a directory whose session it no longer
// finds.
func (w *ActorWorkflow) releaseDirectory(ctx context.Context, session *apiv1alpha1.Session) {
	if w.directories == nil || session.GetVolumeSource() == nil {
		return
	}
	if err := w.directories.ReleaseSessionDirectory(ctx, session); err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "the owner of the deleted session's volume was not told its directory may go",
			"session_id", session.GetId(), "error", err)
	}
}

// actorBinding names the session's Actor and the ActorTemplate of revision.
func actorBinding(sessionID string, revision *database.RuntimeRevision) substrate.ActorBinding {
	return substrate.ActorBinding{Atespace: revision.ActorTemplateAtespace, Name: substrate.ActorName(sessionID),
		TemplateAtespace: revision.ActorTemplateAtespace, TemplateName: revision.ActorTemplateName}
}

// repointTarget returns the revision an Actor of revision moves to when it
// resumes, with that revision's egress allowlist plus the session's admitted
// providers, or nil when it stays: a
// revision rendered by an older release lacks configuration the platform needs
// now, and once no session references it the runtime revision GC collects it.
// The target is the agent's current revision in the same atespace; an Actor
// never moves between atespaces.
func (w *ActorWorkflow) repointTarget(ctx context.Context, revision *database.RuntimeRevision, providers []*apiv1alpha1.SessionProvider) (*database.RuntimeRevision, *ateapipb.EgressPolicy, error) {
	current, err := w.store.GetCurrentRuntimeRevision(ctx, revision.Revision)
	switch {
	case errors.Is(err, database.ErrNotFound):
		return nil, nil, nil
	case err != nil:
		return nil, nil, fmt.Errorf("load current revision: %w", err)
	case current.Revision == revision.Revision || current.ActorTemplateAtespace != revision.ActorTemplateAtespace:
		return nil, nil, nil
	}
	policy, err := sessionEgressPolicy(current.ActorTemplateAtespace, current, providers)
	if err != nil {
		return nil, nil, fmt.Errorf("build egress policy of revision %s: %w", current.Revision, err)
	}
	return current, policy, nil
}

// sessionEgressPolicy compiles the egress allowlist of a session's Actor on
// revision. A binding scoped to the golden boot, such as a private skill
// source's git credential, stays off it: the sandbox reaches that host
// without the header. The hosts the session was admitted with for its volume
// source (its providers) are allowed over HTTPS on this Actor alone, so the
// session's directory setup and its git and API calls reach them without the
// Agent listing them, and each gets its caller credential there
// (providerCredentials); the revision and the Agent's other sessions are
// unchanged.
func sessionEgressPolicy(atespace string, revision *database.RuntimeRevision, providers []*apiv1alpha1.SessionProvider) (*ateapipb.EgressPolicy, error) {
	destinations := slices.Clone(revision.EgressDestinations)
	for _, provider := range providers {
		destinations = append(destinations, "https://"+provider.GetHostname())
	}
	credentials, err := egress.CanonicalCredentials(append(egress.ForActors(revision.Credentials), providerCredentials(providers)...))
	if err != nil {
		return nil, fmt.Errorf("the session's provider credentials: %w", err)
	}
	return substrate.ActorEgressPolicy(atespace, destinations, credentials)
}

// providerCredentials binds the caller credential of each Bearer provider: on
// the provider's host, the egress gateway replaces the Authorization header a
// request from the session's Actor carries with the JWT Substrate mints for
// that Actor, bound to the provider's audience. The token is minted at the
// gateway per request and never reaches the sandbox or the controller. The
// provider, or the broker it names, verifies the JWT against Substrate's
// issuer and maps its subject (atespaces:<atespace>:actors:<actor>) to the
// person the session was admitted for: the Actor is the session's own, and
// the session records its creator. A Basic provider gets none yet: the
// gateway prepends the credential verbatim, and a Basic header needs the
// broker's token encoded with the user name, which waits for the gateway's
// token exchange at a named broker.
func providerCredentials(providers []*apiv1alpha1.SessionProvider) []egress.Credential {
	var bindings []egress.Credential
	for _, provider := range providers {
		if provider.GetScheme() != "Bearer" {
			continue
		}
		bindings = append(bindings, egress.Credential{
			Hostname: provider.GetHostname(), Header: "authorization", Prefix: "Bearer ", Audience: provider.GetAudience(),
		})
	}
	return bindings
}

// repointActor moves a suspended Actor onto the ActorTemplate of current and
// gives it that revision's egress allowlist; an Actor already on the template
// keeps it. Substrate refuses, without effect, an Actor that is no longer
// suspended and a template whose sandbox config or durable volumes differ,
// since the Actor's data could not be restored onto it: that returns a nil
// Actor and the Actor stays on its prepared revision.
func (w *ActorWorkflow) repointActor(ctx context.Context, sessionID string, current *database.RuntimeRevision, policy *ateapipb.EgressPolicy) (*ateapipb.Actor, error) {
	atespace, name := current.ActorTemplateAtespace, substrate.ActorName(sessionID)
	actor, err := w.actors.RepointActor(ctx, atespace, name, current.ActorTemplateAtespace, current.ActorTemplateName)
	if status.Code(err) == codes.FailedPrecondition {
		logging.FromContext(ctx).InfoContext(ctx, "the session stays on its prepared revision: Substrate refused its agent's current ActorTemplate",
			"session_id", sessionID, "revision", current.Revision, "reason", status.Convert(err).Message())
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("repoint Actor %s/%s to ActorTemplate %s: %w", atespace, name, current.ActorTemplateName, err)
	}
	if !validActorIdentity(actor, current, name) || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		return nil, fmt.Errorf("repoint Actor %s/%s returned unexpected identity or state", atespace, name)
	}
	if err := w.actors.ReplaceActorEgressPolicy(ctx, atespace, name, policy); err != nil {
		return nil, fmt.Errorf("replace Actor %s/%s egress policy: %w", atespace, name, err)
	}
	return actor, nil
}

// RepointQuiesced moves the quiesced Actor of a READY session onto its agent's
// current revision, before a turn wakes it or because nothing has for a while,
// and returns the session as it then is. The caller keeps every turn of its
// own off the session meanwhile: the gateway holds the turn's dispatch, the
// sweep visits only settled sessions. A session whose Actor is current, live,
// paused, not its own, or refused by Substrate is returned unchanged. Two
// callers may move the same Actor, the gateway before a turn and the sweep of
// superseded revisions: the second finds the Actor moved and records what the
// first has yet to. A lifecycle operation that claimed the session meanwhile
// wins: the Actor goes back to the template of the revision the session
// records.
func (w *ActorWorkflow) RepointQuiesced(ctx context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	if session.GetState() != apiv1alpha1.RuntimeState_RUNTIME_STATE_READY {
		return session, nil
	}
	revision, err := w.store.GetRuntimeRevision(ctx, session.GetPreparedRevision())
	if err != nil {
		return nil, fmt.Errorf("load prepared revision: %w", err)
	}
	current, policy, err := w.repointTarget(ctx, revision, session.GetProviders())
	if err != nil || current == nil {
		return session, err
	}
	atespace, name := revision.ActorTemplateAtespace, substrate.ActorName(session.GetId())
	actor, err := w.actors.GetActor(ctx, atespace, name)
	if err != nil {
		return nil, fmt.Errorf("get Actor %s/%s: %w", atespace, name, err)
	}
	if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED || (!validActorIdentity(actor, revision, name) && !validActorIdentity(actor, current, name)) {
		return session, nil
	}
	if _, err := w.store.GetSessionForRuntime(ctx, session.GetId(), actor.GetMetadata().GetUid()); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return session, nil
		}
		return nil, fmt.Errorf("verify runtime actor UID: %w", err)
	}
	repointed, err := w.repointActor(ctx, session.GetId(), current, policy)
	if err != nil || repointed == nil {
		return session, err
	}
	updated, err := w.store.RepointSession(ctx, session.GetId(), revision.Revision, current.Revision)
	if errors.Is(err, database.ErrConflict) && updated.GetPreparedRevision() == revision.Revision {
		previous, policyErr := sessionEgressPolicy(atespace, revision, session.GetProviders())
		if policyErr == nil {
			_, policyErr = w.repointActor(ctx, session.GetId(), revision, previous)
		}
		return nil, errors.Join(err, policyErr)
	}
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// failPreparation releases only unissued work. If another caller won, observe
// the same generation instead. Completion returns current state; supersession
// returns a conflict. A local preparation error cannot clear a newer operation.
func (w *ActorWorkflow) failPreparation(ctx context.Context, admitted *database.SessionOperation, cause error) (*apiv1alpha1.Session, error) {
	if admitted.ExecutorID != uuid.Nil {
		return nil, cause // An earlier attempt may have issued work; retain its intent.
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err := w.store.FinishSessionOperation(finishCtx, admitted.Instance.Id, admitted.ID, uuid.Nil, "", "", "lifecycle preparation failed")
	if errors.Is(err, database.ErrConflict) {
		operation, readErr := w.store.GetSessionOperation(finishCtx, admitted.Instance.Id, admitted.ID)
		if readErr != nil {
			return nil, errors.Join(cause, err, readErr)
		}
		return operationOutcome(operation)
	}
	return nil, errors.Join(cause, err)
}

func operationOutcome(operation *database.SessionOperation) (*apiv1alpha1.Session, error) {
	if operation.Instance.Operation == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		return operation.Instance, nil
	}
	return nil, fmt.Errorf("lifecycle operation %s is pending; runtime effects may be unresolved: %w", operation.ID, database.ErrConflict)
}

// Verify the recorded actor UID before issuing lifecycle work. Names and
// templates alone also match an externally replaced actor, which we must not
// adopt or checkpoint as this session's runtime.
func (w *ActorWorkflow) verifyActor(ctx context.Context, session *apiv1alpha1.Session, revision *database.RuntimeRevision, actor *ateapipb.Actor) error {
	if !validActorIdentity(actor, revision, substrate.ActorName(session.Id)) {
		return fmt.Errorf("runtime actor identity or template changed")
	}
	if _, err := w.store.GetSessionForRuntime(ctx, session.Id, actor.GetMetadata().GetUid()); err != nil {
		return fmt.Errorf("verify runtime actor UID: %w", err)
	}
	return nil
}

func validActorIdentity(actor *ateapipb.Actor, revision *database.RuntimeRevision, name string) bool {
	metadata, ref := actor.GetMetadata(), actor.GetActorTemplate()
	return metadata.GetName() == name && metadata.GetAtespace() == revision.ActorTemplateAtespace && metadata.GetUid() != "" &&
		ref.GetAtespace() == revision.ActorTemplateAtespace && ref.GetName() == revision.ActorTemplateName
}
