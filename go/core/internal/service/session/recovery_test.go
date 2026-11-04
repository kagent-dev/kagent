package session

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

func (a *lifecycleTestActors) RevertActor(_ context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	actor := a.actors[actorKey(atespace, name)]
	if actor.Status.State != ateapipb.ActorState_ACTOR_STATE_CRASHED && actor.Status.State != ateapipb.ActorState_ACTOR_STATE_REVERTING {
		return nil, status.Error(codes.FailedPrecondition, "actor is not crashed or reverting")
	}
	actor.Status.State = ateapipb.ActorState_ACTOR_STATE_SUSPENDED
	actor.Status.Crash = nil
	snapshot := proto.CloneOf(substrate.LatestDurableActorSnapshot(actor))
	actor.Status.Snapshots = nil
	if snapshot != nil {
		snapshot.Locality = ""
		actor.Status.Snapshots = []*ateapipb.Snapshot{snapshot}
	}
	return proto.CloneOf(actor), nil
}

type recoveryTestActors struct {
	*lifecycleTestActors
	revert  func(context.Context, string, string) (*ateapipb.Actor, error)
	readErr error
	// afterRead runs once, after the first successful read returns its copy.
	afterRead func(context.Context)
}

func (a *recoveryTestActors) RevertActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	return a.revert(ctx, atespace, name)
}

func (a *recoveryTestActors) GetActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	if a.readErr != nil {
		return nil, a.readErr
	}
	actor, err := a.lifecycleTestActors.GetActor(ctx, atespace, name)
	if hook := a.afterRead; hook != nil && err == nil {
		a.afterRead = nil
		hook(ctx)
	}
	return actor, err
}

func revertGeneration(t *testing.T, store *lifecycleTestStore, sessionID string) uuid.UUID {
	t.Helper()
	generation, err := store.SessionLifecycleGeneration(t.Context(), sessionID)
	require.NoError(t, err)
	return generation
}

func crashedTurnFixture(t *testing.T) (*lifecycleTestStore, *lifecycleTestActors, *apiv1.Session, *a2a.Task, int64) {
	t.Helper()
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	var err error
	session, err = NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second).Create(t.Context(), session)
	require.NoError(t, err)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("original input"))
	message.ContextID = session.ContextId
	task := a2a.NewSubmittedTask(message, message)
	task.Status.State = a2a.TaskStateWorking
	hash := sha256.Sum256([]byte("working"))
	version, err := store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, "")
	require.NoError(t, err)
	actors.actors[actorKey("team-a", substrate.ActorName(session.Id))].Status.State = ateapipb.ActorState_ACTOR_STATE_CRASHED
	return store, actors, session, task, version
}

func TestCrashedSessionRecoveryRetriesWithoutLosingState(t *testing.T) {
	for _, test := range []struct {
		name         string
		lostResponse bool
		wantReverts  int
	}{
		{name: "revert failed", wantReverts: 2},
		{name: "revert response lost", lostResponse: true, wantReverts: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, base, session, task, _ := crashedTurnFixture(t)
			calls := 0
			actors := &recoveryTestActors{lifecycleTestActors: base}
			actors.revert = func(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
				calls++
				if calls == 1 {
					if test.lostResponse {
						_, err := base.RevertActor(ctx, atespace, name)
						require.NoError(t, err)
					}
					return nil, status.Error(codes.Unavailable, "response unavailable")
				}
				return base.RevertActor(ctx, atespace, name)
			}
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
			_, err := workflow.Revert(t.Context(), session)
			require.Error(t, err)
			failed, err := store.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_FAILED, failed.State)
			require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME, failed.Operation)
			require.Equal(t, database.SessionRuntimeCrashed, failed.Failure.Reason)
			interrupted, interruptedVersion, err := store.GetVersionedSessionTask(t.Context(), session.Id, string(task.ID))
			require.NoError(t, err)
			require.Equal(t, a2a.TaskStateFailed, interrupted.Status.State)
			published, err := store.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
			require.NoError(t, err)
			require.Equal(t, a2a.TaskStateFailed, published.Status.State)
			require.Len(t, published.History, 1)
			require.ErrorIs(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), uuid.NewString()), database.ErrConflict)
			// A new workflow instance discovers the admitted operation from PostgreSQL.
			restarted := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
			_, err = restarted.recoverSessionPage(t.Context(), "")
			require.NoError(t, err)
			ready, err := store.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, ready.State)
			require.Nil(t, ready.Failure)
			stored, err := store.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
			require.NoError(t, err)
			require.Equal(t, a2a.TaskStateFailed, stored.Status.State)
			require.Equal(t, test.wantReverts, calls)
			require.Len(t, stored.History, 1, "the original input remains archived once")
			_, recoveredVersion, err := store.GetVersionedSessionTask(t.Context(), session.Id, string(task.ID))
			require.NoError(t, err)
			require.Equal(t, interruptedVersion, recoveredVersion, "retries do not append another terminal event")
			_, err = restarted.Revert(t.Context(), ready)
			require.ErrorIs(t, err, database.ErrFailedPrecondition)
			require.Equal(t, test.wantReverts, calls, "completed recovery cannot discard another execution")
		})
	}
}

