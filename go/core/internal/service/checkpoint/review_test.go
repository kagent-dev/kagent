package checkpoint

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestCheckpointRequestsAndRecovery(t *testing.T) {
	store := reviewStore(t)
	owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})

	t.Run("Given a ready source, when Create is called without a worker, assert that it returns a retained checkpoint inline", func(t *testing.T) {
		source, head := recoverySource(t, store)
		counted := &countedClaims{Client: store}
		tags := &testTags{snapshotURI: "s3://snapshots/source"}
		service := NewService(counted, testAuthorizer{}, tags, nil)
		request := uuid.NewString()
		cp, err := service.Create(owner, source.Id, request, head)
		require.NoError(t, err)
		require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_READY, cp.State)
		require.Zero(t, counted.claims.Load(), "the reservation INSERT must own the first attempt")
		retained, err := store.GetCheckpointOperation(t.Context(), cp.Id, "alice")
		require.NoError(t, err)
		require.Equal(t, "tag-uid", retained.TagUID)
		require.Equal(t, "s3://tags/checkpoint", retained.Snapshot.URI)
		repeated, err := service.Create(owner, source.Id, request, head)
		require.NoError(t, err)
		require.Equal(t, cp.Id, repeated.Id)
		require.Equal(t, 1, tags.createCalls)
		require.NoError(t, service.Delete(owner, cp.Id))
	})

	for _, later := range []codes.Code{codes.OK, codes.Unavailable} {
		t.Run("Given a denied deletion, when its backend changes to "+later.String()+", assert that a public retry executes the current attempt", func(t *testing.T) {
			source, head := recoverySource(t, store)
			tags := &testTags{snapshotURI: "s3://snapshots/source"}
			service := NewService(store, testAuthorizer{}, tags, nil)
			cp, err := service.Create(owner, source.Id, uuid.NewString(), head)
			require.NoError(t, err)
			tags.deleteErr = status.Error(codes.PermissionDenied, "private bucket and credentials")
			require.Equal(t, codes.PermissionDenied, status.Code(service.Delete(owner, cp.Id)))
			calls := tags.deleteCalls
			tags.deleteErr = nil
			if later != codes.OK {
				tags.deleteErr = status.Error(later, "temporary outage")
			}
			replacement := NewService(store, testAuthorizer{}, tags, nil)
			err = replacement.Delete(owner, cp.Id)
			require.Equal(t, later, status.Code(err))
			require.Equal(t, calls+1, tags.deleteCalls, "retry must issue a new backend attempt")
			if later != codes.OK {
				pending, readErr := store.GetCheckpointOperation(t.Context(), cp.Id, "alice")
				require.NoError(t, readErr)
				require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_DELETING, pending.Checkpoint.State)
				require.Equal(t, "tag-uid", pending.TagUID)
				tags.deleteErr = nil
				require.NoError(t, replacement.Delete(owner, cp.Id))
			}
			_, err = store.GetCheckpointOperation(t.Context(), cp.Id, "alice")
			require.ErrorIs(t, err, database.ErrNotFound)
		})
	}

	t.Run("Given a canceled caller with a live claim, when a replica retries and later takes over, assert that only the owning request copies", func(t *testing.T) {
		source, head := recoverySource(t, store)
		gate := make(chan struct{})
		tags := &lockedTags{base: &testTags{snapshotURI: "s3://snapshots/source"}, gate: gate, entered: make(chan struct{})}
		service := NewService(store, testAuthorizer{}, tags, nil)
		request := uuid.NewString()
		ctx, cancel := context.WithCancel(owner)
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := service.Create(ctx, source.Id, request, head); done <- err }()
		select {
		case <-tags.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("inline copy was never reached")
		}
		follower := NewService(store, testAuthorizer{}, tags, nil)
		_, err := follower.Create(owner, source.Id, request, head)
		require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeAborted), "%v", err)
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
		close(gate)
		cp, err := follower.Create(owner, source.Id, request, head)
		require.NoError(t, err)
		require.Equal(t, 1, tags.base.createCalls)
		require.NoError(t, follower.Delete(owner, cp.Id))
	})

	t.Run("Given abandoned work, when a replacement sweeps, assert that expired creation is published", func(t *testing.T) {
		source, head := recoverySource(t, store)
		tags := &testTags{snapshotURI: "s3://snapshots/source"}
		service := NewService(store, testAuthorizer{}, tags, nil)
		cp, _, err := service.beginCreate(owner, source.Id, uuid.NewString(), head)
		require.NoError(t, err)
		replacement := NewService(store, testAuthorizer{}, tags, nil)
		require.NoError(t, NewRecoveryWorker(replacement).sweep(t.Context()))
		ready, err := store.GetSessionCheckpoint(t.Context(), cp.Id, "alice")
		require.NoError(t, err)
		require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_READY, ready.State)
		require.NoError(t, replacement.Delete(owner, cp.Id))
	})

	t.Run("Given a stopped partial copy, when it fails verification, assert that admission opens while its UID remains deletable after restart", func(t *testing.T) {
		source, head := recoverySource(t, store)
		tags := &testTags{snapshotURI: "s3://snapshots/source", mutateTag: func(tag *ateapipb.Tag) {
			tag.Status.Snapshot.DurableSnapshot.State = ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_PENDING
		}}
		service := NewService(store, testAuthorizer{}, tags, nil)
		request := uuid.NewString()
		_, err := service.Create(owner, source.Id, request, head)
		require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnavailable))
		cp, _, err := service.beginCreate(owner, source.Id, request, head)
		require.NoError(t, err)
		require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_FAILED, cp.State)
		pending, err := store.GetCheckpointOperation(t.Context(), cp.Id, "alice")
		require.NoError(t, err)
		require.Equal(t, "tag-uid", pending.TagUID)
		require.Equal(t, "s3://snapshots/source", pending.Snapshot.URI, "pending copy has no completed tag URI")
		_, err = store.GetSessionCheckpointForkSource(t.Context(), cp.Id, "alice")
		require.ErrorIs(t, err, database.ErrNotFound)
		_, err = store.BeginSessionOperation(t.Context(), source.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
		require.NoError(t, err, "the serialized copy has stopped; source admission must open")
		tags.deleteErr = status.Error(codes.PermissionDenied, "storage denied")
		require.Equal(t, codes.PermissionDenied, status.Code(service.Delete(owner, cp.Id)))
		retained, err := store.GetCheckpointOperation(t.Context(), cp.Id, "alice")
		require.NoError(t, err)
		require.Equal(t, "tag-uid", retained.TagUID)
		tags.deleteErr = nil
		require.NoError(t, NewService(store, testAuthorizer{}, tags, nil).Delete(owner, cp.Id))
		require.Nil(t, tags.created)
	})

	t.Run("Given lost create and publication replies, when the same request retries, assert that it adopts the retained tag", func(t *testing.T) {
		for _, publishFailure := range []bool{false, true} {
			source, head := recoverySource(t, store)
			tags := &testTags{snapshotURI: "s3://snapshots/source"}
			var persistence storeInterface = store
			if publishFailure {
				persistence = &failedCheckpointPublish{Client: store, entered: make(chan struct{})}
			} else {
				tags.createErr = status.Error(codes.Unavailable, "copy reply lost")
			}
			service := NewService(persistence, testAuthorizer{}, tags, nil)
			request := uuid.NewString()
			_, err := service.Create(owner, source.Id, request, head)
			require.Error(t, err)
			require.NotNil(t, tags.created)
			uid := tags.created.Metadata.Uid
			replacement := NewService(store, testAuthorizer{}, tags, nil)
			cp, err := replacement.Create(owner, source.Id, request, head)
			require.NoError(t, err)
			op, err := store.GetCheckpointOperation(t.Context(), cp.Id, "alice")
			require.NoError(t, err)
			require.Equal(t, uid, op.TagUID)
			require.NoError(t, replacement.Delete(owner, cp.Id))
		}
	})

	t.Run("Given a lost delete reply, when a replacement retries, assert that backend absence completes deletion", func(t *testing.T) {
		source, head := recoverySource(t, store)
		tags := &testTags{snapshotURI: "s3://snapshots/source", deleteResponseErr: status.Error(codes.Unavailable, "delete reply lost")}
		service := NewService(store, testAuthorizer{}, tags, nil)
		cp, err := service.Create(owner, source.Id, uuid.NewString(), head)
		require.NoError(t, err)
		require.Equal(t, codes.Unavailable, status.Code(service.Delete(owner, cp.Id)))
		require.Nil(t, tags.created)
		require.NoError(t, NewService(store, testAuthorizer{}, tags, nil).Delete(owner, cp.Id))
		require.Equal(t, 1, tags.deleteCalls)
	})

	t.Run("Given a lost claim reply, when the request retries, assert that release makes it immediately reclaimable", func(t *testing.T) {
		source, head := recoverySource(t, store)
		tags := &testTags{snapshotURI: "s3://snapshots/source"}
		service := NewService(store, testAuthorizer{}, tags, nil)
		request := uuid.NewString()
		cp, _, err := service.beginCreate(owner, source.Id, request, head)
		require.NoError(t, err)
		lost := NewService(&lostCheckpointClaim{Client: store}, testAuthorizer{}, tags, nil)
		_, err = lost.Create(owner, source.Id, request, head)
		require.Equal(t, codes.Unavailable, status.Code(err))
		require.Zero(t, tags.createCalls)
		ready, err := service.Create(owner, source.Id, request, head)
		require.NoError(t, err)
		require.Equal(t, cp.Id, ready.Id)
		require.NoError(t, service.Delete(owner, cp.Id))
	})
	t.Run("Given an executor paused after claiming, when its deadline passes and a replacement succeeds, assert that the stale executor cannot copy", func(t *testing.T) {
		source, head := recoverySource(t, store)
		tags := &testTags{snapshotURI: "s3://snapshots/source"}
		service := NewService(store, testAuthorizer{}, tags, nil)
		request := uuid.NewString()
		cp, _, err := service.beginCreate(owner, source.Id, request, head)
		require.NoError(t, err)
		resume := make(chan struct{})
		paused := &pausedCheckpointClaim{Client: store, claimed: make(chan struct{}), resume: resume}
		old := NewService(paused, testAuthorizer{}, tags, nil)
		ctx, cancel := context.WithTimeout(owner, 80*time.Millisecond)
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := old.Create(ctx, source.Id, request, head); done <- err }()
		select {
		case <-paused.claimed:
		case <-time.After(5 * time.Second):
			t.Fatal("claim never reached")
		}
		<-ctx.Done()
		ready, err := service.Create(owner, source.Id, request, head)
		require.NoError(t, err)
		require.Equal(t, cp.Id, ready.Id)
		close(resume)
		require.ErrorIs(t, <-done, context.DeadlineExceeded)
		require.Equal(t, 1, tags.createCalls)
		require.NoError(t, service.Delete(owner, cp.Id))
	})
	t.Run("Given a lost reservation reply, when the same request retries, assert that it reclaims the committed reservation immediately", func(t *testing.T) {
		source, head := recoverySource(t, store)
		tags := &testTags{snapshotURI: "s3://snapshots/source"}
		request := uuid.NewString()
		lost := NewService(&lostCheckpointReservation{Client: store}, testAuthorizer{}, tags, nil)
		_, err := lost.Create(owner, source.Id, request, head)
		require.Error(t, err)
		require.Zero(t, tags.createCalls)
		replacement := NewService(store, testAuthorizer{}, tags, nil)
		cp, err := replacement.Create(owner, source.Id, request, head)
		require.NoError(t, err)
		require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_READY, cp.State)
		require.NoError(t, replacement.Delete(owner, cp.Id))
	})

	t.Run("Given a lost successful finalization reply, when Create retries, assert that the published checkpoint is returned without copying again", func(t *testing.T) {
		source, head := recoverySource(t, store)
		tags := &testTags{snapshotURI: "s3://snapshots/source"}
		request := uuid.NewString()
		lost := NewService(&lostCheckpointFinalization{Client: store}, testAuthorizer{}, tags, nil)
		_, err := lost.Create(owner, source.Id, request, head)
		require.Error(t, err)
		replacement := NewService(store, testAuthorizer{}, tags, nil)
		cp, err := replacement.Create(owner, source.Id, request, head)
		require.NoError(t, err)
		require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_READY, cp.State)
		require.Equal(t, 1, tags.createCalls)
		require.NoError(t, replacement.Delete(owner, cp.Id))
	})
}

