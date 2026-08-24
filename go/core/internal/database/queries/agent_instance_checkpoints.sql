-- name: GetAgentInstanceCheckpointByRequest :one
SELECT * FROM agent_instance_checkpoint
WHERE user_id = $1 AND namespace = $2 AND request_id = $3;

-- name: GetLatestAgentInstanceTask :one
SELECT * FROM agent_instance_task
WHERE instance_id = $1
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: InsertAgentInstanceCheckpoint :one
INSERT INTO agent_instance_checkpoint (
    id, namespace, source_instance_id, user_id, request_id, head_task_id,
    history_sequence, snapshot_atespace, snapshot_name, snapshot_uid, snapshot_content_scope, state
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'CREATING')
RETURNING *;

-- name: GetCreatingAgentInstanceCheckpoint :one
SELECT * FROM agent_instance_checkpoint
WHERE source_instance_id = $1 AND state = 'CREATING';

-- name: MarkAgentInstanceCheckpointReady :one
UPDATE agent_instance_checkpoint
SET state = 'READY', tag_uid = $2, failure = ''
WHERE id = $1 AND state = 'CREATING'
RETURNING *;

-- name: MarkAgentInstanceCheckpointFailed :exec
UPDATE agent_instance_checkpoint
SET state = 'FAILED', failure = $2
WHERE id = $1 AND state = 'CREATING';

-- name: GetAgentInstanceCheckpointByID :one
SELECT * FROM agent_instance_checkpoint WHERE id = $1;

-- name: GetAgentInstanceCheckpoint :one
SELECT * FROM agent_instance_checkpoint
WHERE namespace = $1 AND id = $2 AND user_id = $3 AND state = 'READY';

-- name: ListAgentInstanceCheckpoints :many
SELECT * FROM agent_instance_checkpoint
WHERE namespace = sqlc.arg(namespace)
  AND source_instance_id = sqlc.arg(source_instance_id)
  AND user_id = sqlc.arg(user_id)
  AND state = 'READY'
  AND id > sqlc.arg(after_id)
ORDER BY id
LIMIT sqlc.arg(page_size);

-- name: BeginDeleteAgentInstanceCheckpoint :one
UPDATE agent_instance_checkpoint
SET state = 'DELETING'
WHERE namespace = $1 AND id = $2 AND user_id = $3
  AND state IN ('READY', 'DELETING')
RETURNING *;

-- name: DeleteAgentInstanceCheckpoint :execrows
DELETE FROM agent_instance_checkpoint
WHERE namespace = $1 AND id = $2 AND user_id = $3 AND state = 'DELETING';