func TestCrashedSessionRecoveryFencesConcurrentWork(t *testing.T) {
	store, base, session, task, version := crashedTurnFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	actors := &recoveryTestActors{lifecycleTestActors: base}
	actors.revert = func(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
		close(entered)
		select {
		case <-release:
			return base.RevertActor(ctx, atespace, name)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
	done := make(chan error, 1)
	go func() { _, err := workflow.Revert(ctx, session); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("recovery was not admitted")
	}
	failed, err := store.GetSessionByID(ctx, session.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_FAILED, failed.State)
	_, err = workflow.Revert(ctx, failed)
	require.ErrorIs(t, err, database.ErrConflict)
	hash := sha256.Sum256([]byte("late write"))
	_, err = store.UpdateSessionTask(ctx, session.Id, version, hash[:], task, task, "")
	require.ErrorIs(t, err, database.ErrConflict)
	close(release)
	require.NoError(t, <-done)
	_, err = store.UpdateSessionTask(ctx, session.Id, version, hash[:], task, task, "")
	require.ErrorIs(t, err, database.ErrConflict)
}

func TestSessionRecoveryRequiresConfirmedRecordedActorCrash(t *testing.T) {
	for _, test := range []struct {
		name     string
		state    ateapipb.ActorState
		uid      string
		template string
		readErr  error
		// want is the refusal a client sees; nil means the error must stay
		// retryable, so it may match neither refusal.
		want error
	}{
		{name: "quiet running actor", state: ateapipb.ActorState_ACTOR_STATE_RUNNING, want: errRuntimeNotCrashed},
		{name: "transient read failure", readErr: status.Error(codes.Unavailable, "unreachable")},
		{name: "missing actor", readErr: status.Error(codes.NotFound, "actor not found"), want: errRuntimeUnrecoverable},
		{name: "same-name replacement", state: ateapipb.ActorState_ACTOR_STATE_CRASHED, uid: "another-actor", want: errRuntimeUnrecoverable},
		{name: "template changed", state: ateapipb.ActorState_ACTOR_STATE_CRASHED, template: "another-template", want: errRuntimeUnrecoverable},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, base, session, task, _ := crashedTurnFixture(t)
			actor := base.actors[actorKey("team-a", substrate.ActorName(session.Id))]
			actor.Status.State = test.state
			if test.uid != "" {
				actor.Metadata.Uid = test.uid
			}
			if test.template != "" {
				actor.ActorTemplate.Name = test.template
			}
			actors := &recoveryTestActors{lifecycleTestActors: base, readErr: test.readErr}
			actors.revert = func(context.Context, string, string) (*ateapipb.Actor, error) {
				t.Fatal("unconfirmed crash issued a revert")
				return nil, nil
			}
			_, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second).Revert(t.Context(), session)
			require.Error(t, err)
			if test.want != nil {
				require.ErrorIs(t, err, test.want)
				require.ErrorIs(t, err, database.ErrFailedPrecondition)
			} else {
				require.NotErrorIs(t, err, database.ErrFailedPrecondition, "a transient failure must stay retryable")
			}
			current, err := store.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.State)
			require.Nil(t, current.Failure)
			stored, err := store.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
			require.NoError(t, err)
			require.Equal(t, a2a.TaskStateWorking, stored.Status.State)
		})
	}
}

