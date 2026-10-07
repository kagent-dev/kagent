package session

import (
	"errors"
	"testing"
	"time"

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
