-- name: UpsertAgentInstanceTask :exec
INSERT INTO agent_instance_task (context_id, id, state, status_timestamp, data)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (context_id, id) DO UPDATE SET
    state = EXCLUDED.state,
    status_timestamp = EXCLUDED.status_timestamp,
    data = EXCLUDED.data,
    updated_at = NOW();

-- name: CreateAgentInstanceTask :execrows
INSERT INTO agent_instance_task (
    context_id, id, state, status_timestamp, data, initial_message_id, request_hash
)
SELECT $1, $2, $3, $4, $5, $6, $7
WHERE NOT EXISTS (
    SELECT 1 FROM agent_instance_checkpoint
    WHERE source_context_id = $1 AND state = 'CREATING'
)
ON CONFLICT (context_id, initial_message_id)
    WHERE initial_message_id IS NOT NULL
DO NOTHING;

-- name: InsertAgentInstanceTaskEvent :one
WITH inserted AS (
    INSERT INTO agent_instance_task_event (context_id, task_id, message_id, data)
    VALUES ($1, $2, $3, $4)
    ON CONFLICT (context_id, task_id, message_id)
        WHERE message_id IS NOT NULL
    DO NOTHING
    RETURNING sequence
)
SELECT sequence FROM inserted
UNION ALL
SELECT sequence FROM agent_instance_task_event
WHERE context_id = $1 AND task_id IS NOT DISTINCT FROM $2 AND message_id = $3
LIMIT 1;

-- name: ListAgentInstanceTaskHistory :many
SELECT task_id, data
FROM agent_instance_task_event
WHERE context_id = sqlc.arg(context_id)
  AND task_id = ANY(sqlc.arg(task_ids)::text[])
  AND message_id IS NOT NULL
ORDER BY sequence;

-- name: SetAgentInstanceTaskSnapshot :exec
UPDATE agent_instance_task SET
    snapshot_atespace = $3,
    snapshot_name = $4,
    snapshot_uid = $5,
    snapshot_content_scope = $6,
    history_sequence = $7
WHERE context_id = $1 AND id = $2;

-- name: GetAgentInstanceTask :one
SELECT * FROM agent_instance_task
WHERE context_id = $1 AND id = $2;

-- name: GetActiveAgentInstanceTask :one
SELECT * FROM agent_instance_task
WHERE context_id = $1
  AND state NOT IN (
      'TASK_STATE_COMPLETED',
      'TASK_STATE_CANCELED',
      'TASK_STATE_FAILED',
      'TASK_STATE_REJECTED',
      'TASK_STATE_INPUT_REQUIRED',
      'TASK_STATE_AUTH_REQUIRED'
  );

-- name: GetAgentInstanceTaskByMessageID :one
SELECT * FROM agent_instance_task
WHERE context_id = $1 AND initial_message_id = $2;

-- name: CountAgentInstanceTasks :one
SELECT COUNT(*) FROM agent_instance_task
WHERE context_id = sqlc.arg(context_id)
  AND (sqlc.arg(state)::text = '' OR state = sqlc.arg(state))
  AND (sqlc.narg(status_timestamp_after)::timestamptz IS NULL
       OR status_timestamp > sqlc.narg(status_timestamp_after));

-- name: ListAgentInstanceTasks :many
SELECT * FROM agent_instance_task
WHERE context_id = sqlc.arg(context_id)
  AND id > sqlc.arg(after_id)
  AND (sqlc.arg(state)::text = '' OR state = sqlc.arg(state))
  AND (sqlc.narg(status_timestamp_after)::timestamptz IS NULL
       OR status_timestamp > sqlc.narg(status_timestamp_after))
ORDER BY id
LIMIT sqlc.arg(page_size);

-- name: InsertCopiedAgentInstanceTask :exec
INSERT INTO agent_instance_task (
    context_id, id, state, status_timestamp, data, created_at, updated_at,
    initial_message_id, request_hash, snapshot_atespace, snapshot_name,
    snapshot_uid, snapshot_content_scope, history_sequence
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14);

-- LockActiveAgentInstanceTask holds the instance's non-terminal task for the
-- rest of the transaction so reclamation cannot overwrite concurrent progress.
-- name: LockAgentInstanceTask :one
--
-- One task by id, whatever state it is in.
--
-- Distinct from LockActiveAgentInstanceTask, which finds whichever task currently
-- holds the instance's turn — and deliberately no longer counts a parked one, since a
-- question awaiting an answer must not stop the next turn starting. The parked-task
-- operations still need to reach that exact task to answer it or give it up, so they
-- name it instead of asking for the active one.
SELECT * FROM agent_instance_task
WHERE context_id = $1 AND id = $2
FOR UPDATE;

-- name: LockActiveAgentInstanceTask :one
SELECT * FROM agent_instance_task
WHERE context_id = $1
  AND state NOT IN (
      'TASK_STATE_COMPLETED',
      'TASK_STATE_CANCELED',
      'TASK_STATE_FAILED',
      'TASK_STATE_REJECTED',
      'TASK_STATE_INPUT_REQUIRED',
      'TASK_STATE_AUTH_REQUIRED'
  )
FOR UPDATE;
