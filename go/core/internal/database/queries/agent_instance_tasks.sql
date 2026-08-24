-- name: UpsertAgentInstanceTask :exec
INSERT INTO agent_instance_task (instance_id, id, state, status_timestamp, data)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (instance_id, id) DO UPDATE SET
    state = EXCLUDED.state,
    status_timestamp = EXCLUDED.status_timestamp,
    data = EXCLUDED.data,
    updated_at = NOW();

-- name: CreateAgentInstanceTask :execrows
INSERT INTO agent_instance_task (
    instance_id, id, state, status_timestamp, data, initial_message_id, request_hash
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (instance_id, initial_message_id)
    WHERE initial_message_id IS NOT NULL
DO NOTHING;

-- name: InsertAgentInstanceTaskEvent :one
INSERT INTO agent_instance_task_event (instance_id, task_id, data)
VALUES ($1, $2, $3)
RETURNING sequence;

-- name: SetAgentInstanceTaskSnapshot :exec
UPDATE agent_instance_task SET
    snapshot_atespace = $3,
    snapshot_name = $4,
    snapshot_uid = $5,
    history_sequence = $6
WHERE instance_id = $1 AND id = $2;

-- name: GetAgentInstanceTask :one
SELECT * FROM agent_instance_task
WHERE instance_id = $1 AND id = $2;

-- name: GetActiveAgentInstanceTask :one
SELECT * FROM agent_instance_task
WHERE instance_id = $1
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
WHERE instance_id = $1 AND initial_message_id = $2;

-- name: CountAgentInstanceTasks :one
SELECT COUNT(*) FROM agent_instance_task
WHERE instance_id = sqlc.arg(instance_id)
  AND (sqlc.arg(state)::text = '' OR state = sqlc.arg(state))
  AND (sqlc.narg(status_timestamp_after)::timestamptz IS NULL
       OR status_timestamp > sqlc.narg(status_timestamp_after));

-- name: ListAgentInstanceTasks :many
SELECT * FROM agent_instance_task
WHERE instance_id = sqlc.arg(instance_id)
  AND id > sqlc.arg(after_id)
  AND (sqlc.arg(state)::text = '' OR state = sqlc.arg(state))
  AND (sqlc.narg(status_timestamp_after)::timestamptz IS NULL
       OR status_timestamp > sqlc.narg(status_timestamp_after))
ORDER BY id
LIMIT sqlc.arg(page_size);

-- LockActiveAgentInstanceTask holds the instance's non-terminal task for the
-- rest of the transaction so reclamation cannot overwrite concurrent progress.
-- name: LockActiveAgentInstanceTask :one
SELECT * FROM agent_instance_task
WHERE instance_id = $1
  AND state NOT IN (
      'TASK_STATE_COMPLETED',
      'TASK_STATE_CANCELED',
      'TASK_STATE_FAILED',
      'TASK_STATE_REJECTED',
      'TASK_STATE_INPUT_REQUIRED',
      'TASK_STATE_AUTH_REQUIRED'
  )
FOR UPDATE;
