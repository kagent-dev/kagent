package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func (c *Client) GetTool(ctx context.Context, name string) (*Tool, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT id, server_name, group_kind, created_at, updated_at, deleted_at, description FROM tool
		WHERE id = $1 AND deleted_at IS NULL
		LIMIT 1
	`, pgx.RowToStructByName[toolRow], name)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool %s: %w", name, notFoundOr(err))
	}
	return toTool(row), nil
}

func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	return c.listTools(ctx, nil, nil)
}

func (c *Client) ListToolsForServer(ctx context.Context, serverName, groupKind string) ([]Tool, error) {
	return c.listTools(ctx, &serverName, &groupKind)
}

func (c *Client) listTools(ctx context.Context, serverName, groupKind *string) ([]Tool, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT id, server_name, group_kind, created_at, updated_at, deleted_at, description FROM tool
		WHERE deleted_at IS NULL
		  AND ($1::text IS NULL OR server_name = $1)
		  AND ($2::text IS NULL OR group_kind = $2)
		ORDER BY created_at ASC
	`, pgx.RowToStructByName[toolRow], serverName, groupKind)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}
	tools := make([]Tool, len(rows))
	for i, r := range rows {
		tools[i] = *toTool(r)
	}
	return tools, nil
}

func (c *Client) DeleteToolsForServer(ctx context.Context, serverName, groupKind string) error {
	return execSQL(ctx, c.db, `
		UPDATE tool SET deleted_at = NOW()
		WHERE server_name = $1 AND group_kind = $2 AND deleted_at IS NULL
	`, serverName, groupKind)
}

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

func (c *Client) GetToolServer(ctx context.Context, name string) (*ToolServer, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT name, group_kind, created_at, updated_at, deleted_at, description, last_connected FROM toolserver
		WHERE name = $1 AND deleted_at IS NULL
		LIMIT 1
	`, pgx.RowToStructByName[toolServerRow], name)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool server %s: %w", name, notFoundOr(err))
	}
	return toToolServer(row), nil
}

func (c *Client) ListToolServers(ctx context.Context) ([]ToolServer, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT name, group_kind, created_at, updated_at, deleted_at, description, last_connected FROM toolserver
		WHERE deleted_at IS NULL
		ORDER BY created_at ASC
	`, pgx.RowToStructByName[toolServerRow])
	if err != nil {
		return nil, fmt.Errorf("failed to list tool servers: %w", err)
	}
	servers := make([]ToolServer, len(rows))
	for i, r := range rows {
		servers[i] = *toToolServer(r)
	}
	return servers, nil
}

func (c *Client) StoreToolServer(ctx context.Context, ts *ToolServer) (*ToolServer, error) {
	row, err := queryOne(ctx, c.db, `
		INSERT INTO toolserver (name, group_kind, description, last_connected, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		ON CONFLICT (name, group_kind) DO UPDATE SET
		    description    = EXCLUDED.description,
		    last_connected = EXCLUDED.last_connected,
		    updated_at     = NOW(),
		    deleted_at     = NULL
		RETURNING name, group_kind, created_at, updated_at, deleted_at, description, last_connected
	`, pgx.RowToStructByName[toolServerRow], ts.Name, ts.GroupKind, &ts.Description, ts.LastConnected)
	if err != nil {
		return nil, fmt.Errorf("failed to store tool server: %w", err)
	}
	return toToolServer(row), nil
}

func (c *Client) DeleteToolServer(ctx context.Context, serverName, groupKind string) error {
	return execSQL(ctx, c.db, `
		UPDATE toolserver SET deleted_at = NOW()
		WHERE name = $1 AND group_kind = $2 AND deleted_at IS NULL
	`, serverName, groupKind)
}

func toTool(r toolRow) *Tool {
	return &Tool{
		ID:          r.ID,
		ServerName:  r.ServerName,
		GroupKind:   r.GroupKind,
		CreatedAt:   derefTime(r.CreatedAt),
		UpdatedAt:   derefTime(r.UpdatedAt),
		DeletedAt:   r.DeletedAt,
		Description: derefStr(r.Description),
	}
}

func toToolServer(r toolServerRow) *ToolServer {
	return &ToolServer{
		Name:          r.Name,
		GroupKind:     r.GroupKind,
		CreatedAt:     derefTime(r.CreatedAt),
		UpdatedAt:     derefTime(r.UpdatedAt),
		DeletedAt:     r.DeletedAt,
		Description:   derefStr(r.Description),
		LastConnected: r.LastConnected,
	}
}

type toolRow struct {
	ID          string
	ServerName  string
	GroupKind   string
	CreatedAt   *time.Time
	UpdatedAt   *time.Time
	DeletedAt   *time.Time
	Description *string
}

type toolServerRow struct {
	Name          string
	GroupKind     string
	CreatedAt     *time.Time
	UpdatedAt     *time.Time
	DeletedAt     *time.Time
	Description   *string
	LastConnected *time.Time
}
