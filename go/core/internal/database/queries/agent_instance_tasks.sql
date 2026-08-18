-- name: UpsertAgentInstanceTask :exec
INSERT INTO agent_instance_task (instance_id, id, state, status_timestamp, data)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (instance_id, id) DO UPDATE SET
    state = EXCLUDED.state,
    status_timestamp = EXCLUDED.status_timestamp,
    data = EXCLUDED.data,
    updated_at = NOW();

-- name: InsertAgentInstanceTaskEvent :exec
INSERT INTO agent_instance_task_event (instance_id, task_id, data)
VALUES ($1, $2, $3);

-- name: GetAgentInstanceTask :one
SELECT * FROM agent_instance_task
WHERE instance_id = $1 AND id = $2;

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
