-- name: UpsertScheduledRunExecution :exec
INSERT INTO scheduled_run_executions (
    id, scheduled_run_namespace, scheduled_run_name, scheduled_run_uid,
    start_time, completion_time, trigger, session_id, task_id, status, status_message
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (id) DO UPDATE SET
    completion_time = EXCLUDED.completion_time,
    session_id = EXCLUDED.session_id,
    task_id = EXCLUDED.task_id,
    status = EXCLUDED.status,
    status_message = EXCLUDED.status_message,
    updated_at = NOW();

-- name: ListScheduledRunExecutions :many
SELECT * FROM scheduled_run_executions
WHERE scheduled_run_namespace = $1
  AND scheduled_run_name = $2
  AND scheduled_run_uid = $3
  AND (
    sqlc.narg('before')::timestamptz IS NULL
    OR start_time < sqlc.narg('before')
    OR (
      sqlc.narg('before_id')::text IS NOT NULL
      AND start_time = sqlc.narg('before')
      AND id < sqlc.narg('before_id')
    )
  )
ORDER BY start_time DESC, id DESC
LIMIT sqlc.arg('page_limit');

-- name: GetScheduledRunExecutionBySessionID :one
SELECT * FROM scheduled_run_executions
WHERE session_id = $1
LIMIT 1;

-- name: ListInProgressScheduledRunExecutions :many
SELECT * FROM scheduled_run_executions
WHERE status = 'InProgress'
ORDER BY start_time ASC, id ASC;
