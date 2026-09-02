-- name: GetAgentInstanceCheckpointByRequest :one
SELECT * FROM agent_instance_checkpoint
WHERE user_id = $1 AND namespace = $2 AND request_id = $3;

-- name: GetLatestQuiescentAgentInstanceTask :one
SELECT latest.*
FROM (
    SELECT * FROM agent_instance_task
    WHERE agent_instance_task.context_id = $1
    ORDER BY created_at DESC, id DESC
    LIMIT 1
) latest
WHERE NOT EXISTS (
    SELECT 1 FROM agent_instance_task active
    WHERE active.context_id = $1
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
    history_sequence, snapshot_atespace, snapshot_name, snapshot_uid, snapshot_content_scope,
    source_context_id, prepared_revision, source_labels, state
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, 'CREATING')
ON CONFLICT DO NOTHING
RETURNING *;

-- name: ListAgentInstanceCheckpointTasks :many
SELECT t.*
FROM agent_instance_checkpoint c
JOIN agent_instance_task head
  ON head.context_id = c.source_context_id AND head.id = c.head_task_id
JOIN agent_instance_task t
  ON t.context_id = c.source_context_id
 AND (t.created_at, t.id) <= (head.created_at, head.id)
WHERE c.id = sqlc.arg(checkpoint_id)
ORDER BY t.created_at, t.id;

-- name: ListAgentInstanceCheckpointEvents :many
SELECT e.*
FROM agent_instance_checkpoint c
JOIN agent_instance_task_event e
  ON e.context_id = c.source_context_id
 AND e.sequence <= c.history_sequence
WHERE c.id = sqlc.arg(checkpoint_id)
ORDER BY e.sequence;

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
  AND (NULLIF(sqlc.arg(after_id)::text, '') IS NULL OR id > NULLIF(sqlc.arg(after_id)::text, '')::uuid)
ORDER BY id
LIMIT sqlc.arg(page_size);

-- name: BeginDeleteAgentInstanceCheckpoint :one
UPDATE agent_instance_checkpoint
SET state = 'DELETING'
WHERE agent_instance_checkpoint.namespace = $1 AND agent_instance_checkpoint.id = $2 AND agent_instance_checkpoint.user_id = $3
  AND agent_instance_checkpoint.state IN ('READY', 'DELETING')
  AND NOT EXISTS (
      SELECT 1 FROM agent_instance i WHERE i.source_checkpoint_id = agent_instance_checkpoint.id
  )
RETURNING *;

-- name: DeleteAgentInstanceCheckpoint :execrows
DELETE FROM agent_instance_checkpoint
WHERE namespace = $1 AND id = $2 AND user_id = $3 AND state = 'DELETING';

-- name: LockReadyAgentInstanceCheckpoint :one
SELECT * FROM agent_instance_checkpoint
WHERE namespace = $1 AND id = $2 AND user_id = $3 AND state = 'READY'
FOR UPDATE;