// Only an active lifecycle operation is contention a retry waits out. A Session
// at rest outside READY, such as one suspended by deadline cleanup, would
// return ABORTED forever if it were reported as contention.
func TestExplicitRevertSeparatesContentionFromRestingState(t *testing.T) {
	for _, test := range []struct {
		name      string
		state     apiv1.RuntimeState
		operation apiv1.RuntimeOperation
		want      error
	}{
		{name: "suspended by deadline cleanup", state: apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED, operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE, want: errRuntimeNotCrashed},
		{name: "suspension in progress", state: apiv1.RuntimeState_RUNTIME_STATE_READY, operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND, want: database.ErrConflict},
		{name: "resume in progress", state: apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED, operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME, want: database.ErrConflict},
		{name: "creation in progress", state: apiv1.RuntimeState_RUNTIME_STATE_CREATING, operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_CREATE, want: database.ErrConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Both checks run on the Session the caller read, before any store
			// or Substrate access, so neither dependency is needed.
			workflow := NewActorWorkflow(nil, nil, make(chan struct{}, 1), time.Second)
			_, err := workflow.Revert(t.Context(), &apiv1.Session{Id: uuid.NewString(), State: test.state, Operation: test.operation})
			require.ErrorIs(t, err, test.want)
			if test.want == database.ErrConflict {
				require.NotErrorIs(t, err, database.ErrFailedPrecondition)
			}
		})
	}
}

func TestSessionRecoveryCandidatePolicy(t *testing.T) {
	store, actors, active, _, _ := crashedTurnFixture(t)
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
	for _, state := range []a2a.TaskState{"", a2a.TaskStateInputRequired, a2a.TaskStateAuthRequired, a2a.TaskStateWorking} {
		session, _, err := store.CreateSession(t.Context(), &apiv1.Session{Id: uuid.NewString(), Creator: "alice", Agent: active.Agent}, uuid.NewString())
		require.NoError(t, err)
		session, err = workflow.Create(t.Context(), session)
		require.NoError(t, err)
		if state == "" {
			continue
		}
		message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("input"))
		message.ContextID = session.ContextId
		task := a2a.NewSubmittedTask(message, message)
		task.Status.State = state
		hash := sha256.Sum256([]byte(task.ID))
		version, err := store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, "")
		require.NoError(t, err)
		if state != a2a.TaskStateWorking {
			require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
		}
	}
	page, err := store.ListSessionRecoveryCandidates(t.Context(), "", 100)
	require.NoError(t, err)
	require.Empty(t, page, "manual recovery does not inspect idle or active runtimes")
	actor, err := actors.GetActor(t.Context(), "team-a", substrate.ActorName(active.Id))
	require.NoError(t, err)
	_, err = store.BeginCrashedSessionRecovery(t.Context(), active.Id, revertGeneration(t, store, active.Id), actor.GetMetadata().GetUid(), database.SessionTaskSnapshot{})
	require.NoError(t, err)
	page, err = store.ListSessionRecoveryCandidates(t.Context(), "", 100)
	require.NoError(t, err)
	require.Len(t, page, 1, "explicitly admitted recovery remains eligible for retry")
	require.Equal(t, active.Id, page[0].Id)
}

func TestCrashedSessionRecoversAnOlderResumedTask(t *testing.T) {
	store, actors, session, older, version := crashedTurnFixture(t)
	older.Status.State = a2a.TaskStateInputRequired
	hash := sha256.Sum256([]byte("waiting"))
	waitingVersion, err := store.UpdateSessionTask(t.Context(), session.Id, version, hash[:], older, older, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(older.ID), waitingVersion))
	work, err := store.ClaimSessionQuiescence(t.Context())
	require.NoError(t, err)
	require.NoError(t, store.FinishSessionQuiescence(t.Context(), work, &database.SessionTaskSnapshot{
		Atespace: "team-a", URI: "s3://snapshots/waiting-turn", Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES.String(),
	}))

	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("another turn"))
	message.ContextID = session.ContextId
	newer := a2a.NewSubmittedTask(message, message)
	newer.Status.State = a2a.TaskStateCompleted
	hash = sha256.Sum256([]byte("newer completed task"))
	newerVersion, err := store.CreateRuntimeTask(t.Context(), session.Id, hash[:], newer, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(newer.ID), newerVersion))
	work, err = store.ClaimSessionQuiescence(t.Context())
	require.NoError(t, err)
	require.NoError(t, store.FinishSessionQuiescence(t.Context(), work, &database.SessionTaskSnapshot{
		Atespace: "team-a", URI: "s3://snapshots/newer-completed-turn", Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES.String(),
	}))

	older.Status.State = a2a.TaskStateWorking
	hash = sha256.Sum256([]byte("resume older task"))
	_, err = store.UpdateSessionTask(t.Context(), session.Id, waitingVersion, hash[:], older, older, "")
	require.NoError(t, err)
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
	_, err = workflow.Revert(t.Context(), session)
	require.NoError(t, err)
	failed, err := store.GetSettledSessionTask(t.Context(), session.Id, string(older.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateFailed, failed.Status.State)
	retained, retainedVersion, err := store.GetVersionedSessionTask(t.Context(), session.Id, string(newer.ID))
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, retained.Status.State)
	require.Equal(t, newerVersion, retainedVersion, "recovery cannot rewrite a later completed task")
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), uuid.NewString()))
}

