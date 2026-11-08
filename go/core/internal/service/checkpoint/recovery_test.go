package checkpoint

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
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
	revision := "revision-" + snapshot.GetActorTemplateUid()
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
	op, err := store.BeginSessionOperation(ctx, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_CREATE, false)
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

func (s *failedCheckpointPublish) FinalizeClaimedSessionCheckpoint(ctx context.Context, _ *apiv1.Checkpoint, _ uuid.UUID, _, _, _ string) (*apiv1.Checkpoint, error) {
	s.once.Do(func() { close(s.entered) })
	return nil, errors.New("publication reply lost")
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

// Fixture for a process that reserved work but exited before calling Substrate.
func (s *Service) beginCreate(ctx context.Context, source, request, head string) (*apiv1.Checkpoint, string, error) {
	ctx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancel()
	executor := uuid.New()
	id, err := uuid.NewV7()
	if err != nil {
		return nil, "", err
	}
	op, err := s.reserveCreate(ctx, source, request, head, id.String(), executor)
	err = errors.Join(err, s.release(ctx, id.String(), executor))
	if err != nil {
		return nil, "", err
	}
	return op.Checkpoint, op.Creator, nil
}

func (s *Service) beginDelete(ctx context.Context, id string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancel()
	owner, err := s.authorize(ctx, auth.VerbDelete, auth.Resource{Type: "Checkpoint", Name: id})
	if err != nil {
		return "", err
	}
	executor := uuid.New()
	_, err = s.store.BeginDeleteSessionCheckpoint(ctx, id, owner, executor)
	return owner, errors.Join(err, s.release(ctx, id, executor))
}

func TestRecoveryWaitsAfterSlowSweep(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprintf("Given a slow sweep with failure=%t, when it finishes, assert that another sweep waits a full interval", failed), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				gate := make(chan struct{})
				persistence := &blockedCheckpointSweep{testStore: &testStore{}, gate: gate, failed: failed}
				worker := NewRecoveryWorker(NewService(persistence, testAuthorizer{}, nil, nil))
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- worker.Start(ctx) }()
				synctest.Wait()
				require.Equal(t, 1, int(persistence.calls.Load()))
				time.Sleep(3 * worker.interval)
				close(gate)
				synctest.Wait()
				require.Equal(t, 1, int(persistence.calls.Load()), "a stale tick must not immediately start another sweep")
				time.Sleep(worker.interval - time.Millisecond)
				synctest.Wait()
				require.Equal(t, 1, int(persistence.calls.Load()))
				time.Sleep(time.Millisecond)
				synctest.Wait()
				require.Equal(t, 2, int(persistence.calls.Load()))
				cancel()
				synctest.Wait()
				require.NoError(t, <-done)
			})
		})
	}
}

type blockedCheckpointSweep struct {
	*testStore
	gate   <-chan struct{}
	calls  atomic.Int32
	failed bool
}

func (s *blockedCheckpointSweep) ListPendingCheckpoints(ctx context.Context, _ string, _ int) ([]*database.CheckpointOperation, error) {
	if s.calls.Add(1) == 1 {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if s.failed {
			return nil, errors.New("sweep unavailable")
		}
	}
	return nil, nil
}
