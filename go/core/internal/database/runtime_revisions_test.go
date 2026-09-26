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
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/stretchr/testify/require"
)

func TestRetireAgentIdentities(t *testing.T) {
	for _, test := range []struct {
		name   string
		except *AgentDefinition
		keep   bool
	}{
		{name: "all"},
		{name: "except current", except: &AgentDefinition{AgentUID: "assistant-uid"}, keep: true},
		{name: "replacement template", except: &AgentDefinition{AgentUID: "replacement-uid"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient(setupTestDB(t))
			ctx := t.Context()
			sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
			sessionFixture(t, client, ctx, "team-b", "other-namespace", "assistant", "kagent")
			sessionFixture(t, client, ctx, "team-a", "other-template", "other", "kagent")
			sessionFixture(t, client, ctx, "team-a", "other-agent", "another", "other")
			for range 2 {
				require.NoError(t, client.RetireAgentIdentities(ctx, "team-a", "assistant", test.except))
			}
			revisions, err := client.ListUnreferencedRuntimeRevisions(ctx)
			require.NoError(t, err)
			if test.keep {
				require.Empty(t, revisions)
				session, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), "session")
				require.NoError(t, err)
				require.Equal(t, "revision", session.GetPreparedRevision(), "the exception retains its last-good revision")
			} else {
				require.Len(t, revisions, 1, "retirement must stay within the requested namespace and names")
				require.Equal(t, "revision", revisions[0].Revision)
			}
		})
	}
}