// Alias prevents the fixture's local store variable from shadowing the interface.
type storeInterface = store

type countedClaims struct {
	*database.Client
	claims atomic.Int64
}

func (s *countedClaims) ClaimCheckpointOperation(ctx context.Context, id, owner string, executor uuid.UUID) (*database.CheckpointOperation, error) {
	s.claims.Add(1)
	return s.Client.ClaimCheckpointOperation(ctx, id, owner, executor)
}

func reviewStore(t *testing.T) *database.Client {
	t.Helper()
	dsn := dbtest.StartT(context.WithoutCancel(t.Context()), t)
	require.NoError(t, dbtest.Migrate(dsn, false))
	pool, err := database.Connect(t.Context(), &database.PostgresConfig{Pool: dbtest.PoolConfig(dsn), Schema: "public"})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return database.NewClient(pool, "public")
}

func TestPublicBackendErrorsHideConfiguration(t *testing.T) {
	tags := &testTags{snapshotURI: "s3://snapshots/snapshot-1", createErr: status.Error(codes.PermissionDenied, "secret endpoint and bucket")}
	service := NewService(&testStore{}, testAuthorizer{}, tags, nil)
	_, err := service.Create(auth.AuthSessionTo(t.Context(), testSession{userID: "alice"}), uuid.NewString(), "request", "head")
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.NotContains(t, err.Error(), "secret endpoint")
	require.False(t, errors.Is(err, database.ErrConflict))
}

