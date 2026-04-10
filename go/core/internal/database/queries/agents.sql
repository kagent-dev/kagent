-- name: GetAgent :one
SELECT * FROM agent
WHERE id = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: ListAgents :many
SELECT * FROM agent
WHERE deleted_at IS NULL
ORDER BY created_at ASC;

-- name: UpsertAgent :exec
INSERT INTO agent (id, type, workload_type, config, created_at, updated_at)
VALUES ($1, $2, $3, $4, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
    type       = EXCLUDED.type,
    workload_type = EXCLUDED.workload_type,
    config     = EXCLUDED.config,
    updated_at = NOW(),
    deleted_at = NULL;

-- name: SoftDeleteAgent :exec
UPDATE agent SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL;
