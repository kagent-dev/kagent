package database

import (
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
)

func TestRecordRuntimeRevisionPromotesOnlyCurrentActivePair(t *testing.T) {
	pool := setupTestDB(t)
	c := NewClient(pool)
	ctx := t.Context()
	revision := RuntimeRevision{
		Revision: "first", Namespace: "team", AgentTemplateName: "assistant", AgentTemplateUID: "template-uid",
		HarnessName: "runtime", HarnessUID: "harness-uid", AgentCard: &a2apb.AgentCard{Name: "original"},
		SourceSnapshot: []byte("{}"), EgressDestinations: []string{},
		ActorTemplateAtespace: "team", ActorTemplateName: "first", ActorTemplateUID: "actor-uid",
	}
	pair := AgentTemplateHarnessPair{
		Namespace: revision.Namespace, AgentTemplateName: revision.AgentTemplateName, AgentTemplateUID: revision.AgentTemplateUID,
		HarnessName: revision.HarnessName, HarnessUID: revision.HarnessUID, DesiredRevision: revision.Revision,
	}
	assertAvailableRevision := func(want string) {
		t.Helper()
		instance, _, err := c.CreateAgentInstance(ctx, &apiv1alpha1.AgentInstance{
			Id: uuid.NewString(), Creator: "alice",
			Harness:       &apiv1alpha1.ResourceReference{Namespace: pair.Namespace, Name: pair.HarnessName},
			AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: pair.Namespace, Name: pair.AgentTemplateName},
		}, uuid.NewString())
		if want == "" {
			require.ErrorIs(t, err, ErrNotFound)
			return
		}
		require.NoError(t, err)
		require.Equal(t, want, instance.PreparedRevision)
	}
	require.NoError(t, c.UpsertAgentTemplateHarnessPair(ctx, pair))
	require.NoError(t, c.RecordRuntimeRevision(ctx, revision, false))
	assertAvailableRevision("")
	stored, err := c.GetRuntimeRevision(ctx, revision.Revision)
	require.NoError(t, err)
	require.Equal(t, "original", stored.AgentCard.Name)

	require.NoError(t, c.RecordRuntimeRevision(ctx, revision, true))
	assertAvailableRevision("first")
	pair.DesiredRevision = "second"
	require.NoError(t, c.UpsertAgentTemplateHarnessPair(ctx, pair))
	revision.Revision, revision.ActorTemplateName = "second", "second"
	require.NoError(t, c.RecordRuntimeRevision(ctx, revision, true))
	assertAvailableRevision("second")
	// A delayed report for the old revision must not replace the newer ready revision.
	revision.Revision, revision.ActorTemplateName = "first", "first"
	require.NoError(t, c.RecordRuntimeRevision(ctx, revision, true))
	assertAvailableRevision("second")
	pair.DesiredRevision = "third"
	require.NoError(t, c.UpsertAgentTemplateHarnessPair(ctx, pair))
	require.NoError(t, c.RetireAgentTemplateHarnessPair(ctx, pair.Namespace, pair.AgentTemplateName, pair.HarnessName))
	revision.Revision, revision.ActorTemplateName = "third", "third"
	require.NoError(t, c.RecordRuntimeRevision(ctx, revision, true))
	assertAvailableRevision("")
	// Reviving the pair must retain the last success, not a report made while retired.
	require.NoError(t, c.UpsertAgentTemplateHarnessPair(ctx, pair))
	assertAvailableRevision("second")
}
