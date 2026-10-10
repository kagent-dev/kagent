package session

import (
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Deleting an Agent retires its definition, which is all the reconciler does;
// the sweep then deletes the Agent's sessions, READY runtimes included, through
// the ordinary delete workflow, so the revision they pinned becomes unreferenced
// and the runtime revision GC collects its ActorTemplate.
func TestSessionsOfDeletedAgentAreDeleted(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
	session, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, session.State)
	// A live Actor, as after a turn: the delete suspends it first.
	_, err = actors.ResumeActor(t.Context(), store.revision.ActorTemplateAtespace, substrate.ActorName(session.Id))
	require.NoError(t, err)
	// Idle deletion off: the Agent's deletion alone ends the session.
	worker, err := NewExpirationWorker(store, workflow, 0, time.Minute)
	require.NoError(t, err)

	listed := func() []string {
		t.Helper()
		ids, err := store.ListSessionsOfDeletedAgents(t.Context(), "", 100)
		require.NoError(t, err)
		return ids
	}
	require.Empty(t, listed(), "a session of an active Agent stays")
	worker.sweepDeletedAgents(t.Context())
	_, err = store.GetSessionByID(t.Context(), session.Id)
	require.NoError(t, err)

	require.NoError(t, store.RetireAgentIdentities(t.Context(), "team-a", "assistant", nil))
	require.Equal(t, []string{session.Id}, listed())
	worker.sweepDeletedAgents(t.Context())
	_, err = store.GetSessionByID(t.Context(), session.Id)
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Empty(t, actors.actors, "the session's Actor is deleted")
	unreferenced, err := store.ListUnreferencedRuntimeRevisions(t.Context())
	require.NoError(t, err)
	require.Len(t, unreferenced, 1, "nothing references the revision any more: the GC collects its ActorTemplate")
	require.Equal(t, store.revision.Revision, unreferenced[0].Revision)
	require.Empty(t, listed())
}

// An Agent deleted and created again under its name is a new identity; the
// sessions of the old one keep their pinned revision until they idle out.
func TestSessionsOfReplacedAgentStay(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
	session, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)
	require.NoError(t, store.RetireAgentIdentities(t.Context(), "team-a", "assistant", nil))
	require.NoError(t, store.UpsertAgentDefinition(t.Context(), database.AgentDefinition{Namespace: "team-a", AgentName: "assistant", AgentUID: "template-uid-2", DesiredRevision: "revision-2"}))

	ids, err := store.ListSessionsOfDeletedAgents(t.Context(), "", 100)
	require.NoError(t, err)
	require.Empty(t, ids)
	// A listing made before the Agent came back is rechecked at admission.
	_, err = store.BeginDeletedAgentSessionDeletion(t.Context(), session.Id)
	require.ErrorIs(t, err, database.ErrConflict)
	worker, err := NewExpirationWorker(store, workflow, 0, time.Minute)
	require.NoError(t, err)
	worker.sweepDeletedAgents(t.Context())
	current, err := store.GetSessionByID(t.Context(), session.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, current.State)
	require.Len(t, actors.actors, 1)
}

// A deletion whose runtime work fails stays admitted and is finished by a later
// sweep, without a client retry, and never deletes a second Actor.
func TestSessionDeletionAfterAgentDeletionRetries(t *testing.T) {
	for _, failure := range []string{"preparation", "lost runtime response", "persistence"} {
		t.Run(failure, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			session, err := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Second).Create(t.Context(), session)
			require.NoError(t, err)
			require.NoError(t, store.RetireAgentIdentities(t.Context(), "team-a", "assistant", nil))
			actors := &expirationActors{retryTestActors: &retryTestActors{lifecycleTestActors: base}}
			writes := &completionTestStore{lifecycleTestStore: store}
			switch failure {
			case "preparation":
				actors.readErr = status.Error(codes.Unavailable, "read unavailable")
			case "lost runtime response":
				actors.loseDeleteResponse = true
			case "persistence":
				writes.finishErr = errors.New("database unavailable")
			}
			worker, err := NewExpirationWorker(store, NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Second), 0, time.Minute)
			require.NoError(t, err)
			require.Error(t, worker.deleteOfDeletedAgent(t.Context(), session.Id))
			require.ErrorIs(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next turn"), database.ErrConflict)

			actors.readErr, writes.finishErr = nil, nil
			ids, err := store.ListSessionsOfDeletedAgents(t.Context(), "", 100)
			require.NoError(t, err)
			require.Equal(t, []string{session.Id}, ids, "the admitted deletion is listed until it is finished")
			require.NoError(t, worker.deleteOfDeletedAgent(t.Context(), session.Id))
			_, err = store.GetSessionByID(t.Context(), session.Id)
			require.ErrorIs(t, err, database.ErrNotFound)
			require.Empty(t, base.actors)
			require.Equal(t, 1, actors.deletions, "retries must not allocate or delete another Actor")
		})
	}
}

