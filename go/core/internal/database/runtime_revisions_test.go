package database

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
)

func TestRuntimeRevisionCollectionAfterPairRetirement(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")

	// The successful pair must protect both the listing and deletion paths.
	revisions, err := client.ListUnreferencedRuntimeRevisions(ctx)
	require.NoError(t, err)
	require.Empty(t, revisions)
	require.NoError(t, client.DeleteRuntimeRevision(ctx, "revision", "revision-actor-uid"))
	_, err = client.GetRuntimeRevision(ctx, "revision")
	require.NoError(t, err)

	err = client.RetireAllPairIdentities(ctx, "team-a", "assistant", "kagent")
	require.NoError(t, err)
	revisions, err = client.ListUnreferencedRuntimeRevisions(ctx)
	require.NoError(t, err)
	require.Len(t, revisions, 1)
	require.Equal(t, "revision", revisions[0].Revision)
	claimed, err := client.BeginRuntimeRevisionDeletion(ctx, "revision")
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, client.DeleteRuntimeRevision(ctx, "revision", "revision-actor-uid"))
	_, err = client.GetRuntimeRevision(ctx, "revision")
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, client.DeleteRuntimeRevision(ctx, "revision", "revision-actor-uid"))
}

func TestRuntimeRevisionCollectionPreservesInstanceAndCheckpoint(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), "instance")
	require.NoError(t, err)
	_, err = markAgentInstanceReady(ctx, client, instance.GetId(), "runtime")
	require.NoError(t, err)
	require.NoError(t, client.RetireAllPairIdentities(ctx, "team-a", "assistant", "kagent"))

	assertRetained := func() {
		t.Helper()
		revisions, err := client.ListUnreferencedRuntimeRevisions(ctx)
		require.NoError(t, err)
		require.Empty(t, revisions)
		claimed, err := client.BeginRuntimeRevisionDeletion(ctx, "revision")
		require.NoError(t, err)
		require.Nil(t, claimed)
		require.NoError(t, client.DeleteRuntimeRevision(ctx, "revision", "revision-actor-uid"))
		_, err = client.GetRuntimeRevision(ctx, "revision")
		require.NoError(t, err)
	}
	assertRetained()
	task := newAgentInstanceTask("task", "message")
	task.ContextID = instance.GetContextId()
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
	claimed, err := client.BeginRuntimeRevisionDeletion(ctx, "revision")
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, client.DeleteRuntimeRevision(ctx, "revision", "revision-actor-uid"))
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
	require.NoError(t, client.RecordRuntimeRevision(ctx, *revision, false))
	pair := AgentTemplateHarnessPair{
		Namespace: "team-a", AgentTemplateName: "assistant", AgentTemplateUID: revision.AgentTemplateUID,
		HarnessName: "kagent", HarnessUID: "kagent-uid", DesiredRevision: "new",
	}
	require.NoError(t, client.UpsertAgentTemplateHarnessPair(ctx, pair))
	request := newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", "")
	_, _, err = client.CreateAgentInstance(ctx, request, "instance")
	require.ErrorIs(t, err, ErrNotFound, "a replacement must not select the previous template UID")
	require.NoError(t, client.RecordRuntimeRevision(ctx, *revision, true))

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
	claimed, err := client.BeginRuntimeRevisionDeletion(ctx, "old")
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, client.DeleteRuntimeRevision(ctx, "old", "old-actor-uid"))
}

// Pause the real store before or after it locks a revision. Exercise both
// commit orderings without replacing persistence queries with test-only writes.
type runtimeReferenceBarrier struct {
	query      string
	afterQuery bool
	reached    chan struct{}
	resume     chan struct{}
}

func (b *runtimeReferenceBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, b.query) {
		if !b.afterQuery {
			close(b.reached)
			<-b.resume
		}
		return context.WithValue(ctx, b, true)
	}
	return ctx
}

func (b *runtimeReferenceBarrier) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if b.afterQuery && ctx.Value(b) != nil {
		close(b.reached)
		<-b.resume
	}
}

