package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
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
	ReleaseSessionQuiescence(context.Context, *database.SessionQuiescence) error
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
	FinishSessionOperation(context.Context, string, uuid.UUID, uuid.UUID, string, string, string) (*apiv1alpha1.Session, error)
	FailSessionOperation(context.Context, string, uuid.UUID, uuid.UUID, *apiv1alpha1.Failure) (*apiv1alpha1.Session, error)
	GetSessionOperation(context.Context, string, uuid.UUID) (*database.SessionOperation, error)
}

type actorClient interface {
	substrate.LifecycleClient
	PauseActor(context.Context, string, string) (*ateapipb.Actor, error)
	RepointActor(context.Context, string, string, string, string) (*ateapipb.Actor, error)
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
	// busyWait bounds how long a refused turn waits for an operation that
	// holds its Actor; busyRetry is the first wait between resumes.
	busyWait  time.Duration
	busyRetry time.Duration
}

// maxBusyRetry caps the wait between resumes of an Actor another operation holds.
const maxBusyRetry = 5 * time.Second

type WorkflowOption func(*ActorWorkflow)

// WithPausedRuntimeTTL bounds how long a runtime paused for input stays on its
// node before the idle worker suspends it durably. Zero keeps every pause in
// place until the reply.
func WithPausedRuntimeTTL(ttl time.Duration) WorkflowOption {
	return func(w *ActorWorkflow) { w.pausedRuntimeTTL = ttl }
}