func TestRuntimeRevisionCollectionAfterPairRetirement(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")

	// The successful pair must protect both the listing and deletion paths.
	revisions, err := client.ListUnreferencedRuntimeRevisions(ctx)
	require.NoError(t, err)
	require.Empty(t, revisions)
	require.NoError(t, client.DeleteRuntimeRevision(ctx, "revision", "revision-actor-uid"))
	_, err = client.GetRuntimeRevision(ctx, "revision")
	require.NoError(t, err)

	err = client.RetireAgentIdentities(ctx, "team-a", "assistant", nil)
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

func TestRuntimeRevisionCollectionPreservesSessionAndCheckpoint(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), "session")
	require.NoError(t, err)
	_, err = markSessionReady(ctx, client, session.GetId(), "runtime")
	require.NoError(t, err)
	require.NoError(t, client.RetireAgentIdentities(ctx, "team-a", "assistant", nil))

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
	task := newSessionTask("task", "message")
	task.ContextID = session.GetContextId()
	_, err = client.CreateRuntimeTask(ctx, session.GetId(), taskMutationHash("request"), task, "")
	require.NoError(t, err)
	task.Status.State = a2a.TaskStateCompleted
	require.NoError(t, saveRuntimeTask(t, client, session.GetId(), task, task,
		&SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/task", ContentScope: "FULL"}))
	checkpoint, _, err := client.ReserveSessionCheckpoint(ctx, &apiv1alpha1.Checkpoint{
		Id: uuid.NewString(), SessionId: session.GetId(), HeadTaskId: string(task.ID),
	}, "alice", "checkpoint")
	require.NoError(t, err)
	require.ErrorIs(t, deleteSession(ctx, client, session.GetId()), ErrConflict)

	// A checkpoint retains the runtime throughout creation, use, and deletion,
	// even after the source session and active template pair are gone.
	assertRetained()
	_, err = client.FinalizeSessionCheckpoint(ctx, checkpoint.GetId(), "tag-uid", "s3://tags/checkpoint", "")
	require.NoError(t, err)
	require.NoError(t, deleteSession(ctx, client, session.GetId()))
	assertRetained()
	_, _, err = client.BeginDeleteSessionCheckpoint(ctx, checkpoint.GetId(), "alice")
	require.NoError(t, err)
	assertRetained()
	require.NoError(t, client.DeleteSessionCheckpoint(ctx, checkpoint.GetId(), "alice"))
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
	sessionFixture(t, client, ctx, "team-a", "old", "assistant", "kagent")
	revision, err := client.GetRuntimeRevision(ctx, "old")
	require.NoError(t, err)
	revision.Revision = "new"
	revision.AgentUID = "replacement-uid"
	revision.ActorTemplateName = "new-actor-template"
	require.NoError(t, client.RecordRuntimeRevision(ctx, *revision, false))
	pair := AgentDefinition{
		Namespace: "team-a", AgentName: "assistant", AgentUID: revision.AgentUID,
		DesiredRevision: "new",
	}
	require.NoError(t, client.UpsertAgentDefinition(ctx, pair))
	request := newSessionRequest(uuid.NewString(), "assistant", "kagent", "")
	_, _, err = client.CreateSession(ctx, request, "session")
	require.ErrorIs(t, err, ErrNotFound, "a replacement must not select the previous template UID")
	require.NoError(t, client.RecordRuntimeRevision(ctx, *revision, true))

	// Reconciliation of the same identity must preserve its last success while
	// a new desired revision is still preparing.
	pair.DesiredRevision = "pending"
	require.NoError(t, client.UpsertAgentDefinition(ctx, pair))
	session, _, err := client.CreateSession(ctx, request, "session")
	require.NoError(t, err)
	require.Equal(t, "new", session.GetPreparedRevision())
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
	for _, source := range []string{"session", "reactivated pair", "new pair"} {
		for _, referenceFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reference_first=%t", source, referenceFirst), func(t *testing.T) {
				pool := setupTestDB(t)
				client := NewClient(pool)
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
				query := "FROM runtime_revision WHERE revision = $1 FOR UPDATE"
				if source != "session" {
					query = "FOR UPDATE OF r"
					require.NoError(t, client.RetireAgentIdentities(ctx, "team-a", "assistant", nil))
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
					if source == "session" {
						_, _, err := creating.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), "session")
						created <- err
						return
					}
					pair := AgentDefinition{
						Namespace: "team-a", AgentName: "assistant", AgentUID: "assistant-uid",
						DesiredRevision: "pending",
					}
					if source == "new pair" {
						pair.AgentUID = "replacement-uid"
						pair.DesiredRevision = "revision"
					}
					created <- creating.UpsertAgentDefinition(ctx, pair)
				}()
				select {
				case <-barrier.reached:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if source == "session" {
					require.NoError(t, client.RetireAgentIdentities(ctx, "team-a", "assistant", nil))
				}
				type claimResult struct {
					revision *RuntimeArtifact
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
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	pair := AgentDefinition{
		Namespace: "team-a", AgentName: "assistant", AgentUID: "assistant-uid",
		DesiredRevision: "pending",
	}
	require.NoError(t, client.RetireAgentIdentities(ctx, "team-a", "assistant", nil))
	// A skipped finalization must leave last-good intact for reactivation.
	require.NoError(t, client.DeleteRuntimeRevision(ctx, "revision", "revision-actor-uid"))
	require.NoError(t, client.UpsertAgentDefinition(ctx, pair))
	session, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), "session")
	require.NoError(t, err)
	require.Equal(t, "revision", session.GetPreparedRevision())
	require.NoError(t, deleteSession(ctx, client, session.GetId()))
	require.NoError(t, client.RetireAgentIdentities(ctx, "team-a", "assistant", nil))
	original, err := client.GetRuntimeRevision(ctx, "revision")
	require.NoError(t, err)
	claimed, err := client.BeginRuntimeRevisionDeletion(ctx, "revision")
	require.NoError(t, err)
	require.NotNil(t, claimed)
	// Reactivating a retired success pointer and adding a new desired edge are
	// both forbidden, as is overwriting a runtime while deletion is in flight.
	require.ErrorIs(t, client.UpsertAgentDefinition(ctx, pair), ErrObjectDeleting)
	pair.AgentUID = "replacement-uid"
	pair.DesiredRevision = "revision"
	require.ErrorIs(t, client.UpsertAgentDefinition(ctx, pair), ErrObjectDeleting)
	require.ErrorIs(t, client.RecordRuntimeRevision(ctx, *original, false), ErrObjectDeleting)
	require.ErrorIs(t, client.RecordRuntimeRevision(ctx, *original, true), ErrObjectDeleting)
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
	original.ActorTemplateUID = "recreated-actor-uid"
	require.NoError(t, restarted.RecordRuntimeRevision(ctx, *original, false))
	newClaim, err := restarted.BeginRuntimeRevisionDeletion(ctx, "revision")
	require.NoError(t, err)
	require.NotNil(t, newClaim)
	require.NoError(t, restarted.DeleteRuntimeRevision(ctx, "revision", "revision-actor-uid"))
	_, err = restarted.GetRuntimeRevision(ctx, "revision")
	require.NoError(t, err, "a delayed collector must not finalize the new runtime")
	require.NoError(t, restarted.DeleteRuntimeRevision(ctx, "revision", "recreated-actor-uid"))
	require.NoError(t, restarted.RecordRuntimeRevision(ctx, *original, false))
	require.NoError(t, restarted.UpsertAgentDefinition(ctx, pair))
}

