package database

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
			require.NoError(t, client.DeleteUnreferencedRuntimeRevision(ctx, "revision", "revision-actor-uid"))
			_, err = client.GetRuntimeRevision(ctx, "revision")
			require.NoError(t, err)

			switch scope {
			case "pair":
				err = client.RetireAllPairIdentities(ctx, "team-a", "assistant", "kagent")
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
			claimed, err := client.ClaimRuntimeRevisionDeletion(ctx, "revision")
			require.NoError(t, err)
			require.NotNil(t, claimed)
			require.NoError(t, client.DeleteUnreferencedRuntimeRevision(ctx, "revision", "revision-actor-uid"))
			_, err = client.GetRuntimeRevision(ctx, "revision")
			require.ErrorIs(t, err, ErrNotFound)
			require.NoError(t, client.DeleteUnreferencedRuntimeRevision(ctx, "revision", "revision-actor-uid"))
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
		claimed, err := client.ClaimRuntimeRevisionDeletion(ctx, "revision")
		require.NoError(t, err)
		require.Nil(t, claimed)
		require.NoError(t, client.DeleteUnreferencedRuntimeRevision(ctx, "revision", "revision-actor-uid"))
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
	claimed, err := client.ClaimRuntimeRevisionDeletion(ctx, "revision")
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, client.DeleteUnreferencedRuntimeRevision(ctx, "revision", "revision-actor-uid"))
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
	claimed, err := client.ClaimRuntimeRevisionDeletion(ctx, "old")
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, client.DeleteUnreferencedRuntimeRevision(ctx, "old", "old-actor-uid"))
}

// Pause the real store's INSERT at either side of reference acquisition. This
// exercises CreateAgentInstance's stale SELECT and commit ordering, without
// replacing its transaction or persistence queries with test-only writes.
type runtimeReferenceBarrier struct {
	afterInsert bool
	reached     chan struct{}
	resume      chan struct{}
}

func (b *runtimeReferenceBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "-- name: InsertAgentInstance :one") {
		if !b.afterInsert {
			close(b.reached)
			<-b.resume
		}
		return context.WithValue(ctx, b, true)
	}
	return ctx
}

func (b *runtimeReferenceBarrier) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if b.afterInsert && ctx.Value(b) != nil {
		close(b.reached)
		<-b.resume
	}
}

func TestRuntimeRevisionClaimSerializesWithInstanceCreation(t *testing.T) {
	for _, referenceFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("reference_first=%t", referenceFirst), func(t *testing.T) {
			pool := setupTestDB(t)
			client := NewClient(pool)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
			barrier := &runtimeReferenceBarrier{afterInsert: referenceFirst, reached: make(chan struct{}), resume: make(chan struct{})}
			var resume sync.Once
			defer resume.Do(func() { close(barrier.resume) })
			config := pool.Config()
			config.ConnConfig.Tracer = barrier
			creatingPool, err := pgxpool.NewWithConfig(ctx, config)
			require.NoError(t, err)
			t.Cleanup(creatingPool.Close)
			created := make(chan error, 1)
			go func() {
				_, _, err := NewClient(creatingPool).CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), "instance")
				created <- err
			}()
			select {
			case <-barrier.reached:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.NoError(t, client.RetireAgentTemplateHarnessPairs(ctx, "team-a", "assistant"))
			type claimResult struct {
				revision *RuntimeRevision
				err      error
			}
			claimed := make(chan claimResult, 1)
			go func() {
				r, err := client.ClaimRuntimeRevisionDeletion(ctx, "revision")
				claimed <- claimResult{r, err}
			}()
			if referenceFirst {
				// Wait for the actual row-lock wait, not an arbitrary sleep. The
				// eligibility check must see the subsequent reference commit.
				require.Eventually(t, func() bool {
					var waiting bool
					err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname = current_database() AND query LIKE '-- name: GetRuntimeRevisionForUpdate%' AND cardinality(pg_blocking_pids(pid)) > 0)").Scan(&waiting)
					return err == nil && waiting
				}, 5*time.Second, 10*time.Millisecond)
				resume.Do(func() { close(barrier.resume) })
				require.NoError(t, <-created)
				result := <-claimed
				require.NoError(t, result.err)
				require.Nil(t, result.revision)
			} else {
				result := <-claimed
				require.NoError(t, result.err)
				require.NotNil(t, result.revision)
				resume.Do(func() { close(barrier.resume) })
				require.ErrorIs(t, <-created, ErrRuntimeRevisionDeleting)
			}
		})
	}
}