func TestExplicitRevertRespectsCheckpointCreation(t *testing.T) {
	store, actors, session, task, version := crashedTurnFixture(t)
	task.Status.State = a2a.TaskStateCompleted
	hash := sha256.Sum256([]byte("completed"))
	version, err := store.UpdateSessionTask(t.Context(), session.Id, version, hash[:], task, task, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
	work, err := store.ClaimSessionQuiescence(t.Context())
	require.NoError(t, err)
	require.NoError(t, store.FinishSessionQuiescence(t.Context(), work, &database.SessionTaskSnapshot{
		Atespace: "team-a", URI: "s3://snapshots/completed", Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES.String(),
	}))
	_, _, err = store.ReserveSessionCheckpoint(t.Context(), &apiv1.Checkpoint{
		Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID),
	}, session.Creator, uuid.NewString())
	require.NoError(t, err)
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute)
	_, err = workflow.Revert(t.Context(), session)
	require.ErrorIs(t, err, database.ErrConflict)
	actor, err := actors.GetActor(t.Context(), "team-a", substrate.ActorName(session.Id))
	require.NoError(t, err)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_CRASHED, actor.Status.State)
	stored, err := store.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, stored.Status.State)
}

// The Actor crashed during post-turn suspension, and the worker kept its claim
// because it cannot tell whether the suspension took effect.
func TestExplicitRevertRetiresIdleSuspensionOfCrashedActor(t *testing.T) {
	store, actors, session, task, version := crashedTurnFixture(t)
	task.Status.State = a2a.TaskStateCompleted
	hash := sha256.Sum256([]byte("completed"))
	version, err := store.UpdateSessionTask(t.Context(), session.Id, version, hash[:], task, task, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
	work, err := store.ClaimSessionQuiescence(t.Context())
	require.NoError(t, err)
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute)
	recovered, err := workflow.Revert(t.Context(), session)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, recovered.State)
	actor, err := actors.GetActor(t.Context(), "team-a", substrate.ActorName(session.Id))
	require.NoError(t, err)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, actor.Status.State)
	stored, err := store.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, stored.Status.State)
	require.NoError(t, store.FinishSessionQuiescence(t.Context(), work, &database.SessionTaskSnapshot{
		Atespace: "team-a", URI: "s3://snapshots/late", Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES.String(),
	}), "a late finish of the retired claim is a no-op")
	_, err = store.ClaimSessionQuiescence(t.Context())
	require.ErrorIs(t, err, database.ErrNotFound)
}

// Revert A observes the crash, then pauses while Revert B restores the same Actor
// and ingress wakes it for a new turn. A then sees the same Actor UID and a READY
// Session, so only the lifecycle generation shows that its observation is stale.
func TestExplicitRevertRejectsCrashObservedBeforeAnotherRevert(t *testing.T) {
	store, base, session, _, _ := crashedTurnFixture(t)
	key := actorKey("team-a", substrate.ActorName(session.Id))
	var next *a2a.Task
	paused := &recoveryTestActors{lifecycleTestActors: base, revert: base.RevertActor, afterRead: func(ctx context.Context) {
		recovered, err := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Minute).Revert(ctx, session)
		require.NoError(t, err)
		require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, recovered.State)
		base.mu.Lock()
		base.actors[key].Status.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
		base.mu.Unlock()
		message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("next input"))
		message.ContextID = session.ContextId
		next = a2a.NewSubmittedTask(message, message)
		next.Status.State = a2a.TaskStateWorking
		hash := sha256.Sum256([]byte("next"))
		_, err = store.CreateRuntimeTask(ctx, session.Id, hash[:], next, "")
		require.NoError(t, err)
	}}
	_, err := NewActorWorkflow(store, paused, make(chan struct{}, 1), time.Minute).Revert(t.Context(), session)
	require.ErrorIs(t, err, database.ErrConflict)
	current, err := store.GetSessionByID(t.Context(), session.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.State)
	require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE, current.Operation)
	running, _, err := store.GetVersionedSessionTask(t.Context(), session.Id, string(next.ID))
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateWorking, running.Status.State, "the stale revert must not fail the new turn")
}