func TestRuntimeRevisionFinalizationSerializesWithPairWrites(t *testing.T) {
	for _, operation := range []string{"reactivate", "record"} {
		for _, finalizeFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/finalize_first=%t", operation, finalizeFirst), func(t *testing.T) {
				pool := setupTestDB(t)
				client := NewClient(pool)
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
				require.NoError(t, client.RetireAgentIdentities(ctx, "team-a", "assistant", nil))
				original, err := client.GetRuntimeRevision(ctx, "revision")
				require.NoError(t, err)
				claimed, err := client.BeginRuntimeRevisionDeletion(ctx, "revision")
				require.NoError(t, err)
				require.NotNil(t, claimed)

				barrier := &runtimeReferenceBarrier{
					query: "FOR UPDATE OF r", reached: make(chan struct{}), resume: make(chan struct{}),
				}
				if operation == "record" {
					barrier.query = "INSERT INTO runtime_revision"
				}
				waitingQuery := "ORDER BY namespace, agent_uid"
				if finalizeFirst {
					barrier.query, barrier.afterQuery = "FROM runtime_revision WHERE revision = $1 FOR UPDATE", true
					waitingQuery = "INSERT INTO agent_definition"
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
				pair := AgentDefinition{
					Namespace: "team-a", AgentName: "assistant", AgentUID: "assistant-uid",
					DesiredRevision: "revision",
				}
				created, deleted := make(chan error, 1), make(chan error, 1)
				create := func() {
					if operation == "record" {
						created <- creating.RecordRuntimeRevision(ctx, *original, true)
						return
					}
					created <- creating.UpsertAgentDefinition(ctx, pair)
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
				require.NoError(t, client.UpsertAgentDefinition(ctx, pair))
			})
		}
	}
}