// quiescenceWake is a shared, single-slot hint channel. Callers must not close it;
// the workflow stops through context cancellation.
func NewActorWorkflow(store workflowStore, actors actorClient, quiescenceWake chan struct{}, recoveryInterval time.Duration, options ...WorkflowOption) *ActorWorkflow {
	workflow := &ActorWorkflow{store: store, actors: actors, recoveryInterval: recoveryInterval, quiescenceWake: quiescenceWake, settleDelay: time.Second, claimLease: 30 * time.Second,
		busyWait: 2 * time.Minute, busyRetry: 500 * time.Millisecond}
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

// RuntimeLost reports whether the session's runtime can no longer take a turn:
// Substrate reports its Actor CRASHED, or the Actor is gone. Substrate crashes
// an Actor it cannot bring back and a crashed Actor never resumes, so every
// later message would fail the way the last one did. The cause names the Actor
// and what became of it. An answer Substrate cannot give is returned as the
// error it is: not knowing is not the same as lost.
func (w *ActorWorkflow) RuntimeLost(ctx context.Context, session *apiv1alpha1.Session) (string, bool, error) {
	revision, err := w.store.GetRuntimeRevision(ctx, session.GetPreparedRevision())
	if err != nil {
		return "", false, fmt.Errorf("load prepared revision: %w", err)
	}
	atespace, name := revision.ActorTemplateAtespace, substrate.ActorName(session.GetId())
	actor, err := w.actors.GetActor(ctx, atespace, name)
	if status.Code(err) == codes.NotFound {
		return fmt.Sprintf("Actor %s/%s not found", atespace, name), true, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get Actor %s/%s: %w", atespace, name, err)
	}
	if actor.GetStatus().GetState() == ateapipb.ActorState_ACTOR_STATE_CRASHED {
		return fmt.Sprintf("Actor %s/%s crashed", atespace, name), true, nil
	}
	return "", false, nil
}

// AwaitRuntime resumes the session's Actor, waiting while Substrate answers
// that another operation holds it (Aborted), such as a repoint onto a newer
// template: that operation finishes on its own, after which the resume goes
// through. The wait is bounded by busyWait; any other answer, or an Actor
// still held at the bound, is returned as the error it is.
func (w *ActorWorkflow) AwaitRuntime(ctx context.Context, session *apiv1alpha1.Session) error {
	revision, err := w.store.GetRuntimeRevision(ctx, session.GetPreparedRevision())
	if err != nil {
		return fmt.Errorf("load prepared revision: %w", err)
	}
	atespace, name := revision.ActorTemplateAtespace, substrate.ActorName(session.GetId())
	ctx, cancel := context.WithTimeout(ctx, w.busyWait)
	defer cancel()
	for delay := w.busyRetry; ; delay = min(2*delay, maxBusyRetry) {
		_, err := w.actors.ResumeActor(ctx, atespace, name)
		if status.Code(err) != codes.Aborted {
			if err != nil {
				return fmt.Errorf("resume Actor %s/%s: %w", atespace, name, err)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("resume Actor %s/%s: still held by another operation after %s: %w", atespace, name, w.busyWait, err)
		case <-time.After(delay):
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

// execute keeps lifecycle preparation separate from the durable issue boundary.
// Multiple callers may prepare using read-only calls; exactly one can authorize
// runtime mutations for a bounded attempt. Errors retain the operation and its
// resource pins so a client retry can continue it. Session admission still
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
		policy, err := substrate.ActorEgressPolicy(binding.Atespace, revision.EgressDestinations, revision.Credentials)
		if err != nil {
			return w.failPreparation(ctx, operation, err)
		}
		creation = &substrate.ActorCreation{EgressPolicy: policy}
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
		current, currentPolicy, err = w.repointTarget(ctx, revision)
		if err != nil {
			return w.failPreparation(ctx, operation, err)
		}
	}
	if current != nil {
		actor, err := w.actors.GetActor(ctx, binding.Atespace, binding.Name)
		if err != nil {
			return w.failPreparation(ctx, operation, fmt.Errorf("get Actor %s/%s: %w", binding.Atespace, binding.Name, err))
		}
		switch {
		case validActorIdentity(actor, current, binding.Name):
			binding, revision = actorBinding(session.Id, current), current
		case actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED:
			current = nil
		}
	}
	prepare := substrate.PrepareActorTransition
	if operation.ExecutorID != uuid.Nil {
		prepare = substrate.PrepareActorRetry
	}
	transition, err := prepare(ctx, w.actors, binding, kind, creation)
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
	defer func() {
		if err == nil {
			return // Completion already cleared the claim.
		}
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, w.store.ReleaseRuntimeOperation(finishCtx, sessionID, operation.ID, executorID))
	}()
	if current != nil {
		repointed, err := w.repointActor(ctx, session.Id, current, currentPolicy)
		if err != nil {
			return nil, fmt.Errorf("lifecycle operation %s remains pending: %w", operation.ID, err)
		}
		if repointed != nil {
			if err := w.store.RepointSessionOperation(ctx, sessionID, operation.ID, executorID, current.Revision); err != nil {
				return nil, fmt.Errorf("lifecycle operation %s remains pending: %w", operation.ID, err)
			}
			if moved := actorBinding(session.Id, current); moved != binding {
				binding, revision = moved, current
				transition, err = substrate.PrepareActorRetry(ctx, w.actors, binding, kind, creation)
				if err != nil {
					return nil, fmt.Errorf("lifecycle operation %s remains pending: %w", operation.ID, err)
				}
			}
		}
	}
	if err := substrate.ApplyActorTransition(ctx, w.actors, transition); err != nil {
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
		return nil, fmt.Errorf("lifecycle operation %s remains pending: %w", operation.ID, err)
	}
	var authority string
	if kind == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE {
		authority = substrate.ActorHost(binding.Atespace, binding.Name, "")
	}

	// A disconnected client must not discard an already known runtime outcome.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return w.store.FinishSessionOperation(finishCtx, sessionID, operation.ID, executorID, authority, transition.ActorUID(), "")
}

// actorBinding names the session's Actor and the ActorTemplate of revision.
func actorBinding(sessionID string, revision *database.RuntimeRevision) substrate.ActorBinding {
	return substrate.ActorBinding{Atespace: revision.ActorTemplateAtespace, Name: substrate.ActorName(sessionID),
		TemplateAtespace: revision.ActorTemplateAtespace, TemplateName: revision.ActorTemplateName}
}

// repointTarget returns the revision an Actor of revision moves to when it
// resumes, with that revision's egress allowlist, or nil when it stays: a
// revision rendered by an older release lacks configuration the platform needs
// now, and once no session references it the runtime revision GC collects it.
// The target is the agent's current revision in the same atespace; an Actor
// never moves between atespaces.
func (w *ActorWorkflow) repointTarget(ctx context.Context, revision *database.RuntimeRevision) (*database.RuntimeRevision, *ateapipb.EgressPolicy, error) {
	current, err := w.store.GetCurrentRuntimeRevision(ctx, revision.Revision)
	switch {
	case errors.Is(err, database.ErrNotFound):
		return nil, nil, nil
	case err != nil:
		return nil, nil, fmt.Errorf("load current revision: %w", err)
	case current.Revision == revision.Revision || current.ActorTemplateAtespace != revision.ActorTemplateAtespace:
		return nil, nil, nil
	}
	policy, err := substrate.ActorEgressPolicy(current.ActorTemplateAtespace, current.EgressDestinations, current.Credentials)
	if err != nil {
		return nil, nil, fmt.Errorf("build egress policy of revision %s: %w", current.Revision, err)
	}
	return current, policy, nil
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
	current, policy, err := w.repointTarget(ctx, revision)
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
		previous, policyErr := substrate.ActorEgressPolicy(atespace, revision.EgressDestinations, revision.Credentials)
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