// A session whose turn is still being dispatched or settled is left for a later
// sweep: the delete is admitted only once the runtime is settled.
func TestSessionDeletionAfterAgentDeletionWaitsForTheTurn(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
	session, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)
	dispatch := uuid.New()
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, dispatch, "turn"))
	require.NoError(t, store.RetireAgentIdentities(t.Context(), "team-a", "assistant", nil))
	worker, err := NewExpirationWorker(store, workflow, 0, time.Minute)
	require.NoError(t, err)

	require.ErrorIs(t, worker.deleteOfDeletedAgent(t.Context(), session.Id), database.ErrFailedPrecondition)
	_, err = store.GetSessionByID(t.Context(), session.Id)
	require.NoError(t, err)
	require.Len(t, actors.actors, 1)

	revoked, err := store.RevokeSessionDispatch(t.Context(), session.Id, dispatch, "turn")
	require.NoError(t, err)
	require.True(t, revoked)
	require.NoError(t, worker.deleteOfDeletedAgent(t.Context(), session.Id))
	_, err = store.GetSessionByID(t.Context(), session.Id)
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Empty(t, actors.actors)
}

// A checkpoint retains runnable inputs after its Agent is deleted, and a fork
// of it, made before or after the deletion, runs on them: the sweep leaves it
// alone. The fork pins its checkpoint until it is deleted; after the last fork
// and the checkpoint are gone, the GC collects the revision.
func TestForksOfADeletedAgentsCheckpointStayRunnable(t *testing.T) {
	store, source := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
	early, checkpointID := lifecycleForkFixture(t, store, actors, source)
	early, err := workflow.Create(t.Context(), early)
	require.NoError(t, err)
	require.NoError(t, store.RetireAgentIdentities(t.Context(), "team-a", "assistant", nil))
	late, created, err := store.ForkSession(t.Context(), checkpointID, source.Creator, uuid.NewString(), uuid.NewString())
	require.NoError(t, err)
	require.True(t, created, "a checkpoint forks after its Agent is deleted")
	late, err = workflow.Create(t.Context(), late)
	require.NoError(t, err)
	worker, err := NewExpirationWorker(store, workflow, 0, time.Minute)
	require.NoError(t, err)

	ids, err := store.ListSessionsOfDeletedAgents(t.Context(), "", 100)
	require.NoError(t, err)
	require.Equal(t, []string{source.Id}, ids, "only the source is a session of the deleted Agent")
	forks := []*apiv1alpha1.Session{early, late}
	for _, fork := range forks {
		_, err = store.BeginDeletedAgentSessionDeletion(t.Context(), fork.Id)
		require.ErrorIs(t, err, database.ErrConflict, "a listing made before the fork is rechecked at admission")
	}
	worker.sweepDeletedAgents(t.Context())
	_, err = store.GetSessionByID(t.Context(), source.Id)
	require.ErrorIs(t, err, database.ErrNotFound)
	for _, fork := range forks {
		current, err := store.GetSessionByID(t.Context(), fork.Id)
		require.NoError(t, err)
		require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, current.State)
	}
	dispatch := uuid.New()
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), late.Id, dispatch, "turn"), "the fork takes a turn")
	_, err = store.RevokeSessionDispatch(t.Context(), late.Id, dispatch, "turn")
	require.NoError(t, err)
	_, _, err = store.BeginDeleteSessionCheckpoint(t.Context(), checkpointID, source.Creator)
	require.ErrorIs(t, err, database.ErrNotFound, "a fork pins its checkpoint")

	for _, fork := range forks {
		_, err = workflow.Delete(t.Context(), fork)
		require.NoError(t, err)
	}
	require.Empty(t, actors.actors)
	unreferenced, err := store.ListUnreferencedRuntimeRevisions(t.Context())
	require.NoError(t, err)
	require.Empty(t, unreferenced, "the checkpoint keeps the revision")
	_, _, err = store.BeginDeleteSessionCheckpoint(t.Context(), checkpointID, source.Creator)
	require.NoError(t, err)
	require.NoError(t, store.DeleteSessionCheckpoint(t.Context(), checkpointID, source.Creator))
	unreferenced, err = store.ListUnreferencedRuntimeRevisions(t.Context())
	require.NoError(t, err)
	require.Len(t, unreferenced, 1, "the last fork and the checkpoint gone, the GC collects the revision")
	require.Equal(t, store.revision.Revision, unreferenced[0].Revision)
}

// The sweep never cuts a turn off: a session whose turn runs after its dispatch
// was accepted is left for a later sweep, which deletes it once the turn ended.
func TestSessionDeletionAfterAgentDeletionWaitsForARunningTurn(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
	session, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
	message.ContextID = session.ContextId
	task := a2a.NewSubmittedTask(message, message)
	task.Status.State = a2a.TaskStateWorking
	createHash := sha256.Sum256([]byte("running"))
	version, err := store.CreateRuntimeTask(t.Context(), session.Id, createHash[:], task, "")
	require.NoError(t, err)
	require.NoError(t, store.RetireAgentIdentities(t.Context(), "team-a", "assistant", nil))
	worker, err := NewExpirationWorker(store, workflow, 0, time.Minute)
	require.NoError(t, err)

	require.ErrorIs(t, worker.deleteOfDeletedAgent(t.Context(), session.Id), database.ErrFailedPrecondition)
	worker.sweepDeletedAgents(t.Context())
	current, err := store.GetSessionByID(t.Context(), session.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, current.State)
	require.Len(t, actors.actors, 1)

	task.Status.State = a2a.TaskStateCompleted
	completeHash := sha256.Sum256([]byte("completed"))
	version, err = store.UpdateSessionTask(t.Context(), session.Id, version, completeHash[:], task, task, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
	worker.sweepDeletedAgents(t.Context())
	_, err = store.GetSessionByID(t.Context(), session.Id)
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Empty(t, actors.actors)
}
