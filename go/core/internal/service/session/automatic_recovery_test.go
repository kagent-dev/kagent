package session

import (
	"context"
	"crypto/sha256"
	"slices"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

func TestCrashedSessionSettlesTurnAndAcceptsNextMessage(t *testing.T) {
	for _, test := range []struct {
		name   string
		staged a2a.TaskState
		want   a2a.TaskState
	}{
		{name: "interrupted execution", want: a2a.TaskStateFailed},
		{name: "durable result before cleanup", staged: a2a.TaskStateCompleted, want: a2a.TaskStateCompleted},
		{name: "uncheckpointed input wait", staged: a2a.TaskStateInputRequired, want: a2a.TaskStateFailed},
		{name: "uncheckpointed auth wait", staged: a2a.TaskStateAuthRequired, want: a2a.TaskStateFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), 10*time.Millisecond, true)
			session, err := workflow.Create(t.Context(), session)
			require.NoError(t, err)
			message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
			message.ContextID = session.ContextId
			task := a2a.NewSubmittedTask(message, message)
			task.Status.State = a2a.TaskStateWorking
			hash := sha256.Sum256([]byte("working"))
			version, err := store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, "")
			require.NoError(t, err)
			if test.staged != "" {
				task.Status.State = test.staged
				hash = sha256.Sum256([]byte("completed"))
				_, err = store.UpdateSessionTask(t.Context(), session.Id, version, hash[:], task, task, "")
				require.NoError(t, err)
			}
			key := actorKey("team-a", substrate.ActorName(session.Id))
			actors.actors[key].Status.State = ateapipb.ActorState_ACTOR_STATE_CRASHED
			actors.actors[key].Status.Snapshots = []*ateapipb.Snapshot{{Generation: 1, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, DurableSnapshot: &ateapipb.SnapshotStorage{Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/previous-turn"}}}}
			before := proto.CloneOf(actors.actors[key])
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- workflow.Start(ctx) }()
			t.Cleanup(func() { cancel(); require.NoError(t, <-done) })

			require.EventuallyWithT(t, func(t *assert.CollectT) {
				stored, err := store.GetSettledSessionTask(ctx, session.Id, string(task.ID), nil)
				if !assert.NoError(t, err) {
					return
				}
				assert.Equal(t, test.want, stored.Status.State)
				current, err := store.GetSessionByID(ctx, session.Id)
				if assert.NoError(t, err) {
					assert.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.State)
				}
			}, 5*time.Second, 10*time.Millisecond)
			after, err := actors.GetActor(ctx, "team-a", substrate.ActorName(session.Id))
			require.NoError(t, err)
			require.Equal(t, before.Metadata.Uid, after.Metadata.Uid)
			require.True(t, proto.Equal(substrate.LatestDurableActorSnapshot(before), substrate.LatestDurableActorSnapshot(after)))
			require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, after.Status.State)
			current, err := store.GetSessionByID(ctx, session.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.State)
			require.Equal(t, session.PreparedRevision, current.PreparedRevision)
			checkpoint := &apiv1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID)}
			_, _, err = store.ReserveSessionCheckpoint(ctx, checkpoint, session.Creator, uuid.NewString())
			require.ErrorIs(t, err, database.ErrSnapshotPending, "the older Actor snapshot does not checkpoint the discarded turn")
			require.NoError(t, store.ReserveSessionDispatch(ctx, session.Id, uuid.New(), uuid.NewString()))
		})
	}
}

func TestAutomaticRecoveryPagesPastUnavailableActors(t *testing.T) {
	store, base, first, _, _ := crashedTurnFixture(t)
	actors := &recoveryTestActors{lifecycleTestActors: base, revert: base.RevertActor, readErr: status.Error(codes.Unavailable, "control plane unavailable")}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute, true)
	ids := []string{first.Id}
	for range 100 {
		session, _, err := store.CreateSession(t.Context(), &apiv1.Session{
			Id: uuid.NewString(), Creator: first.Creator, Agent: first.Agent,
		}, uuid.NewString())
		require.NoError(t, err)
		session, err = NewActorWorkflow(store, base, make(chan struct{}, 1), time.Minute, false).Create(t.Context(), session)
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
	require.Empty(t, after, "the short page resets the cursor")
	for _, id := range ids {
		actor, err := base.GetActor(t.Context(), "team-a", substrate.ActorName(id))
		require.NoError(t, err)
		require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, actor.GetStatus().GetState())
	}
}

func TestAutomaticRecoveryCandidatePolicy(t *testing.T) {
	store, actors, active, _, _ := crashedTurnFixture(t)
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second, false)
	var want []string
	want = append(want, active.Id)
	for _, state := range []a2a.TaskState{"", a2a.TaskStateInputRequired, a2a.TaskStateAuthRequired, a2a.TaskStateWorking} {
		session, _, err := store.CreateSession(t.Context(), &apiv1.Session{Id: uuid.NewString(), Creator: "alice", Agent: active.Agent}, uuid.NewString())
		require.NoError(t, err)
		session, err = workflow.Create(t.Context(), session)
		require.NoError(t, err)
		want = append(want, session.Id)
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
	var got []string
	var after string
	for {
		page, err := store.ListSessionRecoveryCandidates(t.Context(), after, 1, true)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		got = append(got, page[0].Id)
		after = page[0].Id
	}
	require.ElementsMatch(t, want, got)
	page, err := store.ListSessionRecoveryCandidates(t.Context(), "", 100, false)
	require.NoError(t, err)
	require.Empty(t, page, "disabled automatic recovery does not inspect idle or active runtimes")
	actor, err := actors.GetActor(t.Context(), "team-a", substrate.ActorName(active.Id))
	require.NoError(t, err)
	_, err = store.BeginCrashedSessionRecovery(t.Context(), active.Id, revertGeneration(t, store, active.Id), actor.GetMetadata().GetUid(), database.SessionTaskSnapshot{})
	require.NoError(t, err)
	page, err = store.ListSessionRecoveryCandidates(t.Context(), "", 100, false)
	require.NoError(t, err)
	require.Len(t, page, 1, "admitted recovery still converges after automatic recovery is disabled")
	require.Equal(t, active.Id, page[0].Id)
}

func TestAutomaticRecoveryIsOptInForIdleSessions(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	manual := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute, false)
	session, err := manual.Create(t.Context(), session)
	require.NoError(t, err)
	key := actorKey("team-a", substrate.ActorName(session.Id))
	actors.actors[key].Status.State = ateapipb.ActorState_ACTOR_STATE_CRASHED
	_, err = manual.recoverSessionPage(t.Context(), "")
	require.NoError(t, err)
	actor, err := actors.GetActor(t.Context(), "team-a", substrate.ActorName(session.Id))
	require.NoError(t, err)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_CRASHED, actor.GetStatus().GetState())
	observed, err := manual.Recovery(t.Context(), session)
	require.NoError(t, err)
	require.True(t, observed.GetNeeded())
	require.False(t, observed.Automatic)

	automatic := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute, true)
	_, err = automatic.recoverSessionPage(t.Context(), "")
	require.NoError(t, err)
	actor, err = actors.GetActor(t.Context(), "team-a", substrate.ActorName(session.Id))
	require.NoError(t, err)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, actor.GetStatus().GetState(), "opt-in recovery includes an idle crashed runtime")
	observed, err = automatic.Recovery(t.Context(), session)
	require.NoError(t, err)
	require.True(t, observed.Automatic)
	require.False(t, observed.GetNeeded())
}
