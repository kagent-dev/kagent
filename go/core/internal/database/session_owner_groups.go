package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// SetSessionOwnerGroups records the groups the session's owner's turn
// carries, replacing the ones recorded before. ErrNotFound if the session is
// absent or deleted.
func (c *Client) SetSessionOwnerGroups(ctx context.Context, sessionID string, groups []string) error {
	if groups == nil {
		groups = []string{}
	}
	tag, err := c.db.Exec(ctx, `
		UPDATE session SET owner_groups = $2
		WHERE id = $1 AND EXISTS (SELECT 1 FROM session_record
		    WHERE id = $1 AND state <> 'RUNTIME_STATE_DELETED')
	`, sessionID, groups)
	if err != nil {
		return fmt.Errorf("record the owner's groups of Session %s: %w", sessionID, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("record the owner's groups of Session %s: %w", sessionID, ErrNotFound)
	}
	return nil
}

// GetSessionOwnerGroups returns the groups recorded for the session's owner;
// none before the owner's first recorded turn. ErrNotFound if the session is
// absent or deleted.
func (c *Client) GetSessionOwnerGroups(ctx context.Context, sessionID string) ([]string, error) {
	groups, err := queryOne(ctx, c.db, `
		SELECT s.owner_groups FROM session s JOIN session_record r USING (id)
		WHERE s.id = $1 AND r.state <> 'RUNTIME_STATE_DELETED'
	`, pgx.RowTo[[]string], sessionID)
	if err != nil {
		return nil, fmt.Errorf("read the owner's groups of Session %s: %w", sessionID, notFoundOr(err))
	}
	return groups, nil
}