func TestPublicDeleteReportsCurrentBackendStatus(t *testing.T) {
	persistence := reviewStore(t)
	owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	for _, operation := range []string{"lookup", "delete"} {
		for _, code := range []codes.Code{codes.FailedPrecondition, codes.InvalidArgument, codes.PermissionDenied, codes.Unauthenticated} {
			t.Run(operation+"/"+code.String(), func(t *testing.T) {
				source, head := recoverySource(t, persistence)
				tags := &testTags{snapshotURI: "s3://snapshots/source"}
				service := NewService(persistence, testAuthorizer{}, tags, nil)
				cp, err := service.Create(owner, source.Id, uuid.NewString(), head)
				require.NoError(t, err)
				failure := status.Error(code, "private storage endpoint")
				if operation == "lookup" {
					tags.getErr = failure
				} else {
					tags.deleteErr = failure
				}
				err = service.Delete(owner, cp.Id)
				require.Equal(t, code, status.Code(err))
				require.NotContains(t, err.Error(), "private storage")
				pending, err := persistence.GetCheckpointOperation(t.Context(), cp.Id, "alice")
				require.NoError(t, err)
				require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_DELETING, pending.Checkpoint.State)
				require.Equal(t, "tag-uid", pending.TagUID)
				tags.getErr, tags.deleteErr = nil, nil
				require.NoError(t, NewService(persistence, testAuthorizer{}, tags, nil).Delete(owner, cp.Id))
			})
		}
	}
}

