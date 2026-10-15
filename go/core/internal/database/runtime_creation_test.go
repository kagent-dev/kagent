package database

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestRuntimeCreationRequiresCurrentPreparedRevision(t *testing.T) {
	for _, kind := range []string{"session", "sandbox"} {
		for _, invalidateFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/invalidate_first=%t", kind, invalidateFirst), func(t *testing.T) {
				pool := setupTestDB(t)
				client := NewClient(pool, "public")
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				var create func(*Client, string) (string, bool, error)
				var desire func(*Client, string) error
				var prepare func(string)
				var createQuery, updateQuery, unresolved string
				if kind == "session" {
					prepare = func(revision string) {
						sessionFixture(t, client, ctx, "team-a", revision, "assistant", "kagent")
					}
					create = func(c *Client, requestID string) (string, bool, error) {
						session, created, err := c.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), requestID)
						return session.GetPreparedRevision(), created, err
					}
					desire = func(c *Client, revision string) error {
						return c.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team-a", AgentName: "assistant", AgentUID: "assistant-uid", DesiredRevision: revision})
					}
					createQuery, updateQuery = "FROM agent_definition p", "INSERT INTO agent_definition"
				} else {
					input, options := sandboxFixture(t, client, "revision-1")
					prepare = func(revision string) {
						_, _ = sandboxFixture(t, client, revision)
					}
					create = func(c *Client, requestID string) (string, bool, error) {
						request := proto.CloneOf(input)
						request.Id = uuid.NewString()
						instance, created, err := c.CreateSandbox(ctx, request, requestID, options)
						return instance.GetPreparedRevision(), created, err
					}
					desire = func(c *Client, revision string) error {
						return c.UpsertSandboxTemplateDefinition(ctx, SandboxTemplateDefinition{Namespace: "team-a", SandboxTemplateName: "scratch", SandboxTemplateUID: "uid", DesiredRevision: revision})
					}
					createQuery, updateQuery = "FROM sandbox_template_definition", "INSERT INTO sandbox_template_definition"
					unresolved = "pending:uid:2"
				}
				prepare("revision-1")
				revision, created, err := create(client, "original")
				require.NoError(t, err)
				require.True(t, created)
				require.Equal(t, "revision-1", revision)

				// Pause the first transaction with the definition row locked. The
				// other operation must wait and observe the committed desired state.
				query, waitingQuery := createQuery, updateQuery
				if invalidateFirst {
					query, waitingQuery = updateQuery, createQuery
				}
				barrier := &runtimeReferenceBarrier{query: query, afterQuery: true, reached: make(chan struct{}), resume: make(chan struct{})}
				var resume sync.Once
				defer resume.Do(func() { close(barrier.resume) })
				config := pool.Config()
				config.ConnConfig.Tracer = barrier
				firstPool, err := pgxpool.NewWithConfig(ctx, config)
				require.NoError(t, err)
				t.Cleanup(firstPool.Close)
				first := NewClient(firstPool, "public")
				creation, invalidation := make(chan error, 1), make(chan error, 1)
				createConcurrent := func(c *Client) {
					_, _, err := create(c, "concurrent")
					creation <- err
				}
				invalidate := func(c *Client) { invalidation <- desire(c, unresolved) }
				if invalidateFirst {
					go invalidate(first)
				} else {
					go createConcurrent(first)
				}
				select {
				case <-barrier.reached:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if invalidateFirst {
					go createConcurrent(client)
				} else {
					go invalidate(client)
				}
				require.Eventually(t, func() bool {
					var waiting bool
					err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname = current_database() AND query LIKE $1 AND cardinality(pg_blocking_pids(pid)) > 0)", "%"+waitingQuery+"%").Scan(&waiting)
					return err == nil && waiting
				}, 5*time.Second, 10*time.Millisecond)
				resume.Do(func() { close(barrier.resume) })
				require.NoError(t, <-invalidation)
				if invalidateFirst {
					require.ErrorIs(t, <-creation, ErrNotFound)
				} else {
					require.NoError(t, <-creation)
				}

				// Both unresolved inputs and an ordinary pending update reject fresh
				// requests while retries keep their original revision.
				for _, desired := range []string{unresolved, "revision-2"} {
					require.NoError(t, desire(client, desired))
					_, created, err = create(client, "fresh")
					require.ErrorIs(t, err, ErrNotFound)
					require.False(t, created)
					revision, created, err = create(client, "original")
					require.NoError(t, err)
					require.False(t, created)
					require.Equal(t, "revision-1", revision)
				}
				prepare("revision-2")
				revision, created, err = create(client, "fresh")
				require.NoError(t, err)
				require.True(t, created, "rejected requests must not reserve an idempotency key")
				require.Equal(t, "revision-2", revision)
				revision, created, err = create(client, "original")
				require.NoError(t, err)
				require.False(t, created)
				require.Equal(t, "revision-1", revision)
			})
		}
	}
}
