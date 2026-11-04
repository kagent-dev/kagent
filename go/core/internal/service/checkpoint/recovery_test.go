package checkpoint

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// lockedTags models Substrate's tag lease independently of a request or worker.
// The copy can outlive its client; another CreateTag waits for the lease before
// returning AlreadyExists. GetTag alone can observe a still-live pending copy.
type lockedTags struct {
	mu      sync.Mutex
	base    *testTags
	gate    <-chan struct{}
	entered chan struct{}
	once    sync.Once
}

var _ tagClient = (*lockedTags)(nil)

func (t *lockedTags) GetActor(ctx context.Context, space, name string) (*ateapipb.Actor, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.base.GetActor(ctx, space, name)
}

func (t *lockedTags) GetTag(ctx context.Context, space, name string) (*ateapipb.Tag, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tag, err := t.base.GetTag(ctx, space, name)
	return proto.CloneOf(tag), err
}

func (t *lockedTags) CreateTag(ctx context.Context, space, name, actor string) (*ateapipb.Tag, error) {
	if t.entered != nil {
		t.once.Do(func() { close(t.entered) })
	}
	if t.gate != nil {
		select {
		case <-t.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	tag, err := t.base.CreateTag(ctx, space, name, actor)
	return proto.CloneOf(tag), err
}

func (t *lockedTags) DeleteTag(ctx context.Context, space, name, uid string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.base.DeleteTag(ctx, space, name, uid)
}

func startRecovery(t *testing.T, service *Service) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	worker := NewRecoveryWorker(service)
	worker.interval = 10 * time.Millisecond
	go func() { done <- worker.Start(ctx) }()
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); require.NoError(t, <-done) }) }
	t.Cleanup(stop)
	return stop
}

func recoverySource(t *testing.T, store *database.Client) (*apiv1.Session, string) {
	t.Helper()
	return recoverySourceForActor(t, store, &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: "team-a", Name: substrate.ActorName(uuid.NewString()), Uid: "actor-uid"},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: "team-a", Name: "runtime"},
		Status: &ateapipb.ActorStatus{Snapshots: []*ateapipb.Snapshot{{
			Generation: 1, ActorTemplateUid: "template-uid", Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES,
			DurableSnapshot: &ateapipb.SnapshotStorage{State: ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED,
				Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/source"}},
		}}},
	})
}