func TestPublicCreateRetainsUnverifiedTag(t *testing.T) {
	persistence := reviewStore(t)
	source, head := recoverySource(t, persistence)
	tags := &testTags{snapshotURI: "s3://snapshots/source"}
	service := NewService(persistence, testAuthorizer{}, tags, nil)
	owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	request := uuid.NewString()
	cp, _, err := service.beginCreate(owner, source.Id, request, head)
	require.NoError(t, err)
	_, err = tags.CreateTag(t.Context(), "team-a", tagName(cp.Id), "unrelated")
	require.NoError(t, err)
	_, err = service.Create(owner, source.Id, request, head)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeFailedPrecondition))
	pending, err := persistence.GetCheckpointOperation(t.Context(), cp.Id, "alice")
	require.NoError(t, err)
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_CREATING, pending.Checkpoint.State)
	require.Nil(t, pending.Checkpoint.Failure)
	require.Zero(t, tags.deleteCalls)
	for _, force := range []bool{false, true} {
		_, err = persistence.BeginSessionOperation(t.Context(), source.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, force)
		require.ErrorIs(t, err, database.ErrConflict)
	}
	// Substrate source references are immutable. Model removal of the unrelated
	// tag by its owner instead of pretending it can be edited into our tag.
	require.NoError(t, tags.DeleteTag(t.Context(), "team-a", tagName(cp.Id), tags.created.Metadata.Uid))
	ready, err := NewService(persistence, testAuthorizer{}, tags, nil).Create(owner, source.Id, request, head)
	require.NoError(t, err)
	require.Equal(t, cp.Id, ready.Id)
	require.NoError(t, service.Delete(owner, cp.Id))
}

