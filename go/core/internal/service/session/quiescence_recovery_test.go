package session

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"go.opentelemetry.io/otel/metric/noop"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

// strandedQuiescence leaves a session in the state a worker leaves behind when
// its suspension ends with an unknown outcome: the idle work is claimed, and
// nothing recorded what became of it.
func strandedQuiescence(t *testing.T, store *lifecycleTestStore, actors *lifecycleTestActors, session *apiv1.Session) *database.SessionQuiescence {
	t.Helper()
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second, noop.NewMeterProvider())
	created, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
	message.ContextID = created.ContextId
	task := a2a.NewSubmittedTask(message, message)
	createHash := sha256.Sum256([]byte("stranded-create"))
	version, err := store.CreateRuntimeTask(t.Context(), created.Id, createHash[:], task, "")
	require.NoError(t, err)
	task.Status.State = a2a.TaskStateInputRequired
	updateHash := sha256.Sum256([]byte("stranded-input-required"))
	version, err = store.UpdateSessionTask(t.Context(), created.Id, version, updateHash[:], task, task, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), created.Id, string(task.ID), version))
	work, err := store.ClaimSessionQuiescence(t.Context())
	require.NoError(t, err)
	return work
}

// The Actor was suspended after all: the worker died, or its deadline fired,
// between Substrate completing the snapshot and the outcome being recorded.
// Reading the Actor settles it, and the snapshot is the one already taken.
func TestReconcileStrandedQuiescenceRecordsACompletedSnapshot(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	strandedQuiescence(t, store, actors, session)
	name := actorKey("team-a", substrate.ActorName(session.Id))
	actors.actors[name].Status = &ateapipb.ActorStatus{
		State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
		Snapshots: []*ateapipb.Snapshot{{
			Generation: 1, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, ActorTemplateUid: "actor-template-uid",
			DurableSnapshot: &ateapipb.SnapshotStorage{State: ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/stranded"}},
		}},
	}

	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second, noop.NewMeterProvider())
	workflow.staleClaim = 0
	require.True(t, workflow.reconcileStaleQuiescence(t.Context()))

	// Finished rather than released: the boundary is settled, and the
	// suspension is not owed to anyone again. The snapshot it recorded is
	// covered where the store's own tests can read it back.
	_, err := store.ClaimStaleSessionQuiescence(t.Context(), 0)
	require.ErrorIs(t, err, database.ErrNotFound)
	_, err = store.ClaimSessionQuiescence(t.Context())
	require.ErrorIs(t, err, database.ErrNotFound)
}

// The suspension never took effect. Holding its claim is what made the session
// unusable: answers, cancellation and deletion were all refused with nothing
// coming to release them.
func TestReconcileStrandedQuiescenceReleasesASuspensionThatNeverHappened(t *testing.T) {
	for _, test := range []struct {
		name  string
		state *ateapipb.ActorStatus
	}{
		{name: "still running", state: &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_RUNNING}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			work := strandedQuiescence(t, store, actors, session)
			actors.actors[actorKey("team-a", substrate.ActorName(session.Id))].Status = test.state

			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second, noop.NewMeterProvider())
			workflow.staleClaim = 0
			require.True(t, workflow.reconcileStaleQuiescence(t.Context()))

			// The session is admitted again, and the suspension is owed to an
			// ordinary worker rather than to the claim that was stranded.
			retried, err := store.ClaimSessionQuiescence(t.Context())
			require.NoError(t, err)
			require.Equal(t, work.Version, retried.Version)
			require.NotEqual(t, work.ExecutorID, retried.ExecutorID)
		})
	}
}

// An Actor that no longer exists cannot be suspended, now or later, and its
// claim only keeps the session from being deleted. Releasing it is not enough:
// the suspension would stay owed, and the next worker would claim it and fail
// against the same missing Actor.
func TestReconcileStrandedQuiescenceRetiresAMissingActor(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	strandedQuiescence(t, store, actors, session)
	delete(actors.actors, actorKey("team-a", substrate.ActorName(session.Id)))

	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second, noop.NewMeterProvider())
	workflow.staleClaim = 0
	require.True(t, workflow.reconcileStaleQuiescence(t.Context()))
	_, err := store.ClaimSessionQuiescence(t.Context())
	require.ErrorIs(t, err, database.ErrNotFound, "the boundary is retired, not owed again")
	_, err = store.ClaimStaleSessionQuiescence(t.Context(), 0)
	require.ErrorIs(t, err, database.ErrNotFound, "and no claim is left holding the session")
}

// Anything that is not plainly settled keeps the claim. Reopening the session
// while a suspension may be in progress is the one thing this must not do.
func TestReconcileStrandedQuiescenceKeepsAnUnsettledClaim(t *testing.T) {
	for _, state := range []ateapipb.ActorState{
		ateapipb.ActorState_ACTOR_STATE_SUSPENDING,
		ateapipb.ActorState_ACTOR_STATE_RESUMING,
		ateapipb.ActorState_ACTOR_STATE_CRASHED,
		ateapipb.ActorState_ACTOR_STATE_DELETING,
	} {
		t.Run(state.String(), func(t *testing.T) {
			store, session := lifecycleFixture(t)
			actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			strandedQuiescence(t, store, actors, session)
			actors.actors[actorKey("team-a", substrate.ActorName(session.Id))].Status = &ateapipb.ActorStatus{State: state}

			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second, noop.NewMeterProvider())
			workflow.staleClaim = 0
			require.True(t, workflow.reconcileStaleQuiescence(t.Context()))
			_, err := store.ClaimSessionQuiescence(t.Context())
			require.ErrorIs(t, err, database.ErrNotFound, "the session stays closed while the outcome is unknown")
		})
	}
}

// A claim is only reconciled once it is old enough that the worker holding it
// cannot still be inside its call.
func TestReconcileStrandedQuiescenceLeavesARecentClaimAlone(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	strandedQuiescence(t, store, actors, session)

	// The default age applies here: the claim was taken a moment ago.
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second, noop.NewMeterProvider())
	require.False(t, workflow.reconcileStaleQuiescence(context.WithoutCancel(t.Context())))
}