func recoverySourceForActor(t *testing.T, store *database.Client, actor *ateapipb.Actor) (*apiv1.Session, string) {
	t.Helper()
	ctx := t.Context()
	space := actor.GetMetadata().GetAtespace()
	snapshot := substrate.LatestActorSnapshot(actor)
	revision := "revision-" + actor.GetMetadata().GetUid()
	sessionID := strings.TrimPrefix(actor.GetMetadata().GetName(), "session-")
	_, err := uuid.Parse(sessionID)
	require.NoError(t, err)
	require.NoError(t, store.UpsertAgentDefinition(ctx, database.AgentDefinition{
		Namespace: space, AgentName: "assistant", AgentUID: "agent-uid", DesiredRevision: revision}))
	_, err = store.RecordRuntimeRevision(ctx, database.RuntimeRevision{
		Revision: revision, Namespace: space, AgentName: "assistant", AgentUID: "agent-uid",
		SourceSnapshot: []byte("{}"), AgentCard: &a2apb.AgentCard{}, EgressDestinations: []string{},
		ActorTemplateAtespace: space, ActorTemplateName: actor.GetActorTemplate().GetName(), ActorTemplateUID: snapshot.GetActorTemplateUid(),
	}, database.RecordRevisionOptions{Ready: true})
	require.NoError(t, err)
	session, _, err := store.CreateSession(ctx, &apiv1.Session{Id: sessionID, Creator: "alice",
		Agent: &apiv1.AgentProvenance{Ref: &apiv1.ResourceReference{Namespace: space, Name: "assistant"}}}, uuid.NewString())
	require.NoError(t, err)
	op, err := store.BeginSessionOperation(ctx, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
	require.NoError(t, err)
	executor := uuid.New()
	claimed, err := store.ClaimSessionOperation(ctx, session.Id, op.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	session, err = store.FinishSessionOperation(ctx, session.Id, op.ID, executor, actor.GetActorTemplate().GetName(), actor.GetMetadata().GetUid(), "")
	require.NoError(t, err)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
	message.ContextID = session.ContextId
	task := a2a.NewSubmittedTask(message, message)
	task.Status.State = a2a.TaskStateCompleted
	hash := sha256.Sum256([]byte(task.ID))
	version, err := store.CreateRuntimeTask(ctx, session.Id, hash[:], task, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(ctx, session.Id, string(task.ID), version))
	boundary, err := store.ClaimSessionQuiescence(ctx)
	require.NoError(t, err)
	require.NoError(t, store.FinishSessionQuiescence(ctx, boundary,
		&database.SessionTaskSnapshot{Atespace: space, URI: substrate.SnapshotURI(snapshot), Fidelity: snapshot.GetFidelity().String()}))
	return session, string(task.ID)
}

func waitCheckpointPhase(t *testing.T, store *database.Client, id string, phase apiv1.CheckpointState) *database.CheckpointOperation {
	t.Helper()
	var op *database.CheckpointOperation
	require.Eventually(t, func() bool {
		var err error
		op, err = store.GetCheckpointOperation(t.Context(), id, "alice")
		return err == nil && op.Checkpoint.State == phase
	}, 5*time.Second, 10*time.Millisecond)
	return op
}

type failedCheckpointPublish struct {
	*database.Client
	entered chan struct{}
	once    sync.Once
}

func (s *failedCheckpointPublish) FinalizeClaimedSessionCheckpoint(ctx context.Context, _ string, _ uuid.UUID, _, _, _ string) (*apiv1.Checkpoint, error) {
	s.once.Do(func() { close(s.entered) })
	<-ctx.Done()
	return nil, ctx.Err()
}

// pausedCheckpointClaim models an executor paused after its successful database
// reply. Its attempt deadline still elapses while a replacement makes progress.
type pausedCheckpointClaim struct {
	*database.Client
	claimed chan struct{}
	resume  <-chan struct{}
}

type lostCheckpointClaim struct{ *database.Client }

func (s *lostCheckpointClaim) ClaimCheckpointOperation(ctx context.Context, id, owner string, executor uuid.UUID) (*database.CheckpointOperation, error) {
	if _, err := s.Client.ClaimCheckpointOperation(ctx, id, owner, executor); err != nil {
		return nil, err
	}
	return nil, status.Error(codes.Unavailable, "committed claim response lost")
}

func (s *pausedCheckpointClaim) ClaimCheckpointOperation(ctx context.Context, id, owner string, executor uuid.UUID) (*database.CheckpointOperation, error) {
	op, err := s.Client.ClaimCheckpointOperation(ctx, id, owner, executor)
	if err != nil {
		return nil, err
	}
	close(s.claimed)
	<-s.resume
	return op, nil
}

func TestCheckpointRecoveryWithPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	conn := dbtest.StartT(t.Context(), t)
	require.NoError(t, dbtest.Migrate(conn, false))
	pool, err := database.Connect(t.Context(), &database.PostgresConfig{Pool: dbtest.PoolConfig(conn), Schema: "public"})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	newStore := func() *database.Client { return database.NewClient(pool, "public") }
	ownerContext := func(ctx context.Context) context.Context {
		return auth.AuthSessionTo(ctx, testSession{userID: "alice"})
	}

	t.Run("startup recovers a reservation whose caller disappeared", func(t *testing.T) {
		store := newStore()
		session, head := recoverySource(t, store)
		tags := &lockedTags{base: &testTags{snapshotURI: "s3://snapshots/source"}}
		service := NewService(store, testAuthorizer{}, tags, nil)
		request := uuid.NewString()
		ctx, cancel := context.WithCancel(ownerContext(t.Context()))
		t.Cleanup(cancel)
		done := make(chan error, 1)
		go func() { _, err := service.Create(ctx, session.Id, request, head); done <- err }()
		var pending []*database.CheckpointOperation
		require.Eventually(t, func() bool {
			var err error
			pending, err = store.ListPendingCheckpoints(t.Context(), "", 10)
			return err == nil && len(pending) == 1
		}, 5*time.Second, 10*time.Millisecond)
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
		require.Zero(t, tags.base.createCalls, "API admission must not start its own copy")
		replacement := NewService(newStore(), testAuthorizer{}, tags, nil)
		stop := startRecovery(t, replacement)
		replayed, err := replacement.Create(ownerContext(t.Context()), session.Id, request, head)
		require.NoError(t, err)
		require.Equal(t, pending[0].Checkpoint.Id, replayed.Id)
		stop()
	})

	t.Run("canceled caller and concurrent replicas leave one live copy", func(t *testing.T) {
		store := newStore()
		session, head := recoverySource(t, store)
		gate := make(chan struct{})
		tags := &lockedTags{base: &testTags{snapshotURI: "s3://snapshots/source"}, gate: gate, entered: make(chan struct{})}
		leader := NewService(store, testAuthorizer{}, tags, nil)
		stop := startRecovery(t, leader)
		ctx, cancel := context.WithCancel(ownerContext(t.Context()))
		result := make(chan error, 1)
		request := uuid.NewString()
		go func() { _, err := leader.Create(ctx, session.Id, request, head); result <- err }()
		select {
		case <-tags.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("copy did not start")
		}
		cancel()
		require.ErrorIs(t, <-result, context.Canceled)
		// Another replica shares durable admission, not local singleflight.
		follower := NewService(newStore(), testAuthorizer{}, tags, nil)
		stopFollower := startRecovery(t, follower)
		results := make(chan *apiv1.Checkpoint, 8)
		errors := make(chan error, 8)
		for range 8 {
			go func() {
				cp, err := follower.Create(ownerContext(t.Context()), session.Id, request, head)
				results <- cp
				errors <- err
			}()
		}
		pending, err := store.ListPendingCheckpoints(t.Context(), "", 10)
		require.NoError(t, err)
		require.Len(t, pending, 1)
		_, err = store.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
		require.ErrorIs(t, err, database.ErrConflict)
		close(gate)
		for range 8 {
			require.NoError(t, <-errors)
			require.Equal(t, pending[0].Checkpoint.Id, (<-results).Id)
		}
		stopFollower()
		stop()
		tags.mu.Lock()
		defer tags.mu.Unlock()
		require.Equal(t, 1, tags.base.createCalls)
		require.Zero(t, tags.base.deleteCalls)
	})

	t.Run("restart reuses completed tag after lost database publication", func(t *testing.T) {
		store := newStore()
		session, head := recoverySource(t, store)
		tags := &lockedTags{base: &testTags{snapshotURI: "s3://snapshots/source"}}
		fault := &failedCheckpointPublish{Client: store, entered: make(chan struct{})}
		service := NewService(fault, testAuthorizer{}, tags, nil)
		cp, _, err := service.beginCreate(ownerContext(t.Context()), session.Id, uuid.NewString(), head)
		require.NoError(t, err)
		stop := startRecovery(t, service)
		select {
		case <-fault.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("publication did not start")
		}
		stop()
		waitCheckpointPhase(t, store, cp.Id, apiv1.CheckpointState_CHECKPOINT_STATE_CREATING)
		replacement := NewService(newStore(), testAuthorizer{}, tags, nil)
		stop = startRecovery(t, replacement)
		waitCheckpointPhase(t, store, cp.Id, apiv1.CheckpointState_CHECKPOINT_STATE_READY)
		stop()
		tags.mu.Lock()
		defer tags.mu.Unlock()
		require.Equal(t, "tag-uid", tags.base.created.GetMetadata().GetUid())
		require.Zero(t, tags.base.deleteCalls)
	})

	t.Run("restart completes compensation and releases source admission", func(t *testing.T) {
		store := newStore()
		session, head := recoverySource(t, store)
		tags := &lockedTags{base: &testTags{snapshotURI: "s3://snapshots/source", deleteErr: errors.New("store offline"),
			mutateTag: func(tag *ateapipb.Tag) {
				tag.Status.Snapshot.DurableSnapshot.State = ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_PENDING
			}}}
		service := NewService(store, testAuthorizer{}, tags, nil)
		cp, _, err := service.beginCreate(ownerContext(t.Context()), session.Id, uuid.NewString(), head)
		require.NoError(t, err)
		_, err = tags.base.CreateTag(t.Context(), "team-a", tagName(cp.Id), "session-"+session.Id)
		require.NoError(t, err)
		tags.base.createCalls = 0
		stop := startRecovery(t, service)
		require.Eventually(t, func() bool {
			op, err := store.GetCheckpointOperation(t.Context(), cp.Id, "alice")
			return err == nil && op.Checkpoint.Failure != nil
		}, 5*time.Second, 10*time.Millisecond)
		stop()
		tags.mu.Lock()
		tags.base.deleteErr = nil
		tags.mu.Unlock()
		replacement := NewService(newStore(), testAuthorizer{}, tags, nil)
		stop = startRecovery(t, replacement)
		waitCheckpointPhase(t, store, cp.Id, apiv1.CheckpointState_CHECKPOINT_STATE_FAILED)
		stop()
		tags.mu.Lock()
		require.Equal(t, 1, tags.base.createCalls)
		require.Nil(t, tags.base.created)
		tags.mu.Unlock()
		message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("after recovery"))
		message.ContextID = session.ContextId
		task := a2a.NewSubmittedTask(message, message)
		hash := sha256.Sum256([]byte(task.ID))
		_, err = store.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, "")
		require.NoError(t, err)
	})

	t.Run("Given compensation already deleted the tag, when stale creation resumes, assert that source admission reopens without another copy", func(t *testing.T) {
		store := newStore()
		session, head := recoverySource(t, store)
		tags := &testTags{snapshotURI: "s3://snapshots/source"}
		service := NewService(store, testAuthorizer{}, tags, nil)
		cp, _, err := service.beginCreate(ownerContext(t.Context()), session.Id, uuid.NewString(), head)
		require.NoError(t, err)
		stale, err := store.GetCheckpointOperation(t.Context(), cp.Id, "alice")
		require.NoError(t, err)
		tag, err := tags.CreateTag(t.Context(), "team-a", tagName(cp.Id), substrate.ActorName(session.Id))
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), database.RuntimeOperationTimeout)
		defer cancel()
		executor := uuid.New()
		_, err = store.ClaimCheckpointOperation(ctx, cp.Id, "alice", executor)
		require.NoError(t, err)
		require.NoError(t, store.BeginCheckpointCleanup(ctx, cp.Id, executor, tag.Metadata.Uid, "copy interrupted"))
		require.NoError(t, tags.DeleteTag(ctx, "team-a", tagName(cp.Id), tag.Metadata.Uid))
		require.NoError(t, store.ReleaseCheckpointOperation(ctx, cp.Id, executor))

		require.NoError(t, service.recoverOperation(t.Context(), stale))
		require.Nil(t, tags.created, "a stale discovery result must not recreate a compensated tag")
		require.Equal(t, 1, tags.createCalls)
		waitCheckpointPhase(t, store, cp.Id, apiv1.CheckpointState_CHECKPOINT_STATE_FAILED)
		_, err = store.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
		require.NoError(t, err)
		_, err = service.beginDelete(ownerContext(t.Context()), cp.Id)
		require.NoError(t, err)
		require.NoError(t, service.recoverOperation(t.Context(), stale))
		require.NoError(t, service.recoverOperation(t.Context(), stale), "deleted work must not recreate an orphan tag")
		_, err = store.GetCheckpointOperation(t.Context(), cp.Id, "alice")
		require.ErrorIs(t, err, database.ErrNotFound)
		require.Nil(t, tags.created)
		require.Equal(t, 1, tags.createCalls)
	})

	t.Run("Given a paused executor, when its attempt expires and recovery succeeds, assert that its late continuation cannot create a tag", func(t *testing.T) {
		store := newStore()
		session, head := recoverySource(t, store)
		tags := &testTags{snapshotURI: "s3://snapshots/source"}
		resume := make(chan struct{})
		paused := &pausedCheckpointClaim{Client: store, claimed: make(chan struct{}), resume: resume}
		old := NewService(paused, testAuthorizer{}, tags, nil)
		cp, _, err := old.beginCreate(ownerContext(t.Context()), session.Id, uuid.NewString(), head)
		require.NoError(t, err)
		op, err := store.GetCheckpointOperation(t.Context(), cp.Id, "alice")
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- old.recoverOperation(ctx, op) }()
		select {
		case <-paused.claimed:
		case <-time.After(time.Second):
			t.Fatal("old worker never claimed the operation")
		}
		<-ctx.Done()
		replacement := NewService(newStore(), testAuthorizer{}, tags, nil)
		require.Eventually(t, func() bool {
			require.NoError(t, replacement.recoverOperation(t.Context(), op))
			current, err := store.GetCheckpointOperation(t.Context(), cp.Id, "alice")
			return err == nil && current.Checkpoint.State == apiv1.CheckpointState_CHECKPOINT_STATE_READY
		}, time.Second, 10*time.Millisecond)
		close(resume)
		require.ErrorIs(t, <-done, context.DeadlineExceeded)
		require.Equal(t, 1, tags.createCalls, "the expired worker must issue no backend mutation")
		require.Zero(t, tags.deleteCalls)
	})

	t.Run("Given a lost committed claim reply, when a replacement retries, assert that it can immediately resume", func(t *testing.T) {
		store := newStore()
		session, head := recoverySource(t, store)
		tags := &testTags{snapshotURI: "s3://snapshots/source"}
		service := NewService(&lostCheckpointClaim{Client: store}, testAuthorizer{}, tags, nil)
		cp, _, err := service.beginCreate(ownerContext(t.Context()), session.Id, uuid.NewString(), head)
		require.NoError(t, err)
		op, err := store.GetCheckpointOperation(t.Context(), cp.Id, "alice")
		require.NoError(t, err)
		require.Error(t, service.recoverOperation(t.Context(), op))
		require.Zero(t, tags.createCalls)
		replacement := NewService(newStore(), testAuthorizer{}, tags, nil)
		require.NoError(t, replacement.recoverOperation(t.Context(), op))
		waitCheckpointPhase(t, store, cp.Id, apiv1.CheckpointState_CHECKPOINT_STATE_READY)
		require.Equal(t, 1, tags.createCalls)
	})

	t.Run("restart finishes deletion after backend failure", func(t *testing.T) {
		store := newStore()
		session, head := recoverySource(t, store)
		tags := &lockedTags{base: &testTags{snapshotURI: "s3://snapshots/source"}}
		service := NewService(store, testAuthorizer{}, tags, nil)
		stop := startRecovery(t, service)
		cp, err := service.Create(ownerContext(t.Context()), session.Id, uuid.NewString(), head)
		require.NoError(t, err)
		stop()
		tags.mu.Lock()
		tags.base.deleteErr = status.Error(codes.Unavailable, "store offline")
		tags.mu.Unlock()
		_, err = service.beginDelete(ownerContext(t.Context()), cp.Id)
		require.NoError(t, err)
		stop = startRecovery(t, service)
		waitCheckpointPhase(t, store, cp.Id, apiv1.CheckpointState_CHECKPOINT_STATE_DELETING)
		stop()
		tags.mu.Lock()
		tags.base.deleteErr = nil
		tags.mu.Unlock()
		stop = startRecovery(t, NewService(newStore(), testAuthorizer{}, tags, nil))
		require.Eventually(t, func() bool {
			_, err := store.GetCheckpointOperation(t.Context(), cp.Id, "alice")
			return errors.Is(err, database.ErrNotFound)
		}, 5*time.Second, 10*time.Millisecond)
		stop()
		tags.mu.Lock()
		defer tags.mu.Unlock()
		require.Nil(t, tags.base.created)
	})
}

