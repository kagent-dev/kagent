package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// ListTools returns all undeleted tools in ascending creation-time order.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT id, server_name, group_kind, created_at, updated_at, deleted_at, description FROM tool
		WHERE deleted_at IS NULL
		ORDER BY created_at ASC
	`, pgx.RowToStructByName[Tool])
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}
	return rows, nil
}

// ListToolServers returns all undeleted servers in ascending creation-time order.
func (c *Client) ListToolServers(ctx context.Context) ([]ToolServer, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT name, group_kind, created_at, updated_at, deleted_at, description, last_connected FROM toolserver
		WHERE deleted_at IS NULL
		ORDER BY toolserver.created_at ASC
	`, pgx.RowToStructByName[ToolServer])
	if err != nil {
		return nil, fmt.Errorf("failed to list tool servers: %w", err)
	}
	return rows, nil
}

// RefreshToolServer atomically publishes a server and replaces its visible tools.
// Removed servers and tools are revived when supplied again, preserving creation times.
// An empty tool list keeps the server visible and hides its previous tools.
func (c *Client) RefreshToolServer(ctx context.Context, server *ToolServer, tools ...*v1alpha3.MCPTool) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		// Lock the server before changing tools, including when its catalog is empty.
		if err := execSQL(ctx, tx, `
			INSERT INTO toolserver (name, group_kind, description, last_connected)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (name, group_kind) DO UPDATE SET
			    description = EXCLUDED.description,
			    last_connected = EXCLUDED.last_connected,
			    updated_at = NOW(), deleted_at = NULL
		`, server.Name, server.GroupKind, server.Description, server.LastConnected); err != nil {
			return fmt.Errorf("failed to store tool server: %w", err)
		}
		if err := deleteToolsForServer(ctx, tx, server.Name, server.GroupKind); err != nil {
			return fmt.Errorf("failed to delete existing tools: %w", err)
		}
		for _, tool := range tools {
			if err := execSQL(ctx, tx, `
				INSERT INTO tool (id, server_name, group_kind, description)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (id, server_name, group_kind) DO UPDATE SET
				    description = EXCLUDED.description, updated_at = NOW(), deleted_at = NULL
			`, tool.Name, server.Name, server.GroupKind, tool.Description); err != nil {
				return fmt.Errorf("failed to upsert tool %s: %w", tool.Name, err)
			}
		}
		return nil
	})
}

// DeleteToolServer atomically hides the server and its tools for the exact name and
// group kind. A missing server is a successful no-op.
func (c *Client) DeleteToolServer(ctx context.Context, serverName, groupKind string) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		// Lock even deleted servers so a concurrent refresh cannot revive only their tools.
		result, err := tx.Exec(ctx, `
			UPDATE toolserver SET deleted_at = COALESCE(deleted_at, NOW())
			WHERE name = $1 AND group_kind = $2
		`, serverName, groupKind)
		if err != nil {
			return fmt.Errorf("failed to delete tool server: %w", err)
		}
		if result.RowsAffected() == 0 {
			return nil
		}
		return deleteToolsForServer(ctx, tx, serverName, groupKind)
	})
}

// deleteToolsForServer hides the server's visible tools in the caller's transaction.
// Missing tools are a successful no-op.
func deleteToolsForServer(ctx context.Context, tx pgx.Tx, serverName, groupKind string) error {
	return execSQL(ctx, tx, `
		UPDATE tool SET deleted_at = NOW()
		WHERE server_name = $1 AND group_kind = $2 AND deleted_at IS NULL
	`, serverName, groupKind)
}