func TestExplicitRevertCannotCheckpointDiscardedTerminalState(t *testing.T) {
	store, actors, session, task, version := crashedTurnFixture(t)
	task.Status.State = a2a.TaskStateCompleted
	hash := sha256.Sum256([]byte("completed before snapshot"))
	version, err := store.UpdateSessionTask(t.Context(), session.Id, version, hash[:], task, task, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute)
	_, err = workflow.Revert(t.Context(), session)
	require.NoError(t, err)
	_, err = store.ClaimSessionQuiescence(t.Context())
	require.ErrorIs(t, err, database.ErrNotFound, "the reverted snapshot cannot represent discarded terminal state")
	_, _, err = store.ReserveSessionCheckpoint(t.Context(), &apiv1.Checkpoint{
		Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID),
	}, session.Creator, uuid.NewString())
	require.ErrorIs(t, err, database.ErrSnapshotPending)
	stored, err := store.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, stored.Status.State)
}

func TestExplicitRevertPreservesOnlyCapturedWaits(t *testing.T) {
	for _, state := range []a2a.TaskState{a2a.TaskStateInputRequired, a2a.TaskStateAuthRequired} {
		for _, captured := range []bool{false, true} {
			t.Run(fmt.Sprint(state, "/captured=", captured), func(t *testing.T) {
				store, actors, session, task, version := crashedTurnFixture(t)
				task.Status.State = state
				hash := sha256.Sum256([]byte(state))
				version, err := store.UpdateSessionTask(t.Context(), session.Id, version, hash[:], task, task, "")
				require.NoError(t, err)
				require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
				if captured {
					work, err := store.ClaimSessionQuiescence(t.Context())
					require.NoError(t, err)
					require.NoError(t, store.FinishSessionQuiescence(t.Context(), work, &database.SessionTaskSnapshot{
						Atespace: "team-a", URI: "s3://snapshots/waiting", Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES.String(),
					}))
					actors.actors[actorKey("team-a", substrate.ActorName(session.Id))].Status.Snapshots = []*ateapipb.Snapshot{{Generation: 1, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, DurableSnapshot: &ateapipb.SnapshotStorage{State: ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/waiting"}}}}
					actor := actors.actors[actorKey("team-a", substrate.ActorName(session.Id))]
					actor.Status.Snapshots[0].Locality = "lost-node"
					actor.Status.Snapshots = append(actor.Status.Snapshots, &ateapipb.Snapshot{
						Generation: 2, DurableSnapshot: &ateapipb.SnapshotStorage{State: ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_PENDING},
					})
				}
				_, err = NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute).Revert(t.Context(), session)
				require.NoError(t, err)
				stored, err := store.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
				require.NoError(t, err)
				want := a2a.TaskStateFailed
				if captured {
					want = state
				}
				require.Equal(t, want, stored.Status.State)
			})
		}
	}
}

func TestExplicitRevertRetainsWaitCapturedByLaterTurn(t *testing.T) {
	store, actors, session, waiting, version := crashedTurnFixture(t)
	waiting.Status.State = a2a.TaskStateInputRequired
	hash := sha256.Sum256([]byte("waiting"))
	version, err := store.UpdateSessionTask(t.Context(), session.Id, version, hash[:], waiting, waiting, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(waiting.ID), version))
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("another turn"))
	message.ContextID = session.ContextId
	later := a2a.NewSubmittedTask(message, message)
	later.Status.State = a2a.TaskStateCompleted
	hash = sha256.Sum256([]byte("later completed turn"))
	version, err = store.CreateRuntimeTask(t.Context(), session.Id, hash[:], later, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(later.ID), version))
	work, err := store.ClaimSessionQuiescence(t.Context())
	require.NoError(t, err)
	require.NoError(t, store.FinishSessionQuiescence(t.Context(), work, &database.SessionTaskSnapshot{
		Atespace: "team-a", URI: "s3://snapshots/later-turn", Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES.String(),
	}))
	actors.actors[actorKey("team-a", substrate.ActorName(session.Id))].Status.Snapshots = []*ateapipb.Snapshot{{Generation: 1, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, DurableSnapshot: &ateapipb.SnapshotStorage{State: ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/later-turn"}}}}
	_, err = NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute).Revert(t.Context(), session)
	require.NoError(t, err)
	retained, err := store.GetSettledSessionTask(t.Context(), session.Id, string(waiting.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateInputRequired, retained.Status.State)
	retained, err = store.GetSettledSessionTask(t.Context(), session.Id, string(later.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, retained.Status.State)
}

func TestSessionRecoveryPagesPastUnavailableActors(t *testing.T) {
	store, base, first, _, _ := crashedTurnFixture(t)
	actors := &recoveryTestActors{lifecycleTestActors: base, revert: base.RevertActor, readErr: status.Error(codes.Unavailable, "control plane unavailable")}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute)
	ids := []string{first.Id}
	for range 100 {
		session, _, err := store.CreateSession(t.Context(), &apiv1.Session{
			Id: uuid.NewString(), Creator: first.Creator, Agent: first.Agent,
		}, uuid.NewString())
		require.NoError(t, err)
		session, err = NewActorWorkflow(store, base, make(chan struct{}, 1), time.Minute).Create(t.Context(), session)
		require.NoError(t, err)
		message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("accepted turn"))
		message.ContextID = session.ContextId
		task := a2a.NewSubmittedTask(message, message)
		task.Status.State = a2a.TaskStateWorking
		hash := sha256.Sum256([]byte(task.ID))
		_, err = store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, "")
		require.NoError(t, err)
		base.actors[actorKey("team-a", substrate.ActorName(session.Id))].Status.State = ateapipb.ActorState_ACTOR_STATE_CRASHED
		ids = append(ids, session.Id)
	}
	for _, id := range ids {
		actor, err := base.GetActor(t.Context(), "team-a", substrate.ActorName(id))
		require.NoError(t, err)
		_, err = store.BeginCrashedSessionRecovery(t.Context(), id, revertGeneration(t, store, id), actor.GetMetadata().GetUid(), database.SessionTaskSnapshot{})
		require.NoError(t, err)
	}
	slices.Sort(ids)
	after, err := workflow.recoverSessionPage(t.Context(), "")
	require.NoError(t, err)
	require.Equal(t, ids[99], after, "failed Actor reads still advance the bounded page")
	actors.readErr = nil
	after, err = workflow.recoverSessionPage(t.Context(), after)
	require.NoError(t, err)
	require.Empty(t, after, "the short second page resets the cursor")
	last, err := base.GetActor(t.Context(), "team-a", substrate.ActorName(ids[100]))
	require.NoError(t, err)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, last.GetStatus().GetState(), "the second page recovers the 101st candidate")
	firstActor, err := base.GetActor(t.Context(), "team-a", substrate.ActorName(ids[0]))
	require.NoError(t, err)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_CRASHED, firstActor.GetStatus().GetState(), "unavailable first-page Actors still need recovery")
	after, err = workflow.recoverSessionPage(t.Context(), after)
	require.NoError(t, err)
	require.Equal(t, ids[99], after, "the wrapped scan revisits previously unavailable Actors")
	after, err = workflow.recoverSessionPage(t.Context(), after)
	require.NoError(t, err)
	require.Empty(t, after, "an empty page resets the cursor")
	for _, id := range ids {
		actor, err := base.GetActor(t.Context(), "team-a", substrate.ActorName(id))
		require.NoError(t, err)
		require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, actor.GetStatus().GetState())
	}
}

