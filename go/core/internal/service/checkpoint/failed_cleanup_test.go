package checkpoint

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestFailedTagsAreCollectedWithoutExposingFailedCheckpoints(t *testing.T) {
	persistence := recoveryStore(t)
	owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	source, head := recoverySource(t, persistence)
	tags := &testTags{snapshotURI: "s3://snapshots/source", createErr: status.Error(codes.Unavailable, "copy interrupted"),
		mutateTag: func(tag *ateapipb.Tag) {
			tag.Status.Snapshot.DurableSnapshot.State = ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_PENDING
		}}
	service := NewService(persistence, testAuthorizer{}, tags, nil)
	request := uuid.NewString()
	_, err := service.Create(owner, source.Id, request, head)
	require.Equal(t, codes.Unavailable, status.Code(err))
	_, err = service.Create(owner, source.Id, request, head)
	assertFailedCheckpointOutcome(t, err)
	pending, err := persistence.ListPendingCheckpoints(owner, "", 100)
	require.NoError(t, err)
	require.Len(t, pending, 1, "a new FAILED tag is immediately eligible for collection")
	id := pending[0].Checkpoint.Id
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_FAILED, pending[0].Checkpoint.State)
	_, err = service.Get(owner, id)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	listed, err := service.List(owner, ListRequest{SessionID: source.Id})
	require.NoError(t, err)
	require.Empty(t, listed.Checkpoints)
	_, err = service.Fork(owner, id, uuid.NewString())
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	_, _, err = persistence.ForkSession(owner, id, "alice", uuid.NewString(), uuid.NewString())
	require.ErrorIs(t, err, database.ErrNotFound)
	tags.deleteErr = status.Error(codes.PermissionDenied, "storage denied")
	require.NoError(t, NewRecoveryWorker(service).sweep(owner))
	retained, err := persistence.GetCheckpointOperation(owner, id, "alice")
	require.NoError(t, err)
	require.Equal(t, "tag-uid", retained.TagUID)
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_FAILED, retained.Checkpoint.State)
	tags.deleteErr = nil
	lost := &lostFailedCollection{Client: persistence}
	require.NoError(t, NewRecoveryWorker(NewService(lost, testAuthorizer{}, tags, nil)).sweep(owner))
	require.Equal(t, 1, lost.calls, "the UID-clear reply is lost after the real write commits")
	collected, err := persistence.GetCheckpointOperation(owner, id, "alice")
	require.NoError(t, err)
	require.Empty(t, collected.TagUID)
	require.Equal(t, retained.Checkpoint, collected.Checkpoint, "collection preserves the terminal request result")
	require.Nil(t, tags.created)
	_, err = service.Create(owner, source.Id, request, head)
	assertFailedCheckpointOutcome(t, err)
	require.Equal(t, 2, tags.createCalls, "terminal retries must not copy again")
	pending, err = persistence.ListPendingCheckpoints(owner, "", 100)
	require.NoError(t, err)
	require.Empty(t, pending, "collected FAILED rows must not be retried")
	require.NoError(t, NewRecoveryWorker(service).sweep(owner))
	require.Equal(t, 2, tags.createCalls)
}

type lostFailedCollection struct {
	*database.Client
	calls int
}

func (s *lostFailedCollection) ClearFailedCheckpointTag(ctx context.Context, id, owner, uid string, executor uuid.UUID) error {
	if err := s.Client.ClearFailedCheckpointTag(ctx, id, owner, uid, executor); err != nil {
		return err
	}
	s.calls++
	return errors.New("cleanup reply lost")
}

