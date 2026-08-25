-- name: GetAgentInstanceCheckpointByRequest :one
SELECT * FROM agent_instance_checkpoint
WHERE user_id = $1 AND namespace = $2 AND request_id = $3;

-- name: GetLatestQuiescentAgentInstanceTask :one
SELECT latest.*
FROM (
    SELECT * FROM agent_instance_task
    WHERE agent_instance_task.instance_id = $1
    ORDER BY created_at DESC, id DESC
    LIMIT 1
) latest
WHERE NOT EXISTS (
    SELECT 1 FROM agent_instance_task active
    WHERE active.instance_id = $1
      AND active.state NOT IN (
          'TASK_STATE_COMPLETED',
          'TASK_STATE_CANCELED',
          'TASK_STATE_FAILED',
          'TASK_STATE_REJECTED',
          'TASK_STATE_INPUT_REQUIRED',
          'TASK_STATE_AUTH_REQUIRED'
      )
);

-- name: InsertAgentInstanceCheckpoint :one
INSERT INTO agent_instance_checkpoint (
    id, namespace, source_instance_id, user_id, request_id, head_task_id,
    history_sequence, snapshot_atespace, snapshot_name, snapshot_uid, snapshot_content_scope, state
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'CREATING')
ON CONFLICT DO NOTHING
RETURNING *;

-- name: FinalizeAgentInstanceCheckpoint :one
UPDATE agent_instance_checkpoint
SET state = CASE WHEN sqlc.arg(tag_uid)::text <> '' THEN 'READY' ELSE 'FAILED' END,
    tag_uid = sqlc.arg(tag_uid),
    failure = sqlc.arg(failure)
WHERE id = $1
  AND (
    state = 'CREATING'
    OR (state = 'READY' AND tag_uid = sqlc.arg(tag_uid)::text AND sqlc.arg(failure)::text = '')
    OR (state = 'FAILED' AND sqlc.arg(tag_uid)::text = '' AND failure = sqlc.arg(failure)::text)
  )
RETURNING *;

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