// An old controller's remote copy can still be running after failover. Model
// the backend lease directly: the new CreateTag cannot answer AlreadyExists
// until that copy ends, even though GetTag would already return a pending row.
type serializedCopyTags struct {
	*testTags
	entered chan struct{}
	release <-chan struct{}
}

func (s *serializedCopyTags) CreateTag(ctx context.Context, _, _, _ string) (*ateapipb.Tag, error) {
	close(s.entered)
	select {
	case <-s.release:
		s.created.Status.Snapshot.DurableSnapshot.State = ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED
		return nil, status.Error(codes.AlreadyExists, "copy finished")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestRecoveryWaitsForLiveBackendCopyBeforeInspectingPendingTag(t *testing.T) {
	cp := &apiv1.Checkpoint{Id: "11111111-1111-4111-8111-111111111111", SessionId: "22222222-2222-4222-8222-222222222222", State: apiv1.CheckpointState_CHECKPOINT_STATE_CREATING}
	base := &testTags{snapshotURI: "s3://snapshots/source"}
	_, err := base.CreateTag(t.Context(), "team-a", "checkpoint-"+cp.Id, "session-"+cp.SessionId)
	require.NoError(t, err)
	base.created.Status.Snapshot.DurableSnapshot.State = ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_PENDING
	release := make(chan struct{})
	tags := &serializedCopyTags{testTags: base, entered: make(chan struct{}), release: release}
	store := &testStore{prepared: cp, snapshot: &database.SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/source", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}, checkpointOwner: "alice"}
	service := NewService(store, testAuthorizer{}, tags, nil)
	op, err := store.GetCheckpointOperation(t.Context(), cp.Id, "alice")
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() { result <- service.recoverOperation(t.Context(), op) }()
	select {
	case <-tags.entered:
	case <-time.After(time.Second):
		t.Fatal("backend synchronization was skipped")
	}
	select {
	case err := <-result:
		t.Fatalf("recovered before the live copy ended: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_CREATING, cp.State)
	require.Zero(t, base.deleteCalls)
	require.Zero(t, base.getCalls, "a pending tag must not be interpreted before the lease barrier")
	close(release)
	require.NoError(t, <-result)
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_READY, cp.State)
	require.Zero(t, base.deleteCalls)
}