func TestFreshInvalidSnapshotDoesNotStartCopy(t *testing.T) {
	persistence := recoveryStore(t)
	owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	source, head := recoverySource(t, persistence)
	tags := &testTags{snapshotURI: "s3://snapshots/changed"}
	service := NewService(persistence, testAuthorizer{}, tags, nil)
	_, err := service.Create(owner, source.Id, uuid.NewString(), head)
	assertFailedCheckpointOutcome(t, err)
	require.Zero(t, tags.createCalls)
	pending, err := persistence.ListPendingCheckpoints(owner, "", 100)
	require.NoError(t, err)
	require.Empty(t, pending, "there is no tag to collect")
	_, err = persistence.BeginSessionOperation(owner, source.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
	require.NoError(t, err, "failed verification releases source admission")
}

func TestRecoveryRetainsPriorTagWhenSourceChanged(t *testing.T) {
	persistence := recoveryStore(t)
	owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	source, head := recoverySource(t, persistence)
	tags := &testTags{snapshotURI: "s3://snapshots/source", createErr: status.Error(codes.Unavailable, "copy reply lost"),
		mutateTag: func(tag *ateapipb.Tag) {
			tag.Status.Snapshot.DurableSnapshot.State = ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_PENDING
		}}
	service := NewService(persistence, testAuthorizer{}, tags, nil)
	request := uuid.NewString()
	_, err := service.Create(owner, source.Id, request, head)
	require.Equal(t, codes.Unavailable, status.Code(err))
	tags.snapshotURI = "s3://snapshots/changed"
	_, err = service.Create(owner, source.Id, request, head)
	assertFailedCheckpointOutcome(t, err)
	require.Equal(t, 2, tags.createCalls, "recovery must serialize with the prior copy")
	pending, err := persistence.ListPendingCheckpoints(owner, "", 100)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, "tag-uid", pending[0].TagUID)
	tags.created.Metadata.Uid = "replacement-uid"
	require.NoError(t, NewRecoveryWorker(service).sweep(owner))
	require.Zero(t, tags.deleteCalls, "collection must never delete a replacement tag")
	retained, err := persistence.GetCheckpointOperation(owner, pending[0].Checkpoint.Id, "alice")
	require.NoError(t, err)
	require.Equal(t, "tag-uid", retained.TagUID)
}

func TestFailedCollectionSurvivesSourceDeletion(t *testing.T) {
	f := newFailedCollectionFixture(t)
	source := f.operation.Checkpoint.SessionId
	work, err := f.persistence.BeginSessionOperation(f.owner, source, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(f.owner, time.Minute)
	defer cancel()
	executor := uuid.New()
	acquired, err := f.persistence.ClaimSessionOperation(ctx, source, work.ID, executor)
	require.NoError(t, err)
	require.True(t, acquired)
	deleted, err := f.persistence.FinishSessionOperation(ctx, source, work.ID, executor, "", "", "")
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETED, deleted.State)
	f.tags.actorErr = status.Error(codes.NotFound, "source no longer exists")
	require.NoError(t, NewRecoveryWorker(f.service).sweep(f.owner))
	collected, err := f.persistence.GetCheckpointOperation(f.owner, f.operation.Checkpoint.Id, "alice")
	require.NoError(t, err)
	require.Empty(t, collected.TagUID)
	require.Equal(t, f.operation.Checkpoint, collected.Checkpoint)
	_, err = f.service.Create(f.owner, source, f.request, f.operation.Checkpoint.HeadTaskId)
	assertFailedCheckpointOutcome(t, err)
	require.Equal(t, 1, f.tags.createCalls, "replay after source deletion must not copy again")
}