func TestRuntimeRevisionDeletionSerializesWithReferenceAcquisition(t *testing.T) {
	for _, source := range []string{"instance", "reactivated pair", "new pair"} {
		for _, referenceFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reference_first=%t", source, referenceFirst), func(t *testing.T) {
				pool := setupTestDB(t)
				client := NewClient(pool)
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
				query := "FROM runtime_revision WHERE revision = $1 FOR UPDATE"
				if source != "instance" {
					query = "FOR UPDATE OF r"
					require.NoError(t, client.RetireAllPairIdentities(ctx, "team-a", "assistant", "kagent"))
				}
				barrier := &runtimeReferenceBarrier{query: query, afterQuery: referenceFirst, reached: make(chan struct{}), resume: make(chan struct{})}
				var resume sync.Once
				defer resume.Do(func() { close(barrier.resume) })
				config := pool.Config()
				config.ConnConfig.Tracer = barrier
				creatingPool, err := pgxpool.NewWithConfig(ctx, config)
				require.NoError(t, err)
				t.Cleanup(creatingPool.Close)
				created := make(chan error, 1)
				go func() {
					creating := NewClient(creatingPool)
					if source == "instance" {
						_, _, err := creating.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), "instance")
						created <- err
						return
					}
					pair := AgentTemplateHarnessPair{
						Namespace: "team-a", AgentTemplateName: "assistant", AgentTemplateUID: "assistant-uid",
						HarnessName: "kagent", HarnessUID: "kagent-uid", DesiredRevision: "pending",
					}
					if source == "new pair" {
						pair.AgentTemplateUID = "replacement-uid"
						pair.DesiredRevision = "revision"
					}
					created <- creating.UpsertAgentTemplateHarnessPair(ctx, pair)
				}()
				select {
				case <-barrier.reached:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if source == "instance" {
					require.NoError(t, client.RetireAllPairIdentities(ctx, "team-a", "assistant", "kagent"))
				}
				type claimResult struct {
					revision *RuntimeRevision
					err      error
				}
				claimed := make(chan claimResult, 1)
				go func() {
					r, err := client.BeginRuntimeRevisionDeletion(ctx, "revision")
					claimed <- claimResult{r, err}
				}()
				if referenceFirst {
					// Wait for the actual row-lock wait. The eligibility check
					// must see the reference committed after the wait ends.
					require.Eventually(t, func() bool {
						var waiting bool
						err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname = current_database() AND query LIKE '%FROM runtime_revision WHERE revision = $1 FOR UPDATE%' AND cardinality(pg_blocking_pids(pid)) > 0)").Scan(&waiting)
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
					require.ErrorIs(t, <-created, ErrObjectDeleting)
				}
			})
		}
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
	require.NoError(t, client.RetireAllPairIdentities(ctx, "team-a", "assistant", "kagent"))
	// A skipped finalization must leave last-good intact for reactivation.
	require.NoError(t, client.DeleteRuntimeRevision(ctx, "revision", "revision-actor-uid"))
	require.NoError(t, client.UpsertAgentTemplateHarnessPair(ctx, pair))
	instance, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), "instance")
	require.NoError(t, err)
	require.Equal(t, "revision", instance.GetPreparedRevision())
	require.NoError(t, client.DeleteAgentInstance(ctx, instance.GetId()))
	require.NoError(t, client.RetireAllPairIdentities(ctx, "team-a", "assistant", "kagent"))
	claimed, err := client.BeginRuntimeRevisionDeletion(ctx, "revision")
	require.NoError(t, err)
	require.NotNil(t, claimed)
	// Reactivating a retired success pointer and adding a new desired edge are
	// both forbidden, as is overwriting a runtime while deletion is in flight.
	require.ErrorIs(t, client.UpsertAgentTemplateHarnessPair(ctx, pair), ErrObjectDeleting)
	pair.AgentTemplateUID = "replacement-uid"
	pair.DesiredRevision = "revision"
	require.ErrorIs(t, client.UpsertAgentTemplateHarnessPair(ctx, pair), ErrObjectDeleting)
	require.ErrorIs(t, client.RecordRuntimeRevision(ctx, *claimed, false), ErrObjectDeleting)
	require.ErrorIs(t, client.RecordRuntimeRevision(ctx, *claimed, true), ErrObjectDeleting)
	// A new client rediscovers and retries the committed claim after a crash.
	restarted := NewClient(pool)
	revisions, err := restarted.ListUnreferencedRuntimeRevisions(ctx)
	require.NoError(t, err)
	require.Len(t, revisions, 1)
	retry, err := restarted.BeginRuntimeRevisionDeletion(ctx, "revision")
	require.NoError(t, err)
	require.NotNil(t, retry)
	require.Equal(t, claimed.Revision, retry.Revision)
	require.Equal(t, claimed.ActorTemplateUID, retry.ActorTemplateUID)
	require.NoError(t, restarted.DeleteRuntimeRevision(ctx, "revision", "revision-actor-uid"))
	_, err = restarted.GetRuntimeRevision(ctx, "revision")
	require.ErrorIs(t, err, ErrNotFound)
	// The same digest can be prepared again once cleanup has completed.
	claimed.ActorTemplateUID = "recreated-actor-uid"
	require.NoError(t, restarted.RecordRuntimeRevision(ctx, *claimed, false))
	newClaim, err := restarted.BeginRuntimeRevisionDeletion(ctx, "revision")
	require.NoError(t, err)
	require.NotNil(t, newClaim)
	require.NoError(t, restarted.DeleteRuntimeRevision(ctx, "revision", "revision-actor-uid"))
	_, err = restarted.GetRuntimeRevision(ctx, "revision")
	require.NoError(t, err, "a delayed collector must not finalize the new runtime")
	require.NoError(t, restarted.DeleteRuntimeRevision(ctx, "revision", "recreated-actor-uid"))
	require.NoError(t, restarted.RecordRuntimeRevision(ctx, *claimed, false))
	require.NoError(t, restarted.UpsertAgentTemplateHarnessPair(ctx, pair))
}

