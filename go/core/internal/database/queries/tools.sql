-- name: GetTool :one
SELECT * FROM tool
WHERE id = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: ListTools :many
SELECT * FROM tool
WHERE deleted_at IS NULL
ORDER BY created_at ASC;

-- name: ListToolsForServer :many
SELECT * FROM tool
WHERE server_name = $1 AND group_kind = $2 AND deleted_at IS NULL
ORDER BY created_at ASC;

-- name: UpsertTool :exec
INSERT INTO tool (id, server_name, group_kind, description, created_at, updated_at)
VALUES ($1, $2, $3, $4, NOW(), NOW())
ON CONFLICT (id, server_name, group_kind) DO UPDATE SET
    description = EXCLUDED.description,
    updated_at  = NOW(),
    deleted_at  = NULL;

-- name: SoftDeleteToolsForServer :exec
UPDATE tool SET deleted_at = NOW()
WHERE server_name = $1 AND group_kind = $2 AND deleted_at IS NULL;

-- name: GetToolServer :one
SELECT * FROM toolserver
WHERE name = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: ListToolServers :many
SELECT * FROM toolserver
WHERE deleted_at IS NULL
ORDER BY created_at ASC;

-- name: UpsertToolServer :one
INSERT INTO toolserver (name, group_kind, description, last_connected, created_at, updated_at)
VALUES ($1, $2, $3, $4, NOW(), NOW())
ON CONFLICT (name, group_kind) DO UPDATE SET
    description    = EXCLUDED.description,
    last_connected = EXCLUDED.last_connected,
    updated_at     = NOW(),
    deleted_at     = NULL
RETURNING *;

-- name: SoftDeleteToolServer :exec
UPDATE toolserver SET deleted_at = NOW()
WHERE name = $1 AND group_kind = $2 AND deleted_at IS NULL;