func TestFailedCollectionObservesReplacementCompletion(t *testing.T) {
	for _, outcome := range []string{"collected", "deleted"} {
		t.Run("Given replacement "+outcome+", when stale cleanup finishes, assert that completed work is observed without an obsolete release", func(t *testing.T) {
			f := newFailedCollectionFixture(t)
			gate := &checkpointCompletionGate{Client: f.persistence}
			gate.beforeFailedClear = func(ctx context.Context, id, owner, uid string, executor uuid.UUID) {
				require.NoError(t, f.persistence.ReleaseCheckpointOperation(ctx, id, executor))
				replacement := uuid.New()
				if outcome == "deleted" {
					_, err := f.persistence.BeginDeleteSessionCheckpoint(ctx, id, owner, replacement)
					require.NoError(t, err)
					require.NoError(t, f.persistence.DeleteClaimedSessionCheckpoint(ctx, id, owner, replacement))
					return
				}
				_, err := f.persistence.ClaimCheckpointOperation(ctx, id, owner, replacement)
				require.NoError(t, err)
				require.NoError(t, f.persistence.ClearFailedCheckpointTag(ctx, id, owner, uid, replacement))
			}
			releases := &checkpointReleaseFailure{store: gate}
			service := NewService(releases, testAuthorizer{}, f.tags, nil)
			require.NoError(t, service.recoverOperation(f.owner, f.operation), "an obsolete release failure must not mask completed cleanup")
			require.Equal(t, 1, gate.conflicts, "the real store must reject the original executor")
			require.Zero(t, releases.calls)
			require.Equal(t, 1, f.tags.createCalls)
		})
	}
}

func TestFailedCollectionPreservesReplacementClaim(t *testing.T) {
	f := newFailedCollectionFixture(t)
	replacement := uuid.New()
	gate := &checkpointCompletionGate{Client: f.persistence}
	gate.beforeFailedClear = func(ctx context.Context, id, owner, _ string, executor uuid.UUID) {
		require.NoError(t, f.persistence.ReleaseCheckpointOperation(ctx, id, executor))
		_, err := f.persistence.ClaimCheckpointOperation(ctx, id, owner, replacement)
		require.NoError(t, err)
	}
	service := NewService(gate, testAuthorizer{}, f.tags, nil)
	err := service.recoverOperation(f.owner, f.operation)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeAborted), "%v", err)
	require.Equal(t, 1, gate.conflicts)
	ctx, cancel := context.WithTimeout(f.owner, time.Minute)
	defer cancel()
	_, err = f.persistence.ClaimCheckpointOperation(ctx, f.operation.Checkpoint.Id, "alice", uuid.New())
	require.ErrorIs(t, err, database.ErrConflict, "the stale executor's real release must preserve replacement ownership")
	require.NoError(t, f.persistence.ReleaseCheckpointOperation(ctx, f.operation.Checkpoint.Id, replacement))
	require.NoError(t, NewRecoveryWorker(f.service).sweep(f.owner))
	collected, err := f.persistence.GetCheckpointOperation(f.owner, f.operation.Checkpoint.Id, "alice")
	require.NoError(t, err)
	require.Empty(t, collected.TagUID, "a subsequent sweep must finish after the replacement retires")
	require.Equal(t, 1, f.tags.createCalls)
}

type failedCollectionFixture struct {
	persistence *database.Client
	owner       context.Context
	service     *Service
	tags        *testTags
	operation   *database.CheckpointOperation
	request     string
}

func newFailedCollectionFixture(t *testing.T) *failedCollectionFixture {
	t.Helper()
	persistence := recoveryStore(t)
	owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	source, head := recoverySource(t, persistence)
	tags := &testTags{snapshotURI: "s3://snapshots/source", mutateTag: func(tag *ateapipb.Tag) {
		tag.Status.Snapshot.DurableSnapshot.State = ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_PENDING
	}}
	service := NewService(persistence, testAuthorizer{}, tags, nil)
	request := uuid.NewString()
	_, err := service.Create(owner, source.Id, request, head)
	assertFailedCheckpointOutcome(t, err)
	pending, err := persistence.ListPendingCheckpoints(owner, "", 100)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	return &failedCollectionFixture{persistence: persistence, owner: owner, service: service,
		tags: tags, operation: pending[0], request: request}
}

func assertFailedCheckpointOutcome(t *testing.T, err error) {
	t.Helper()
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Empty(t, status.Convert(err).Details(), "internal failure must not add public discovery identity")
}
