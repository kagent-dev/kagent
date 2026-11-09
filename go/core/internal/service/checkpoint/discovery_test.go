package checkpoint

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestOwnerDiscoversFailedCheckpointAndDeletesPartialTag(t *testing.T) {
	persistence := reviewStore(t)
	owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	other := auth.AuthSessionTo(t.Context(), testSession{userID: "bob"})
	source, head := recoverySource(t, persistence)
	tags := &testTags{snapshotURI: "s3://snapshots/source", createErr: status.Error(codes.Unavailable, "copy interrupted"),
		mutateTag: func(tag *ateapipb.Tag) {
			tag.Status.Snapshot.DurableSnapshot.State = ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_PENDING
		}}
	service := NewService(persistence, testAuthorizer{}, tags, nil)
	request := uuid.NewString()
	created, err := service.Create(owner, source.Id, request, head)
	require.Equal(t, codes.Unavailable, status.Code(err), "an uncertain attempt stays retryable")
	require.Nil(t, created)
	pending, err := service.List(owner, ListRequest{SessionID: source.Id})
	require.NoError(t, err)
	require.Empty(t, pending.Checkpoints, "CREATING remains hidden")

	_, terminalErr := service.Create(owner, source.Id, request, head)
	require.Error(t, terminalErr)
	// Discover the identity through public methods only. Neither the backend
	// fixture nor an internal lifecycle read supplies the ID used for cleanup.
	listed, err := service.List(owner, ListRequest{SessionID: source.Id, PageSize: 1})
	require.NoError(t, err)
	require.Len(t, listed.Checkpoints, 1)
	failed := listed.Checkpoints[0]
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_FAILED, failed.State)
	require.Empty(t, listed.NextPageToken)
	assertFailedCheckpointIdentity(t, terminalErr, failed.Id)
	read, err := service.Get(owner, failed.Id)
	require.NoError(t, err)
	require.True(t, proto.Equal(failed, read))
	_, err = service.Create(owner, source.Id, request, head)
	assertFailedCheckpointIdentity(t, err, failed.Id)
	require.Equal(t, 2, tags.createCalls, "terminal retries must not repeat copying")

	_, err = service.Get(other, failed.Id)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	foreign, err := service.List(other, ListRequest{SessionID: source.Id})
	require.NoError(t, err)
	require.Empty(t, foreign.Checkpoints)
	require.True(t, serviceerrors.IsCode(service.Delete(other, failed.Id), serviceerrors.CodeNotFound))
	_, err = service.Fork(owner, failed.Id, uuid.NewString())
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound), "FAILED must never become a fork source")
	_, _, err = persistence.ForkSession(owner, failed.Id, "alice", uuid.NewString(), uuid.NewString())
	require.ErrorIs(t, err, database.ErrNotFound, "the store must independently reject FAILED forks")

	tags.deleteErr = status.Error(codes.PermissionDenied, "private object-store configuration")
	require.Equal(t, codes.PermissionDenied, status.Code(service.Delete(owner, failed.Id)))
	retained, err := persistence.GetCheckpointOperation(owner, failed.Id, "alice")
	require.NoError(t, err)
	require.Equal(t, "tag-uid", retained.TagUID, "failed cleanup must retain its deletion handle")
	_, err = service.Get(owner, failed.Id)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound), "DELETING remains hidden")
	tags.deleteErr = nil
	require.NoError(t, NewService(persistence, testAuthorizer{}, tags, nil).Delete(owner, failed.Id))
	require.Nil(t, tags.created)
	_, err = persistence.GetCheckpointOperation(owner, failed.Id, "alice")
	require.ErrorIs(t, err, database.ErrNotFound, "successful cleanup must release the retained row and references")
	listed, err = service.List(owner, ListRequest{SessionID: source.Id})
	require.NoError(t, err)
	require.Empty(t, listed.Checkpoints)
}

func TestFailedCheckpointReadsHideLegacyBackendDetails(t *testing.T) {
	persistence := reviewStore(t)
	owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	source, head := recoverySource(t, persistence)
	ctx, cancel := context.WithTimeout(owner, database.RuntimeOperationTimeout)
	defer cancel()
	executor := uuid.New()
	checkpoint, _, err := persistence.ReserveSessionCheckpoint(ctx,
		&apiv1.Checkpoint{Id: uuid.NewString(), SessionId: source.Id, HeadTaskId: head}, "alice", uuid.NewString(), executor)
	require.NoError(t, err)
	const privateFailure = "private bucket and credentials in a legacy failure"
	_, err = persistence.FinalizeClaimedSessionCheckpoint(ctx, checkpoint, executor, "legacy-tag", "", privateFailure)
	require.NoError(t, err)
	service := NewService(persistence, testAuthorizer{}, &testTags{}, nil)
	read, err := service.Get(owner, checkpoint.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_FAILED, read.State)
	require.NotEmpty(t, read.GetFailure().GetMessage())
	require.NotContains(t, read.String(), "private")
	listed, err := service.List(owner, ListRequest{SessionID: source.Id})
	require.NoError(t, err)
	require.Len(t, listed.Checkpoints, 1)
	require.True(t, proto.Equal(read, listed.Checkpoints[0]))
	stored, err := persistence.GetCheckpointOperation(owner, checkpoint.Id, "alice")
	require.NoError(t, err)
	require.Equal(t, privateFailure, stored.Checkpoint.Failure.Message, "public projection must not rewrite durable data")
	require.NoError(t, service.Delete(owner, checkpoint.Id))
}

func assertFailedCheckpointIdentity(t *testing.T, err error, id string) {
	t.Helper()
	require.Equal(t, codes.FailedPrecondition, status.Code(err), "a terminal failure must not invite transient retries")
	var reason, resource string
	for _, detail := range status.Convert(err).Details() {
		switch info := detail.(type) {
		case *errdetails.ErrorInfo:
			require.Equal(t, "kagent.dev", info.Domain)
			reason = info.Reason
		case *errdetails.ResourceInfo:
			require.Equal(t, "kagent.api.v1.Checkpoint", info.ResourceType)
			resource = info.ResourceName
		}
	}
	require.Equal(t, "KAGENT_CHECKPOINT_FAILED", reason)
	require.Equal(t, id, resource)
	require.NotContains(t, err.Error(), "private")
}