func TestRuntimeRevisionClaimPreservesReferencesUntilFinalization(t *testing.T) {
	pool := setupTestDB(t)
	client := NewClient(pool)
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	pair := AgentTemplateHarnessPair{
		Namespace: "team-a", AgentTemplateName: "assistant", AgentTemplateUID: "assistant-uid",
		HarnessName: "kagent", HarnessUID: "kagent-uid", DesiredRevision: "pending",
	}
	require.NoError(t, client.RetireAgentTemplateHarnessPairs(ctx, "team-a", "assistant"))
	// A skipped finalization must leave last-good intact for reactivation.
	require.NoError(t, client.DeleteUnreferencedRuntimeRevision(ctx, "revision", "revision-actor-uid"))
	require.NoError(t, client.UpsertAgentTemplateHarnessPair(ctx, pair))
	instance, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), "instance")
	require.NoError(t, err)
	require.Equal(t, "revision", instance.GetPreparedRevision())
	require.NoError(t, client.DeleteAgentInstance(ctx, instance.GetId()))
	require.NoError(t, client.RetireAgentTemplateHarnessPairs(ctx, "team-a", "assistant"))
	claimed, err := client.ClaimRuntimeRevisionDeletion(ctx, "revision")
	require.NoError(t, err)
	require.NotNil(t, claimed)
	// Reactivating a retired success pointer and adding a new desired edge are
	// both forbidden, as is overwriting a runtime while deletion is in flight.
	require.ErrorIs(t, client.UpsertAgentTemplateHarnessPair(ctx, pair), ErrRuntimeRevisionDeleting)
	pair.AgentTemplateUID = "replacement-uid"
	pair.DesiredRevision = "revision"
	require.ErrorIs(t, client.UpsertAgentTemplateHarnessPair(ctx, pair), ErrRuntimeRevisionDeleting)
	require.ErrorIs(t, client.UpsertRuntimeRevision(ctx, *claimed), ErrRuntimeRevisionDeleting)
	// A new client rediscovers and retries the committed claim after a crash.
	restarted := NewClient(pool)
	revisions, err := restarted.ListUnreferencedRuntimeRevisions(ctx)
	require.NoError(t, err)
	require.Len(t, revisions, 1)
	retry, err := restarted.ClaimRuntimeRevisionDeletion(ctx, "revision")
	require.NoError(t, err)
	require.NotNil(t, retry)
	require.Equal(t, claimed.Revision, retry.Revision)
	require.Equal(t, claimed.ActorTemplateUID, retry.ActorTemplateUID)
	require.NoError(t, restarted.DeleteUnreferencedRuntimeRevision(ctx, "revision", "revision-actor-uid"))
	_, err = restarted.GetRuntimeRevision(ctx, "revision")
	require.ErrorIs(t, err, ErrNotFound)
	// The same digest can be prepared again once cleanup has completed.
	claimed.ActorTemplateUID = "recreated-actor-uid"
	require.NoError(t, restarted.UpsertRuntimeRevision(ctx, *claimed))
	newClaim, err := restarted.ClaimRuntimeRevisionDeletion(ctx, "revision")
	require.NoError(t, err)
	require.NotNil(t, newClaim)
	require.NoError(t, restarted.DeleteUnreferencedRuntimeRevision(ctx, "revision", "revision-actor-uid"))
	_, err = restarted.GetRuntimeRevision(ctx, "revision")
	require.NoError(t, err, "a delayed collector must not finalize the new runtime")
	require.NoError(t, restarted.DeleteUnreferencedRuntimeRevision(ctx, "revision", "recreated-actor-uid"))
	require.NoError(t, restarted.UpsertRuntimeRevision(ctx, *claimed))
	require.NoError(t, restarted.UpsertAgentTemplateHarnessPair(ctx, pair))
}