func TestRuntimeRevisionFinalizationSerializesWithPairWrites(t *testing.T) {
	for _, operation := range []string{"reactivate", "record"} {
		for _, finalizeFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/finalize_first=%t", operation, finalizeFirst), func(t *testing.T) {
				pool := setupTestDB(t)
				client := NewClient(pool)
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
				require.NoError(t, client.RetireAllPairIdentities(ctx, "team-a", "assistant", "kagent"))
				claimed, err := client.BeginRuntimeRevisionDeletion(ctx, "revision")
				require.NoError(t, err)
				require.NotNil(t, claimed)

				barrier := &runtimeReferenceBarrier{
					query: "FOR UPDATE OF r", reached: make(chan struct{}), resume: make(chan struct{}),
				}
				if operation == "record" {
					barrier.query = "INSERT INTO runtime_revision"
				}
				waitingQuery := "ORDER BY namespace, agent_template_uid, harness_uid"
				if finalizeFirst {
					barrier.query, barrier.afterQuery = "FROM runtime_revision WHERE revision = $1 FOR UPDATE", true
					waitingQuery = "INSERT INTO agent_template_harness_pair"
					if operation == "record" {
						waitingQuery = "FOR UPDATE OF p"
					}
				}
				var resume sync.Once
				defer resume.Do(func() { close(barrier.resume) })
				config := pool.Config()
				config.ConnConfig.Tracer = barrier
				blockedPool, err := pgxpool.NewWithConfig(ctx, config)
				require.NoError(t, err)
				t.Cleanup(blockedPool.Close)
				creating, deleting := NewClient(blockedPool), client
				if finalizeFirst {
					creating, deleting = client, creating
				}
				pair := AgentTemplateHarnessPair{
					Namespace: "team-a", AgentTemplateName: "assistant", AgentTemplateUID: "assistant-uid",
					HarnessName: "kagent", HarnessUID: "kagent-uid", DesiredRevision: "revision",
				}
				created, deleted := make(chan error, 1), make(chan error, 1)
				create := func() {
					if operation == "record" {
						created <- creating.RecordRuntimeRevision(ctx, *claimed, true)
						return
					}
					created <- creating.UpsertAgentTemplateHarnessPair(ctx, pair)
				}
				finalize := func() { deleted <- deleting.DeleteRuntimeRevision(ctx, "revision", claimed.ActorTemplateUID) }
				if finalizeFirst {
					go finalize()
				} else {
					go create()
				}
				select {
				case <-barrier.reached:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if finalizeFirst {
					go create()
				} else {
					go finalize()
				}
				require.Eventually(t, func() bool {
					var waiting bool
					err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname = current_database() AND query LIKE $1 AND cardinality(pg_blocking_pids(pid)) > 0)", "%"+waitingQuery+"%").Scan(&waiting)
					return err == nil && waiting
				}, 5*time.Second, 10*time.Millisecond)
				resume.Do(func() { close(barrier.resume) })
				if finalizeFirst {
					require.NoError(t, <-created)
				} else {
					require.ErrorIs(t, <-created, ErrObjectDeleting)
				}
				require.NoError(t, <-deleted, "finalization and pair writes must not deadlock")
				require.NoError(t, client.UpsertAgentTemplateHarnessPair(ctx, pair))
			})
		}
	}
}

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
	require.NoError(t, c.RetireAllPairIdentities(ctx, pair.Namespace, pair.AgentTemplateName, pair.HarnessName))
	revision.Revision, revision.ActorTemplateName = "third", "third"
	require.NoError(t, c.RecordRuntimeRevision(ctx, revision, true))
	assertAvailableRevision("")
	// Reviving the pair must retain the last success, not a report made while retired.
	require.NoError(t, c.UpsertAgentTemplateHarnessPair(ctx, pair))
	assertAvailableRevision("second")
}
