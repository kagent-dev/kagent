-- name: UpsertCheckpoint :exec
INSERT INTO lg_checkpoint (
    user_id, thread_id, checkpoint_ns, checkpoint_id,
    parent_checkpoint_id, metadata, checkpoint, checkpoint_type, version,
    created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
ON CONFLICT (user_id, thread_id, checkpoint_ns, checkpoint_id) DO UPDATE SET
    parent_checkpoint_id = EXCLUDED.parent_checkpoint_id,
    metadata             = EXCLUDED.metadata,
    checkpoint           = EXCLUDED.checkpoint,
    checkpoint_type      = EXCLUDED.checkpoint_type,
    version              = EXCLUDED.version,
    updated_at           = NOW();

-- name: ListCheckpoints :many
SELECT * FROM lg_checkpoint
WHERE user_id = $1 AND thread_id = $2 AND checkpoint_ns = $3
  AND deleted_at IS NULL
ORDER BY checkpoint_id DESC;

-- name: ListCheckpointsLimit :many
SELECT * FROM lg_checkpoint
WHERE user_id = $1 AND thread_id = $2 AND checkpoint_ns = $3
  AND deleted_at IS NULL
ORDER BY checkpoint_id DESC
LIMIT $4;

-- name: GetCheckpoint :one
SELECT * FROM lg_checkpoint
WHERE user_id = $1 AND thread_id = $2 AND checkpoint_ns = $3
  AND checkpoint_id = $4 AND deleted_at IS NULL
LIMIT 1;

-- name: ListCheckpointWrites :many
SELECT * FROM lg_checkpoint_write
WHERE user_id = $1 AND thread_id = $2 AND checkpoint_ns = $3
  AND checkpoint_id = $4 AND deleted_at IS NULL
ORDER BY task_id, write_idx;

-- name: UpsertCheckpointWrite :exec
INSERT INTO lg_checkpoint_write (
    user_id, thread_id, checkpoint_ns, checkpoint_id, write_idx,
    value, value_type, channel, task_id, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
ON CONFLICT (user_id, thread_id, checkpoint_ns, checkpoint_id, write_idx) DO UPDATE SET
    value      = EXCLUDED.value,
    value_type = EXCLUDED.value_type,
    channel    = EXCLUDED.channel,
    task_id    = EXCLUDED.task_id,
    updated_at = NOW();

-- name: SoftDeleteCheckpoints :exec
UPDATE lg_checkpoint SET deleted_at = NOW()
WHERE user_id = $1 AND thread_id = $2 AND deleted_at IS NULL;

-- name: SoftDeleteCheckpointWrites :exec
UPDATE lg_checkpoint_write SET deleted_at = NOW()
WHERE user_id = $1 AND thread_id = $2 AND deleted_at IS NULL;
