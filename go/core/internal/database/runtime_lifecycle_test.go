package database

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestRuntimeLifecycleStoresSharedValues(t *testing.T) {
	pool := setupTestDB(t)
	client := NewClient(pool)
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateSession(t.Context(), newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), "create")
	require.NoError(t, err)
	assertStored := func(state, operation string) {
		t.Helper()
		var storedState, storedOperation string
		require.NoError(t, pool.QueryRow(t.Context(), "SELECT state, operation FROM runtime_instance WHERE id = $1", instance.Id).Scan(&storedState, &storedOperation))
		require.Equal(t, state, storedState)
		require.Equal(t, operation, storedOperation)
	}
	assertStored("RUNTIME_STATE_CREATING", "RUNTIME_OPERATION_CREATE")
	instance, err = markSessionReady(t.Context(), client, instance.Id, "runtime.example")
	require.NoError(t, err)
	assertStored("RUNTIME_STATE_READY", "RUNTIME_OPERATION_NONE")

	require.NoError(t, deleteSession(t.Context(), client, instance.Id))
	assertStored("RUNTIME_STATE_DELETED", "RUNTIME_OPERATION_NONE")
}

func TestRuntimeLifecycleRejectsUnknownStoredValues(t *testing.T) {
	for _, row := range []runtimeInstanceRow{
		{State: "UNRECOGNIZED", Operation: "RUNTIME_OPERATION_NONE"},
		{State: "RUNTIME_STATE_UNSPECIFIED", Operation: "RUNTIME_OPERATION_NONE"},
		{State: "RUNTIME_STATE_READY", Operation: "UNRECOGNIZED"},
	} {
		_, _, err := row.lifecycle()
		require.Error(t, err)
	}
}

func TestAgentWritesRejectSandboxRevisions(t *testing.T) {
	pool := setupTestDB(t)
	client := NewClient(pool)
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	_, err := pool.Exec(ctx, `
		WITH created AS (
			INSERT INTO runtime_revision (revision, kind, namespace, source_snapshot, actor_template_atespace, actor_template_name, actor_template_uid)
			VALUES ('sandbox-revision', 'sandbox', 'team-a', '{}', 'team-a', 'scratch', 'original-uid') RETURNING revision
		)
		INSERT INTO sandbox_revision (revision, sandbox_template_name, sandbox_template_uid)
		SELECT revision, 'scratch', 'scratch-uid' FROM created
	`)
	require.NoError(t, err)
	// Failed validation must roll back preparation changes and preserve the ready revision.
	err = client.UpsertAgentDefinition(ctx, AgentDefinition{
		Namespace: "team-a", AgentName: "assistant", AgentUID: "assistant-uid",
		DesiredRevision: "sandbox-revision",
	})
	require.ErrorIs(t, err, ErrConflict)
	var desired string
	require.NoError(t, pool.QueryRow(ctx, "SELECT desired_revision FROM agent_definition").Scan(&desired))
	require.Equal(t, "revision", desired)

	revision, err := client.GetRuntimeRevision(ctx, "revision")
	require.NoError(t, err)
	revision.Revision, revision.ActorTemplateUID = "sandbox-revision", "replacement-uid"
	require.ErrorIs(t, client.RecordRuntimeRevision(ctx, *revision, true), ErrConflict)
	var uid string
	require.NoError(t, pool.QueryRow(ctx, "SELECT actor_template_uid FROM runtime_revision WHERE revision = 'sandbox-revision'").Scan(&uid))
	require.Equal(t, "original-uid", uid)
	var count int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM agent_revision WHERE revision = 'sandbox-revision'").Scan(&count))
	require.Zero(t, count)

	require.ErrorIs(t, client.withTx(ctx, func(tx pgx.Tx) error {
		_, err := getAvailableRuntimeRevisionForUpdate(ctx, tx, "sandbox-revision")
		return err
	}), ErrConflict)
}
