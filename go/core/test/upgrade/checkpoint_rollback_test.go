package upgrade

import (
	"context"
	"crypto/sha256"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
)

// Exercise the actual rollback guard with valid persisted checkpoints. The
// partial tag is a storage fixture; real object cleanup has its own Substrate test.
func TestUpgradeRejectsIncompatibleCheckpointRollback(t *testing.T) {
	dsn := dbtest.StartT(context.WithoutCancel(t.Context()), t)
	require.NoError(t, dbtest.Migrate(dsn, false))
	pool, err := database.Connect(t.Context(), &database.PostgresConfig{Pool: dbtest.PoolConfig(dsn), Schema: "public"})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := database.NewClient(pool, "public")
	source, head := checkpointRollbackSource(t, store)
	ctx, cancel := context.WithTimeout(t.Context(), database.RuntimeOperationTimeout)
	defer cancel()
	require.NoError(t, checkLegacyCheckpointRollback(ctx, pool))
	executor := uuid.New()
	checkpoint, _, err := store.ReserveSessionCheckpoint(ctx,
		&apiv1.Checkpoint{Id: uuid.NewString(), SessionId: source.Id, HeadTaskId: head}, "alice", uuid.NewString(), executor)
	require.NoError(t, err)
	require.Error(t, checkLegacyCheckpointRollback(ctx, pool), "pending creation must block rollback")
	failed, err := store.FinalizeClaimedSessionCheckpoint(ctx, checkpoint, executor, "partial-tag-uid", "", "copy failed")
	require.NoError(t, err)
	retained, err := store.GetCheckpointOperation(ctx, checkpoint.Id, "alice")
	require.NoError(t, err)
	require.Equal(t, "partial-tag-uid", retained.TagUID)
	require.Equal(t, "s3://snapshots/source", retained.Snapshot.URI, "the failed copy has no completed tag URI")
	require.Error(t, checkLegacyCheckpointRollback(ctx, pool), "a retained FAILED tag must block rollback")
	deletion := uuid.New()
	_, err = store.BeginDeleteSessionCheckpoint(ctx, failed.Id, "alice", deletion)
	require.NoError(t, err)
	require.Error(t, checkLegacyCheckpointRollback(ctx, pool), "pending cleanup must block rollback")
	require.NoError(t, store.DeleteClaimedSessionCheckpoint(ctx, failed.Id, "alice", deletion))
	require.NoError(t, checkLegacyCheckpointRollback(ctx, pool), "completed cleanup permits rollback")

	for _, outcome := range []string{"ready", "failed without a tag"} {
		t.Run("Given a checkpoint "+outcome+", when rollback is checked, assert that no incompatible work remains", func(t *testing.T) {
			executor := uuid.New()
			checkpoint, _, err := store.ReserveSessionCheckpoint(ctx,
				&apiv1.Checkpoint{Id: uuid.NewString(), SessionId: source.Id, HeadTaskId: head}, "alice", uuid.NewString(), executor)
			require.NoError(t, err)
			uid, uri, failure := "complete-tag-uid", "s3://tags/complete", ""
			if outcome != "ready" {
				uid, uri, failure = "", "", "source was deleted before copying"
			}
			_, err = store.FinalizeClaimedSessionCheckpoint(ctx, checkpoint, executor, uid, uri, failure)
			require.NoError(t, err)
			require.NoError(t, checkLegacyCheckpointRollback(ctx, pool))
		})
	}
}

func checkpointRollbackSource(t *testing.T, store *database.Client) (*apiv1.Session, string) {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, store.UpsertAgentDefinition(ctx, database.AgentDefinition{
		Namespace: "team-a", AgentName: "assistant", AgentUID: "agent-uid", DesiredRevision: "revision"}))
	_, err := store.RecordRuntimeRevision(ctx, database.RuntimeRevision{
		Revision: "revision", Namespace: "team-a", AgentName: "assistant", AgentUID: "agent-uid",
		SourceSnapshot: []byte("{}"), AgentCard: &a2apb.AgentCard{}, EgressDestinations: []string{},
		ActorTemplateAtespace: "team-a", ActorTemplateName: "runtime", ActorTemplateUID: "template-uid",
	}, database.RecordRevisionOptions{Ready: true})
	require.NoError(t, err)
	session, _, err := store.CreateSession(ctx, &apiv1.Session{Id: uuid.NewString(), Creator: "alice",
		Agent: &apiv1.AgentProvenance{Ref: &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"}}}, uuid.NewString())
	require.NoError(t, err)
	op, err := store.BeginSessionOperation(ctx, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_CREATE, false)
	require.NoError(t, err)
	executor := uuid.New()
	claimed, err := store.ClaimSessionOperation(ctx, session.Id, op.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	session, err = store.FinishSessionOperation(ctx, session.Id, op.ID, executor, "runtime", "actor-uid", "")
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
		&database.SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/source", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}))
	return session, string(task.ID)
}
