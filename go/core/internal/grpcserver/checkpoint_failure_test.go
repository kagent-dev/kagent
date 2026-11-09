package grpcserver

import (
	"context"
	"crypto/sha256"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/internal/service/checkpoint"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestFailedCheckpointIdentitySurvivesGRPCMapping(t *testing.T) {
	dsn := dbtest.StartT(context.WithoutCancel(t.Context()), t)
	dbtest.MigrateT(t, dsn, false)
	pool, err := database.Connect(t.Context(), &database.PostgresConfig{Pool: dbtest.PoolConfig(dsn)})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := database.NewClient(pool, "public")
	session := createTaskStoreSession(t, store)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("checkpoint boundary"))
	message.ContextID = session.ContextId
	task := a2a.NewSubmittedTask(message, message)
	task.Status.State = a2a.TaskStateCompleted
	digest := sha256.Sum256([]byte(task.ID))
	version, err := store.CreateRuntimeTask(t.Context(), session.Id, digest[:], task, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
	boundary, err := store.ClaimSessionQuiescence(t.Context())
	require.NoError(t, err)
	require.NoError(t, store.FinishSessionQuiescence(t.Context(), boundary, &database.SessionTaskSnapshot{
		Atespace: "team-a", URI: "s3://snapshots/source", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}))
	ctx, cancel := context.WithTimeout(t.Context(), database.RuntimeOperationTimeout)
	defer cancel()
	executor, requestID := uuid.New(), uuid.NewString()
	failed, _, err := store.ReserveSessionCheckpoint(ctx, &apiv1.Checkpoint{
		Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID)}, "alice", requestID, executor)
	require.NoError(t, err)
	failed, err = store.FinalizeClaimedSessionCheckpoint(ctx, failed, executor, "partial-tag-uid", "", "private legacy backend detail")
	require.NoError(t, err)
	tags := &checkpointFailureTags{tag: &ateapipb.Tag{Metadata: &ateapipb.ResourceMetadata{
		Atespace: "team-a", Name: "checkpoint-" + failed.Id, Uid: "partial-tag-uid"}}}
	connection := newConfigurationConnection(t, nil, func(config *Config) {
		config.CheckpointService = checkpoint.NewService(store, &auth.NoopAuthorizer{}, tags, nil)
	})
	client := apiv1.NewCheckpointServiceClient(connection)
	ctx = metadata.AppendToOutgoingContext(ctx, "x-user-id", "alice")
	_, err = client.CreateCheckpoint(ctx, &apiv1.CreateCheckpointRequest{
		SessionId: failed.SessionId, RequestId: requestID, ExpectedHeadTaskId: failed.HeadTaskId})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	var reason, id string
	for _, detail := range status.Convert(err).Details() {
		switch info := detail.(type) {
		case *errdetails.ErrorInfo:
			require.Equal(t, "kagent.dev", info.Domain)
			reason = info.Reason
		case *errdetails.ResourceInfo:
			id = info.ResourceName
		}
	}
	require.Equal(t, "KAGENT_CHECKPOINT_FAILED", reason)
	require.Equal(t, failed.Id, id)
	require.NotContains(t, err.Error(), "private")
	read, err := client.GetCheckpoint(ctx, &apiv1.GetCheckpointRequest{CheckpointId: id})
	require.NoError(t, err)
	require.Equal(t, failed.State, read.Checkpoint.State)
	require.NotContains(t, read.String(), "private")
	listed, err := client.ListCheckpoints(ctx, &apiv1.ListCheckpointsRequest{SessionId: failed.SessionId})
	require.NoError(t, err)
	require.Len(t, listed.Checkpoints, 1)
	require.Equal(t, id, listed.Checkpoints[0].Id)
	require.NotContains(t, listed.String(), "private")
	other := metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "bob")
	_, err = client.GetCheckpoint(other, &apiv1.GetCheckpointRequest{CheckpointId: id})
	require.Equal(t, codes.NotFound, status.Code(err))
	_, err = client.DeleteCheckpoint(other, &apiv1.DeleteCheckpointRequest{CheckpointId: id})
	require.Equal(t, codes.NotFound, status.Code(err))
	_, err = client.ForkSession(ctx, &apiv1.ForkSessionRequest{CheckpointId: id, RequestId: uuid.NewString()})
	require.Equal(t, codes.NotFound, status.Code(err))
	_, err = client.DeleteCheckpoint(ctx, &apiv1.DeleteCheckpointRequest{CheckpointId: id})
	require.NoError(t, err)
	require.Nil(t, tags.tag)
	_, err = store.GetCheckpointOperation(t.Context(), id, "alice")
	require.ErrorIs(t, err, database.ErrNotFound)
	listed, err = client.ListCheckpoints(ctx, &apiv1.ListCheckpointsRequest{SessionId: failed.SessionId})
	require.NoError(t, err)
	require.Empty(t, listed.Checkpoints)
}

// Only the external Tag API is replaced: the generated client, interceptors,
// service and PostgreSQL use their production implementations.
type checkpointFailureTags struct {
	*substrate.Client
	tag *ateapipb.Tag
}

func (s *checkpointFailureTags) GetTag(context.Context, string, string) (*ateapipb.Tag, error) {
	return s.tag, nil
}

func (s *checkpointFailureTags) DeleteTag(_ context.Context, atespace, name, uid string) error {
	if s.tag.GetMetadata().GetAtespace() != atespace || s.tag.GetMetadata().GetName() != name || s.tag.GetMetadata().GetUid() != uid {
		return status.Error(codes.FailedPrecondition, "tag identity changed")
	}
	s.tag = nil
	return nil
}
