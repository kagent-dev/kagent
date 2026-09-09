package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	pgvector "github.com/pgvector/pgvector-go"
)

// StoreAgentMemory inserts a memory and assigns its generated ID to the input on success.
// Creation time comes from the database; the supplied owner, agent, expiration, and access
// count are stored as given.
func (c *Client) StoreAgentMemory(ctx context.Context, memory *Memory) error {
	id, err := insertAgentMemory(ctx, c.db, memory)
	if err != nil {
		return err
	}
	memory.ID = id
	return nil
}

// StoreAgentMemories inserts all memories in one transaction and assigns their generated
// IDs to the inputs. On failure no inserts commit, but inputs already processed can retain
// IDs from the rolled-back transaction.
func (c *Client) StoreAgentMemories(ctx context.Context, memories []*Memory) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		for _, m := range memories {
			id, err := insertAgentMemory(ctx, tx, m)
			if err != nil {
				return fmt.Errorf("failed to store memory: %w", err)
			}
			m.ID = id
		}
		return nil
	})
}

// SearchAgentMemory returns up to limit memories for the user and agent, ranked by cosine
// similarity. It also matches the legacy agent spelling with hyphens replaced by
// underscores. Expired rows remain searchable until pruned; access-count updates are
// best-effort and cannot fail a successful search.
func (c *Client) SearchAgentMemory(ctx context.Context, agentName, userID string, embedding pgvector.Vector, limit int) ([]AgentMemorySearchResult, error) {
	normalized := strings.ReplaceAll(agentName, "-", "_")
	results, err := queryMany(ctx, c.db, `
		SELECT id, COALESCE(agent_name, '') AS agent_name, COALESCE(user_id, '') AS user_id,
		    COALESCE(content, '') AS content, embedding, COALESCE(metadata, '') AS metadata,
		    COALESCE(created_at, '0001-01-01 00:00:00+00'::timestamptz) AS created_at,
		    expires_at, COALESCE(access_count, 0) AS access_count,
		    COALESCE(1 - (embedding <=> $1), 0) AS score
		FROM memory
		WHERE (agent_name = $2 OR agent_name = $3) AND user_id = $4
		ORDER BY embedding <=> $1 ASC
		LIMIT $5
	`, pgx.RowToStructByName[AgentMemorySearchResult], embedding, &agentName, &normalized, &userID, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("failed to search agent memory: %w", err)
	}

	// Access-count bookkeeping is best-effort: a failure must not fail the search.
	if len(results) > 0 {
		ids := make([]string, len(results))
		for i, r := range results {
			ids[i] = r.ID
		}
		// Lock in ID order to avoid deadlocks between overlapping increments.
		if err := execSQL(ctx, c.db, `
			UPDATE memory
			SET access_count = access_count + 1
			WHERE id IN (
			    SELECT id FROM memory
			    WHERE id = ANY($1::text[])
			    ORDER BY id
			    FOR UPDATE
			)
		`, ids); err != nil {
			logging.FromContext(ctx).WarnContext(ctx, "failed to increment memory access count", "error", err)
		}
	}

	return results, nil
}

// ListAgentMemories returns a user's memories for the agent and its legacy underscore
// spelling, ordered by descending access count. Expired rows remain visible until pruned.
func (c *Client) ListAgentMemories(ctx context.Context, agentName, userID string) ([]Memory, error) {
	normalized := strings.ReplaceAll(agentName, "-", "_")
	rows, err := queryMany(ctx, c.db, `
		SELECT id, COALESCE(agent_name, '') AS agent_name, COALESCE(user_id, '') AS user_id,
		    COALESCE(content, '') AS content, embedding, COALESCE(metadata, '') AS metadata,
		    COALESCE(created_at, '0001-01-01 00:00:00+00'::timestamptz) AS created_at,
		    expires_at, COALESCE(access_count, 0) AS access_count FROM memory
		WHERE (agent_name = $1 OR agent_name = $2) AND user_id = $3
		ORDER BY memory.access_count DESC
	`, pgx.RowToStructByName[Memory], &agentName, &normalized, &userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list agent memories: %w", err)
	}
	return rows, nil
}

// DeleteAgentMemory atomically deletes a user's memories for the agent and its legacy
// underscore spelling. Missing rows are a no-op.
func (c *Client) DeleteAgentMemory(ctx context.Context, agentName, userID string) error {
	normalized := strings.ReplaceAll(agentName, "-", "_")
	if err := execSQL(ctx, c.db, `
		DELETE FROM memory
		WHERE (agent_name = $1 OR agent_name = $3) AND user_id = $2
	`, agentName, userID, normalized); err != nil {
		return fmt.Errorf("failed to delete agent memory: %w", err)
	}
	return nil
}

// PruneExpiredMemories atomically extends expired memories with at least ten accesses by
// fifteen days and resets their access count, then deletes expired memories with fewer
// than ten accesses. Memories without an expiration remain untouched.
func (c *Client) PruneExpiredMemories(ctx context.Context) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		if err := execSQL(ctx, tx, `
			UPDATE memory
			SET expires_at = NOW() + INTERVAL '15 days', access_count = 0
			WHERE expires_at < NOW() AND access_count >= 10
		`); err != nil {
			return fmt.Errorf("failed to extend TTL for popular memories: %w", err)
		}
		if err := execSQL(ctx, tx, `
			DELETE FROM memory
			WHERE expires_at < NOW() AND access_count < 10
		`); err != nil {
			return fmt.Errorf("failed to delete expired memories: %w", err)
		}
		return nil
	})
}

// insertAgentMemory inserts the supplied memory using the database creation time and
// returns its generated ID. It uses the caller's connection or transaction.
func insertAgentMemory(ctx context.Context, db dbExecutor, memory *Memory) (string, error) {
	return queryOne(ctx, db, `
		INSERT INTO memory (agent_name, user_id, content, embedding, metadata, created_at, expires_at, access_count)
		VALUES ($1, $2, $3, $4, $5, NOW(), $6, $7)
		RETURNING id
	`,
		pgx.RowTo[string], &memory.AgentName, &memory.UserID, &memory.Content, memory.Embedding, &memory.Metadata,
		memory.ExpiresAt, &memory.AccessCount,
	)
}
