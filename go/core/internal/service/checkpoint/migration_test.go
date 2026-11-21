package checkpoint

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
)

// This is a populated legacy-schema fixture, not a previous-release binary.
// The normal API E2E covers controller restart and request replay separately.
func TestCheckpointRecoveryAcrossClaimMigration(t *testing.T) {
	dsn := dbtest.StartT(context.WithoutCancel(t.Context()), t)
	config := dbtest.PoolConfig(dsn)
	sources := migrations.BuiltinSourcesInSchema(false, "public", "public")
	require.NoError(t, migrations.WithProvider(t.Context(), config, sources[0], func(provider *goose.Provider) error {
		_, err := provider.UpTo(t.Context(), 3)
		return err
	}))
	pool, err := database.Connect(t.Context(), &database.PostgresConfig{Pool: config, Schema: "public"})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	persistence := database.NewClient(pool, "public")
	source, head := recoverySource(t, persistence)
	var historyID uuid.UUID
	var sequence int64
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT s.history_id, t.history_sequence FROM session s
		JOIN session_task t ON t.history_id = s.history_id
		WHERE s.id = $1 AND t.id = $2`, source.Id, head).Scan(&historyID, &sequence))
	created := time.Now().Add(-3 * time.Minute)
	id, err := uuid.NewV7()
	require.NoError(t, err)
	binary.BigEndian.PutUint64(id[:8], uint64(created.UnixMilli())<<16)
	id[6] = 0x70
	request := uuid.NewString()
	legacy := &apiv1.Checkpoint{Id: id.String(), SessionId: source.Id, HeadTaskId: head,
		HistorySequence: uint64(sequence), State: apiv1.CheckpointState_CHECKPOINT_STATE_CREATING,
		CreatedAt: timestamppb.New(created), Name: "legacy checkpoint"}
	data, err := proto.Marshal(legacy)
	require.NoError(t, err)
	// Seed the old table before claim columns exist. All history and revision
	// references come from a settled Session, rather than invented parent rows.
	_, err = pool.CopyFrom(t.Context(), pgx.Identifier{"session_checkpoint"}, []string{
		"id", "source_session_id", "user_id", "request_id", "head_task_id", "history_sequence",
		"snapshot_atespace", "snapshot_uri", "snapshot_fidelity", "source_history_id", "prepared_revision", "state", "data",
	}, pgx.CopyFromRows([][]any{{id, source.Id, "alice", request, head, sequence,
		"team-a", "s3://snapshots/source", "SNAPSHOT_FIDELITY_VOLUMES", historyID, source.PreparedRevision, "CREATING", data}}))
	require.NoError(t, err)
	var claimColumns int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'session_checkpoint'
		AND column_name IN ('executor_id', 'executor_expires_at')`).Scan(&claimColumns))
	require.Zero(t, claimColumns)

	require.NoError(t, migrations.RunUp(t.Context(), config, sources))
	op, err := persistence.GetCheckpointOperation(t.Context(), id.String(), "alice")
	require.NoError(t, err)
	require.True(t, proto.Equal(legacy, op.Checkpoint), "migration must preserve the complete legacy checkpoint")
	var unclaimed bool
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT executor_id IS NULL AND executor_expires_at IS NULL
		FROM session_checkpoint WHERE id = $1`, id).Scan(&unclaimed))
	require.True(t, unclaimed)
	_, err = persistence.BeginSessionOperation(t.Context(), source.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
	require.ErrorIs(t, err, database.ErrConflict, "migration must retain source admission")

	tags := &testTags{snapshotURI: "s3://snapshots/source"}
	service := NewService(persistence, testAuthorizer{}, tags, nil)
	require.NoError(t, NewRecoveryWorker(service).sweep(t.Context()))
	recovered, err := persistence.GetSessionCheckpoint(t.Context(), legacy.Id, "alice")
	require.NoError(t, err, "the sweep must finish legacy work before any client retries")
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_READY, recovered.State)
	owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	replayed, err := service.Create(owner, source.Id, request, head)
	require.NoError(t, err)
	require.Equal(t, legacy.Id, replayed.Id)
	require.Equal(t, legacy.Name, replayed.Name)
	require.Equal(t, legacy.HistorySequence, replayed.HistorySequence)
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_READY, replayed.State)
	require.Equal(t, 1, tags.createCalls, "replaying the old request must not create another copy")
	retained, err := persistence.GetSessionCheckpointForkSource(owner, legacy.Id, "alice")
	require.NoError(t, err)
	require.Equal(t, "tag-uid", retained.TagUID)
	require.Equal(t, "s3://tags/checkpoint", retained.Snapshot.URI)
	require.NoError(t, service.Delete(owner, legacy.Id))
	_, err = persistence.BeginSessionOperation(owner, source.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
	require.NoError(t, err, "recovered work must stop blocking source deletion")
}