func TestRecordRuntimeRevisionPromotesOnlyCurrentActivePair(t *testing.T) {
	pool := setupTestDB(t)
	c := NewClient(pool)
	ctx := t.Context()
	revision := RuntimeRevision{
		Revision: "first", Namespace: "team", AgentName: "assistant", AgentUID: "template-uid",
		AgentCard:      &a2apb.AgentCard{Name: "original"},
		SourceSnapshot: []byte("{}"), EgressDestinations: []string{},
		ActorTemplateAtespace: "team", ActorTemplateName: "first", ActorTemplateUID: "actor-uid",
	}
	pair := AgentDefinition{
		Namespace: revision.Namespace, AgentName: revision.AgentName, AgentUID: revision.AgentUID,
		DesiredRevision: revision.Revision,
	}
	assertAvailableRevision := func(want string) {
		t.Helper()
		session, _, err := c.CreateSession(ctx, &apiv1alpha1.Session{
			Id: uuid.NewString(), Creator: "alice",

			Agent: &apiv1alpha1.ResourceReference{Namespace: pair.Namespace, Name: pair.AgentName},
		}, uuid.NewString())
		if want == "" {
			require.ErrorIs(t, err, ErrNotFound)
			return
		}
		require.NoError(t, err)
		require.Equal(t, want, session.PreparedRevision)
	}
	require.NoError(t, c.UpsertAgentDefinition(ctx, pair))
	require.NoError(t, c.RecordRuntimeRevision(ctx, revision, false))
	assertAvailableRevision("")
	stored, err := c.GetRuntimeRevision(ctx, revision.Revision)
	require.NoError(t, err)
	require.Equal(t, "original", stored.AgentCard.Name)

	require.NoError(t, c.RecordRuntimeRevision(ctx, revision, true))
	assertAvailableRevision("first")
	pair.DesiredRevision = "second"
	require.NoError(t, c.UpsertAgentDefinition(ctx, pair))
	revision.Revision, revision.ActorTemplateName = "second", "second"
	require.NoError(t, c.RecordRuntimeRevision(ctx, revision, true))
	assertAvailableRevision("second")
	// A delayed report for the old revision must not replace the newer ready revision.
	revision.Revision, revision.ActorTemplateName = "first", "first"
	require.NoError(t, c.RecordRuntimeRevision(ctx, revision, true))
	assertAvailableRevision("second")
	pair.DesiredRevision = "third"
	require.NoError(t, c.UpsertAgentDefinition(ctx, pair))
	require.NoError(t, c.RetireAgentIdentities(ctx, pair.Namespace, pair.AgentName, nil))
	revision.Revision, revision.ActorTemplateName = "third", "third"
	require.NoError(t, c.RecordRuntimeRevision(ctx, revision, true))
	assertAvailableRevision("")
	// Reviving the pair must retain the last success, not a report made while retired.
	require.NoError(t, c.UpsertAgentDefinition(ctx, pair))
	assertAvailableRevision("second")
}

func TestRuntimeRevisionPersistsCredentialBindings(t *testing.T) {
	client := NewClient(setupTestDB(t))
	revision := RuntimeRevision{Revision: "credential-revision", Namespace: "team", AgentName: "agent", AgentUID: "agent", SourceSnapshot: []byte("{}"), AgentCard: &a2apb.AgentCard{}, EgressDestinations: []string{"api.example.com"}, ActorTemplateAtespace: "team", ActorTemplateName: "runtime", Credentials: []egress.Credential{{Hostname: "api.example.com", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://kubernetes.io/team/auth/token"}}}
	require.NoError(t, client.RecordRuntimeRevision(t.Context(), revision, false))
	got, err := client.GetRuntimeRevision(t.Context(), revision.Revision)
	require.NoError(t, err)
	require.Equal(t, revision.Credentials, got.Credentials)
	revision.Credentials[0].URI = "ate-secret://kubernetes.io/team/other/token"
	require.NoError(t, client.RecordRuntimeRevision(t.Context(), revision, false))
	unchanged, err := client.GetRuntimeRevision(t.Context(), revision.Revision)
	require.NoError(t, err)
	require.Equal(t, got.Credentials, unchanged.Credentials, "revision credentials are immutable")
}

func TestRuntimeRevisionRejectsMalformedStoredCredentials(t *testing.T) {
	revision, err := toRuntimeRevision(runtimeRevisionRow{
		Revision: "bad-revision",
		Credentials: []egress.Credential{{
			Hostname: "*", Header: "authorization", URI: "ate-secret://kubernetes.io/team/auth/token",
		}},
	})
	require.ErrorContains(t, err, "decode runtime revision bad-revision credentials")
	require.ErrorContains(t, err, "exact DNS hostname")
	require.Nil(t, revision)
}