// blockedSuspendActors holds every idle suspension in runtime I/O until the
// test ends, the way a slow Substrate upload would.
type blockedSuspendActors struct {
	*lifecycleTestActors
	entered atomic.Int32
}

func (a *blockedSuspendActors) SuspendActor(ctx context.Context, _, _ string) (*ateapipb.Actor, error) {
	a.entered.Add(1)
	<-ctx.Done()
	return nil, ctx.Err()
}

// Start retries an admitted recovery on its own ticker. Every quiescence worker
// is blocked in an idle suspension for the whole test, so a scan that shared
// those workers would never run.
func TestStartRetriesAdmittedRecoveryWhileIdleWorkBlocks(t *testing.T) {
	store, base, crashed, _, _ := crashedTurnFixture(t)
	unavailable := &recoveryTestActors{lifecycleTestActors: base, revert: func(context.Context, string, string) (*ateapipb.Actor, error) {
		return nil, status.Error(codes.Unavailable, "control plane unavailable")
	}}
	_, err := NewActorWorkflow(store, unavailable, make(chan struct{}, 1), time.Hour).Revert(t.Context(), crashed)
	require.Error(t, err, "the first attempt leaves an admitted recovery for Start to retry")

	const idleSessions = 4 // one per quiescence worker
	for range idleSessions {
		idle, _, err := store.CreateSession(t.Context(), &apiv1.Session{Id: uuid.NewString(), Creator: crashed.Creator, Agent: crashed.Agent}, uuid.NewString())
		require.NoError(t, err)
		idle, err = NewActorWorkflow(store, base, make(chan struct{}, 1), time.Hour).Create(t.Context(), idle)
		require.NoError(t, err)
		request := stageQuiescenceTask(t, store.Client, idle, a2a.TaskStateCompleted)
		require.NoError(t, store.SettleSessionTask(t.Context(), idle.Id, request.TaskId, request.Version))
	}

	actors := &blockedSuspendActors{lifecycleTestActors: base}
	startQuiescenceWorker(t, NewActorWorkflow(store, actors, make(chan struct{}, 1), 20*time.Millisecond))
	require.Eventually(t, func() bool { return actors.entered.Load() == idleSessions }, 5*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		current, err := store.GetSessionByID(t.Context(), crashed.Id)
		return err == nil && current.State == apiv1.RuntimeState_RUNTIME_STATE_READY
	}, 5*time.Second, 10*time.Millisecond)
	actor, err := base.GetActor(t.Context(), "team-a", substrate.ActorName(crashed.Id))
	require.NoError(t, err)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, actor.GetStatus().GetState())
	require.EqualValues(t, idleSessions, actors.entered.Load(), "idle work stayed blocked while recovery completed")
}

