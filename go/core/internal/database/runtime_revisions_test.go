package database

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
)

func TestRuntimeRevisionCollectionAfterPairRetirement(t *testing.T) {
	for _, scope := range []string{"pair", "template", "removed harness"} {
		t.Run(scope, func(t *testing.T) {
			client := NewClient(setupTestDB(t))
			ctx := t.Context()
			agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")

			// The successful pair must protect both the listing and deletion paths.
			revisions, err := client.ListUnreferencedRuntimeRevisions(ctx)
			require.NoError(t, err)
			require.Empty(t, revisions)
			require.NoError(t, client.DeleteUnreferencedRuntimeRevision(ctx, "revision"))
			_, err = client.GetRuntimeRevision(ctx, "revision")
			require.NoError(t, err)

			switch scope {
			case "pair":
				err = client.RetireAgentTemplateHarnessPair(ctx, "team-a", "assistant", "kagent")
			case "template":
				err = client.RetireAgentTemplateHarnessPairs(ctx, "team-a", "assistant")
			case "removed harness":
				err = client.RetireOtherAgentTemplateHarnessPairs(ctx, "team-a", "assistant-uid", []string{})
			}
			require.NoError(t, err)
			revisions, err = client.ListUnreferencedRuntimeRevisions(ctx)
			require.NoError(t, err)
			require.Len(t, revisions, 1)
			require.Equal(t, "revision", revisions[0].Revision)
			require.NoError(t, client.DeleteUnreferencedRuntimeRevision(ctx, "revision"))
			_, err = client.GetRuntimeRevision(ctx, "revision")
			require.ErrorIs(t, err, ErrNotFound)
			require.NoError(t, client.DeleteUnreferencedRuntimeRevision(ctx, "revision"))
		})
	}
}

func TestRuntimeRevisionCollectionPreservesInstanceAndCheckpoint(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), "instance")
	require.NoError(t, err)
	_, err = client.MarkAgentInstanceReady(ctx, instance.GetId(), "runtime")
	require.NoError(t, err)
	require.NoError(t, client.RetireAgentTemplateHarnessPairs(ctx, "team-a", "assistant"))

	assertRetained := func() {
		t.Helper()
		revisions, err := client.ListUnreferencedRuntimeRevisions(ctx)
		require.NoError(t, err)
		require.Empty(t, revisions)
		require.NoError(t, client.DeleteUnreferencedRuntimeRevision(ctx, "revision"))
		_, err = client.GetRuntimeRevision(ctx, "revision")
		require.NoError(t, err)
	}
	assertRetained()
	task := newAgentInstanceTask("task", "message")
	_, _, err = client.CreateAgentInstanceTask(ctx, instance.GetId(), []byte("request"), task)
	require.NoError(t, err)
	task.Status.State = a2a.TaskStateCompleted
	require.NoError(t, client.StoreAgentInstanceTaskEvent(ctx, instance.GetId(), task, task,
		&AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/task", ContentScope: "FULL"}))
	checkpoint, _, err := client.ReserveAgentInstanceCheckpoint(ctx, &apiv1alpha1.Checkpoint{
		Id: uuid.NewString(), AgentInstanceId: instance.GetId(),
	}, "alice", "checkpoint")
	require.NoError(t, err)
	require.NoError(t, client.DeleteAgentInstance(ctx, instance.GetId()))

	// A checkpoint retains the runtime throughout creation, use, and deletion,
	// even after the source instance and active template pair are gone.
	assertRetained()
	_, err = client.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.GetId(), "tag-uid", "s3://tags/checkpoint", "")
	require.NoError(t, err)
	assertRetained()
	_, _, err = client.BeginDeleteAgentInstanceCheckpoint(ctx, checkpoint.GetId(), "alice")
	require.NoError(t, err)
	assertRetained()
	require.NoError(t, client.DeleteAgentInstanceCheckpoint(ctx, checkpoint.GetId(), "alice"))
	revisions, err := client.ListUnreferencedRuntimeRevisions(ctx)
	require.NoError(t, err)
	require.Len(t, revisions, 1)
	require.NoError(t, client.DeleteUnreferencedRuntimeRevision(ctx, "revision"))
	_, err = client.GetRuntimeRevision(ctx, "revision")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestRuntimeRevisionPairReplacement(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "old", "assistant", "kagent")
	revision, err := client.GetRuntimeRevision(ctx, "old")
	require.NoError(t, err)
	revision.Revision = "new"
	revision.AgentTemplateUID = "replacement-uid"
	revision.ActorTemplateName = "new-actor-template"
	require.NoError(t, client.UpsertRuntimeRevision(ctx, *revision))
	pair := AgentTemplateHarnessPair{
		Namespace: "team-a", AgentTemplateName: "assistant", AgentTemplateUID: revision.AgentTemplateUID,
		HarnessName: "kagent", HarnessUID: "kagent-uid", DesiredRevision: "new",
	}
	require.NoError(t, client.UpsertAgentTemplateHarnessPair(ctx, pair))
	request := newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", "")
	_, _, err = client.CreateAgentInstance(ctx, request, "instance")
	require.ErrorIs(t, err, ErrNotFound, "a replacement must not select the previous template UID")
	require.NoError(t, client.MarkRuntimeRevisionSuccessful(ctx, pair))

	// Reconciliation of the same identity must preserve its last success while
	// a new desired revision is still preparing.
	pair.DesiredRevision = "pending"
	require.NoError(t, client.UpsertAgentTemplateHarnessPair(ctx, pair))
	instance, _, err := client.CreateAgentInstance(ctx, request, "instance")
	require.NoError(t, err)
	require.Equal(t, "new", instance.GetPreparedRevision())
	revisions, err := client.ListUnreferencedRuntimeRevisions(ctx)
	require.NoError(t, err)
	require.Len(t, revisions, 1)
	require.Equal(t, "old", revisions[0].Revision)
	require.NoError(t, client.DeleteUnreferencedRuntimeRevision(ctx, "old"))
}
