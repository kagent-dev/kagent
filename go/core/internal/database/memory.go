package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	pgvector "github.com/pgvector/pgvector-go"
)

func (c *Client) StoreAgentMemory(ctx context.Context, memory *Memory) error {
	id, err := queryOne(ctx, c.db, `
		INSERT INTO memory (agent_name, user_id, content, embedding, metadata, created_at, expires_at, access_count)
		VALUES ($1, $2, $3, $4, $5, NOW(), $6, $7)
		RETURNING id
	`,
		pgx.RowTo[string], &memory.AgentName, &memory.UserID, &memory.Content, memory.Embedding, &memory.Metadata,
		memory.ExpiresAt, &memory.AccessCount,
	)
	if err != nil {
		return err
	}
	memory.ID = id
	return nil
}

func (c *Client) StoreAgentMemories(ctx context.Context, memories []*Memory) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		for _, m := range memories {
			id, err := queryOne(ctx, tx, `
				INSERT INTO memory (agent_name, user_id, content, embedding, metadata, created_at, expires_at, access_count)
				VALUES ($1, $2, $3, $4, $5, NOW(), $6, $7)
				RETURNING id
			`,
				pgx.RowTo[string], &m.AgentName, &m.UserID, &m.Content, m.Embedding, &m.Metadata, m.ExpiresAt,
				&m.AccessCount,
			)
			if err != nil {
				return fmt.Errorf("failed to store memory: %w", err)
			}
			m.ID = id
		}
		return nil
	})
}

func (c *Client) SearchAgentMemory(ctx context.Context, agentName, userID string, embedding pgvector.Vector, limit int) ([]AgentMemorySearchResult, error) {
	normalized := strings.ReplaceAll(agentName, "-", "_")
	rows, err := queryMany(ctx, c.db, `
		SELECT id, agent_name, user_id, content, embedding, metadata, created_at, expires_at, access_count,
		    COALESCE(1 - (embedding <=> $1), 0) AS score
		FROM memory
		WHERE (agent_name = $2 OR agent_name = $3) AND user_id = $4
		ORDER BY embedding <=> $1 ASC
		LIMIT $5
	`, pgx.RowToStructByName[memorySearchRow], embedding, &agentName, &normalized, &userID, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("failed to search agent memory: %w", err)
	}

	results := make([]AgentMemorySearchResult, len(rows))
	for i, r := range rows {
		results[i] = AgentMemorySearchResult{
			Memory: *toMemory(r.memoryRow),
			Score:  r.Score,
		}
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

func (c *Client) ListAgentMemories(ctx context.Context, agentName, userID string) ([]Memory, error) {
	normalized := strings.ReplaceAll(agentName, "-", "_")
	rows, err := queryMany(ctx, c.db, `
		SELECT id, agent_name, user_id, content, embedding, metadata, created_at, expires_at, access_count FROM memory
		WHERE (agent_name = $1 OR agent_name = $2) AND user_id = $3
		ORDER BY access_count DESC
	`, pgx.RowToStructByName[memoryRow], &agentName, &normalized, &userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list agent memories: %w", err)
	}
	memories := make([]Memory, len(rows))
	for i, r := range rows {
		memories[i] = *toMemory(r)
	}
	return memories, nil
}

func (c *Client) DeleteAgentMemory(ctx context.Context, agentName, userID string) error {
	if err := execSQL(ctx, c.db, `
		DELETE FROM memory WHERE agent_name = $1 AND user_id = $2
	`, &agentName, &userID); err != nil {
		return fmt.Errorf("failed to delete agent memory: %w", err)
	}
	normalized := strings.ReplaceAll(agentName, "-", "_")
	if normalized != agentName {
		if err := execSQL(ctx, c.db, `
			DELETE FROM memory WHERE agent_name = $1 AND user_id = $2
		`, &normalized, &userID); err != nil {
			return fmt.Errorf("failed to delete normalized agent memory: %w", err)
		}
	}
	return nil
}

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

func toMemory(r memoryRow) *Memory {
	return &Memory{
		ID:          r.ID,
		AgentName:   derefStr(r.AgentName),
		UserID:      derefStr(r.UserID),
		Content:     derefStr(r.Content),
		Embedding:   r.Embedding,
		Metadata:    derefStr(r.Metadata),
		CreatedAt:   derefTime(r.CreatedAt),
		ExpiresAt:   r.ExpiresAt,
		AccessCount: derefInt64(r.AccessCount),
	}
}

type memoryRow struct {
	ID          string
	AgentName   *string
	UserID      *string
	Content     *string
	Embedding   pgvector.Vector
	Metadata    *string
	CreatedAt   *time.Time
	ExpiresAt   *time.Time
	AccessCount *int64
}

type memorySearchRow struct {
	memoryRow
	Score float64
}