func TestSlowRecoveryDoesNotBlockRequests(t *testing.T) {
	persistence := reviewStore(t)
	slowSource, slowHead := recoverySource(t, persistence)
	fastSource, fastHead := recoverySource(t, persistence)
	owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	gate := make(chan struct{})
	slowTags := &lockedTags{base: &testTags{snapshotURI: "s3://snapshots/source"}, gate: gate, entered: make(chan struct{})}
	slow := NewService(persistence, testAuthorizer{}, slowTags, nil)
	cp, _, err := slow.beginCreate(owner, slowSource.Id, uuid.NewString(), slowHead)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- NewRecoveryWorker(slow).sweep(t.Context()) }()
	select {
	case <-slowTags.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("sweep never began")
	}
	fast := NewService(persistence, testAuthorizer{}, &testTags{snapshotURI: "s3://snapshots/source"}, nil)
	ctx, cancel := context.WithTimeout(owner, time.Second)
	defer cancel()
	ready, err := fast.Create(ctx, fastSource.Id, uuid.NewString(), fastHead)
	require.NoError(t, err, "a request must execute without waiting behind the recovery sweep")
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_READY, ready.State)
	close(gate)
	require.NoError(t, <-done)
	require.NoError(t, slow.Delete(owner, cp.Id))
	require.NoError(t, fast.Delete(owner, ready.Id))
}

// The reservation committed; only its successful reply was lost.
type lostCheckpointReservation struct{ *database.Client }

func (s *lostCheckpointReservation) ReserveSessionCheckpoint(ctx context.Context, checkpoint *apiv1.Checkpoint, owner, request string, executor uuid.UUID) (*apiv1.Checkpoint, *database.SessionTaskSnapshot, error) {
	if _, _, err := s.Client.ReserveSessionCheckpoint(ctx, checkpoint, owner, request, executor); err != nil {
		return nil, nil, err
	}
	return nil, nil, status.Error(codes.Unavailable, "reservation reply lost")
}

type lostCheckpointFinalization struct{ *database.Client }

func (s *lostCheckpointFinalization) FinalizeClaimedSessionCheckpoint(ctx context.Context, checkpoint *apiv1.Checkpoint, executor uuid.UUID, uid, uri, failure string) (*apiv1.Checkpoint, error) {
	if _, err := s.Client.FinalizeClaimedSessionCheckpoint(ctx, checkpoint, executor, uid, uri, failure); err != nil {
		return nil, err
	}
	return nil, status.Error(codes.Unavailable, "finalization reply lost")
}