// A recovery whose Actor cannot be read has issued nothing, so it must not take
// a claim that would make Delete refuse the Session.
func TestDeleteSupersedesUnissuedCrashRecovery(t *testing.T) {
	for _, test := range []struct {
		name    string
		gone    bool
		readErr error
	}{
		{name: "actor read fails", readErr: status.Error(codes.Unavailable, "control plane unavailable")},
		{name: "actor is gone", gone: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, base, session, _, _ := crashedTurnFixture(t)
			key := actorKey("team-a", substrate.ActorName(session.Id))
			_, err := store.BeginCrashedSessionRecovery(t.Context(), session.Id, revertGeneration(t, store, session.Id), base.actors[key].GetMetadata().GetUid(), database.SessionTaskSnapshot{})
			require.NoError(t, err)
			if test.gone {
				delete(base.actors, key)
			}
			actors := &recoveryTestActors{lifecycleTestActors: base, readErr: test.readErr}
			actors.revert = func(context.Context, string, string) (*ateapipb.Actor, error) {
				t.Fatal("an unread Actor was reverted")
				return nil, nil
			}
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute)
			admitted, err := store.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			_, err = workflow.Revert(t.Context(), admitted)
			require.Error(t, err)
			_, err = workflow.recoverSessionPage(t.Context(), "")
			require.NoError(t, err)

			pending, err := store.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_FAILED, pending.State)
			require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME, pending.Operation, "a failed read keeps the recovery for the next scan")
			deleted, err := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Minute).Delete(t.Context(), pending)
			require.NoError(t, err)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETED, deleted.State)
			_, err = base.GetActor(t.Context(), "team-a", substrate.ActorName(session.Id))
			require.Equal(t, codes.NotFound, status.Code(err))
			page, err := store.ListSessionRecoveryCandidates(t.Context(), "", 100)
			require.NoError(t, err)
			require.Empty(t, page)
		})
	}
}
