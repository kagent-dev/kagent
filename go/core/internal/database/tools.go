package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// GetTool returns an undeleted tool with the given name, or ErrNotFound. Names can occur
// on multiple servers; this lookup does not choose a particular server or group kind.
func (c *Client) GetTool(ctx context.Context, name string) (*Tool, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT id, server_name, group_kind, COALESCE(created_at, '0001-01-01 00:00:00+00'::timestamptz) AS created_at,
		    COALESCE(updated_at, '0001-01-01 00:00:00+00'::timestamptz) AS updated_at,
		    deleted_at, COALESCE(description, '') AS description FROM tool
		WHERE id = $1 AND deleted_at IS NULL
		LIMIT 1
	`, pgx.RowToStructByName[Tool], name)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool %s: %w", name, notFoundOr(err))
	}
	return &row, nil
}

// ListTools returns all undeleted tools in ascending creation-time order.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	return c.listTools(ctx, nil, nil)
}

// ListToolsForServer returns undeleted tools for the exact server name and group kind in
// ascending creation-time order.
func (c *Client) ListToolsForServer(ctx context.Context, serverName, groupKind string) ([]Tool, error) {
	return c.listTools(ctx, &serverName, &groupKind)
}

// listTools returns undeleted tools in ascending creation-time order, applying each server
// filter only when its pointer is non-nil. Nullable descriptions and timestamps become Go
// zero values.
func (c *Client) listTools(ctx context.Context, serverName, groupKind *string) ([]Tool, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT id, server_name, group_kind, COALESCE(created_at, '0001-01-01 00:00:00+00'::timestamptz) AS created_at,
		    COALESCE(updated_at, '0001-01-01 00:00:00+00'::timestamptz) AS updated_at,
		    deleted_at, COALESCE(description, '') AS description FROM tool
		WHERE deleted_at IS NULL
		  AND ($1::text IS NULL OR server_name = $1)
		  AND ($2::text IS NULL OR group_kind = $2)
		ORDER BY tool.created_at ASC
	`, pgx.RowToStructByName[Tool], serverName, groupKind)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}
	return rows, nil
}

// DeleteToolsForServer hides all currently visible tools for the server name and group
// kind. Missing tools are a successful no-op.
func (c *Client) DeleteToolsForServer(ctx context.Context, serverName, groupKind string) error {
	return execSQL(ctx, c.db, `
		UPDATE tool SET deleted_at = NOW()
		WHERE server_name = $1 AND group_kind = $2 AND deleted_at IS NULL
	`, serverName, groupKind)
}

// RefreshToolsForServer atomically replaces the server's visible tool catalog with the
// supplied tools. Previously removed tools are revived if listed again; an empty catalog
// hides every tool for that server.
func (c *Client) RefreshToolsForServer(ctx context.Context, serverName, groupKind string, tools ...*v1alpha3.MCPTool) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		if err := execSQL(ctx, tx, `
			UPDATE tool SET deleted_at = NOW()
			WHERE server_name = $1 AND group_kind = $2 AND deleted_at IS NULL
		`, serverName, groupKind); err != nil {
			return fmt.Errorf("failed to delete existing tools: %w", err)
		}
		for _, tool := range tools {
			if err := execSQL(ctx, tx, `
				INSERT INTO tool (id, server_name, group_kind, description, created_at, updated_at)
				VALUES ($1, $2, $3, $4, NOW(), NOW())
				ON CONFLICT (id, server_name, group_kind) DO UPDATE SET
				    description = EXCLUDED.description,
				    updated_at  = NOW(),
				    deleted_at  = NULL
			`, tool.Name, serverName, groupKind, &tool.Description); err != nil {
				return fmt.Errorf("failed to upsert tool %s: %w", tool.Name, err)
			}
		}
		return nil
	})
}

// GetToolServer returns an undeleted server with the given name, or ErrNotFound. If the
// name occurs in multiple group kinds, this lookup does not select a particular kind.
func (c *Client) GetToolServer(ctx context.Context, name string) (*ToolServer, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT name, group_kind, COALESCE(created_at, '0001-01-01 00:00:00+00'::timestamptz) AS created_at,
		    COALESCE(updated_at, '0001-01-01 00:00:00+00'::timestamptz) AS updated_at,
		    deleted_at, COALESCE(description, '') AS description, last_connected FROM toolserver
		WHERE name = $1 AND deleted_at IS NULL
		LIMIT 1
	`, pgx.RowToStructByName[ToolServer], name)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool server %s: %w", name, notFoundOr(err))
	}
	return &row, nil
}

// ListToolServers returns all undeleted servers in ascending creation-time order.
func (c *Client) ListToolServers(ctx context.Context) ([]ToolServer, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT name, group_kind, COALESCE(created_at, '0001-01-01 00:00:00+00'::timestamptz) AS created_at,
		    COALESCE(updated_at, '0001-01-01 00:00:00+00'::timestamptz) AS updated_at,
		    deleted_at, COALESCE(description, '') AS description, last_connected FROM toolserver
		WHERE deleted_at IS NULL
		ORDER BY toolserver.created_at ASC
	`, pgx.RowToStructByName[ToolServer])
	if err != nil {
		return nil, fmt.Errorf("failed to list tool servers: %w", err)
	}
	return rows, nil
}

// StoreToolServer inserts or updates a server by name and group kind, reviving a deleted
// entry. It preserves the original creation time and returns the stored description,
// connection time, and timestamps.
func (c *Client) StoreToolServer(ctx context.Context, ts *ToolServer) (*ToolServer, error) {
	row, err := queryOne(ctx, c.db, `
		INSERT INTO toolserver (name, group_kind, description, last_connected, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		ON CONFLICT (name, group_kind) DO UPDATE SET
		    description    = EXCLUDED.description,
		    last_connected = EXCLUDED.last_connected,
		    updated_at     = NOW(),
		    deleted_at     = NULL
		RETURNING name, group_kind, COALESCE(created_at, '0001-01-01 00:00:00+00'::timestamptz) AS created_at,
		    COALESCE(updated_at, '0001-01-01 00:00:00+00'::timestamptz) AS updated_at,
		    deleted_at, COALESCE(description, '') AS description, last_connected
	`, pgx.RowToStructByName[ToolServer], ts.Name, ts.GroupKind, &ts.Description, ts.LastConnected)
	if err != nil {
		return nil, fmt.Errorf("failed to store tool server: %w", err)
	}
	return &row, nil
}

// DeleteToolServer hides the server matching its name and group kind. Missing servers are
// a no-op; its tool catalog is managed separately.
func (c *Client) DeleteToolServer(ctx context.Context, serverName, groupKind string) error {
	return execSQL(ctx, c.db, `
		UPDATE toolserver SET deleted_at = NOW()
		WHERE name = $1 AND group_kind = $2 AND deleted_at IS NULL
	`, serverName, groupKind)
}
