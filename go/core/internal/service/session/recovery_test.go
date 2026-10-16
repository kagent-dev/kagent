package session

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), 10*time.Millisecond)
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
			actors.actors[key].Status.ExternalSnapshot = &ateapipb.ExternalSnapshot{
				SnapshotUri: "s3://snapshots/previous-turn", ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA,
			}
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
			require.True(t, proto.Equal(before.Status.ExternalSnapshot, after.Status.ExternalSnapshot))
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

func (a *lifecycleTestActors) RevertActor(_ context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	actor := a.actors[actorKey(atespace, name)]
	if actor.Status.State != ateapipb.ActorState_ACTOR_STATE_CRASHED && actor.Status.State != ateapipb.ActorState_ACTOR_STATE_REVERTING {
		return nil, status.Error(codes.FailedPrecondition, "actor is not crashed or reverting")
	}
	actor.Status.State = ateapipb.ActorState_ACTOR_STATE_SUSPENDED
	actor.Status.Crash = nil
	return proto.CloneOf(actor), nil
}

type recoveryTestActors struct {
	*lifecycleTestActors
	revert  func(context.Context, string, string) (*ateapipb.Actor, error)
	readErr error
}

func (a *recoveryTestActors) RevertActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	return a.revert(ctx, atespace, name)
}

func (a *recoveryTestActors) GetActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	if a.readErr != nil {
		return nil, a.readErr
	}
	return a.lifecycleTestActors.GetActor(ctx, atespace, name)
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
			require.Error(t, workflow.recoverSessionCandidate(t.Context(), session))
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
			require.NoError(t, restarted.recoverSessionCandidate(t.Context(), ready))
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
	go func() { done <- workflow.recoverSessionCandidate(ctx, session) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("recovery was not admitted")
	}
	failed, err := store.GetSessionByID(ctx, session.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_FAILED, failed.State)
	require.ErrorIs(t, workflow.recoverSessionCandidate(ctx, failed), database.ErrConflict)
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
		name    string
		state   ateapipb.ActorState
		uid     string
		readErr error
		wantErr bool
	}{
		{name: "quiet running actor", state: ateapipb.ActorState_ACTOR_STATE_RUNNING},
		{name: "transient read failure", readErr: status.Error(codes.Unavailable, "unreachable"), wantErr: true},
		{name: "same-name replacement", state: ateapipb.ActorState_ACTOR_STATE_CRASHED, uid: "another-actor", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, base, session, task, _ := crashedTurnFixture(t)
			actor := base.actors[actorKey("team-a", substrate.ActorName(session.Id))]
			actor.Status.State = test.state
			if test.uid != "" {
				actor.Metadata.Uid = test.uid
			}
			actors := &recoveryTestActors{lifecycleTestActors: base, readErr: test.readErr}
			actors.revert = func(context.Context, string, string) (*ateapipb.Actor, error) {
				t.Fatal("unconfirmed crash issued a revert")
				return nil, nil
			}
			err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second).recoverSessionCandidate(t.Context(), session)
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
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

func TestSessionRecoveryCandidatesExcludeIdleAndWaitingSessions(t *testing.T) {
	store, actors, active, _, _ := crashedTurnFixture(t)
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
	var want []string
	want = append(want, active.Id)
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
		if state == a2a.TaskStateWorking {
			want = append(want, session.Id)
		} else {
			require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
		}
	}
	var got []string
	var after string
	for {
		page, err := store.ListSessionRecoveryCandidates(t.Context(), after, 1)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		got = append(got, page[0].Id)
		after = page[0].Id
	}
	require.ElementsMatch(t, want, got)
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
	require.NoError(t, store.FinishSessionQuiescence(t.Context(), work, &database.SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/waiting-turn", ContentScope: "DATA"}))

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
		Atespace: "team-a", URI: "s3://snapshots/newer-completed-turn", ContentScope: "DATA",
	}))

	older.Status.State = a2a.TaskStateWorking
	hash = sha256.Sum256([]byte("resume older task"))
	_, err = store.UpdateSessionTask(t.Context(), session.Id, waitingVersion, hash[:], older, older, "")
	require.NoError(t, err)
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
	require.NoError(t, workflow.recoverSessionCandidate(t.Context(), session))
	failed, err := store.GetSettledSessionTask(t.Context(), session.Id, string(older.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateFailed, failed.Status.State)
	retained, retainedVersion, err := store.GetVersionedSessionTask(t.Context(), session.Id, string(newer.ID))
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, retained.Status.State)
	require.Equal(t, newerVersion, retainedVersion, "recovery cannot rewrite a later completed task")
	require.NoError(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), uuid.NewString()))
}
