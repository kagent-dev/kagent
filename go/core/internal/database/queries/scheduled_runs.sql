-- name: FindScheduledRunRequest :one
SELECT * FROM scheduled_run WHERE creator = $1 AND request_id = $2;

-- name: CreateScheduledRun :one
INSERT INTO scheduled_run (id, creator, request_id, request_hash, data, next_execution_time)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (creator, request_id) DO NOTHING RETURNING *;

-- name: GetScheduledRun :one
SELECT * FROM scheduled_run WHERE creator = $1 AND id = $2;

-- name: GetScheduledRunForUpdate :one
SELECT * FROM scheduled_run WHERE creator = $1 AND id = $2 FOR UPDATE;

-- name: ListScheduledRuns :many
SELECT * FROM scheduled_run WHERE creator = $1 AND deleted_at IS NULL
  AND (sqlc.narg(after_id)::uuid IS NULL OR id > sqlc.narg(after_id)::uuid)
ORDER BY id LIMIT $2;

-- name: SaveScheduledRun :one
UPDATE scheduled_run SET data = $2, next_execution_time = $3, deleted_at = $4
WHERE id = $1 RETURNING *;

-- name: GetDueScheduledRunsForUpdate :many
SELECT * FROM scheduled_run
WHERE deleted_at IS NULL AND next_execution_time <= sqlc.arg(now)::timestamptz
ORDER BY next_execution_time, id LIMIT $1 FOR UPDATE SKIP LOCKED;

-- name: AdvanceScheduledRun :exec
UPDATE scheduled_run SET next_execution_time = $2 WHERE id = $1;

-- name: DatabaseNow :one
SELECT clock_timestamp()::timestamptz AS now;

-- name: FindManualScheduledRunExecution :one
SELECT * FROM scheduled_run_execution WHERE scheduled_run_id = $1 AND manual_request_id = $2;

-- name: CreateScheduledRunExecution :one
INSERT INTO scheduled_run_execution (id, scheduled_run_id, scheduled_time, manual_request_id, data)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: GetScheduledRunExecution :one
SELECT e.* FROM scheduled_run_execution e JOIN scheduled_run s ON s.id = e.scheduled_run_id
WHERE s.creator = $1 AND e.id = $2;

-- name: GetScheduledRunExecutionForUpdate :one
SELECT e.* FROM scheduled_run_execution e JOIN scheduled_run s ON s.id = e.scheduled_run_id
WHERE s.creator = $1 AND e.id = $2 FOR UPDATE OF e;

-- name: ListScheduledRunExecutions :many
SELECT e.* FROM scheduled_run_execution e JOIN scheduled_run s ON s.id = e.scheduled_run_id
WHERE s.creator = $1 AND e.scheduled_run_id = $2
  AND (sqlc.narg(after_id)::uuid IS NULL OR e.id < sqlc.narg(after_id)::uuid)
ORDER BY e.id DESC LIMIT $3;

-- name: SetScheduledRunExecutionInstance :one
UPDATE scheduled_run_execution SET agent_instance_id = $2 WHERE id = $1 RETURNING *;

-- name: ExpireScheduledRunExecution :one
UPDATE scheduled_run_execution SET state = 'TIMED_OUT', completed_at = clock_timestamp(), data = $2
WHERE id = $1 RETURNING *;

-- name: LeaseScheduledRunExecutions :many
WITH candidates AS (
    SELECT id FROM scheduled_run_execution
    WHERE state IN ('PENDING', 'RUNNING') AND next_attempt_at <= clock_timestamp()
    ORDER BY next_attempt_at, id LIMIT $1 FOR UPDATE SKIP LOCKED
)
UPDATE scheduled_run_execution e
SET lease_token = sqlc.arg(lease_token)::uuid, next_attempt_at = clock_timestamp() + interval '30 seconds'
FROM candidates c WHERE e.id = c.id RETURNING e.*;

-- name: GetLeasedScheduledRunExecutionForUpdate :one
SELECT * FROM scheduled_run_execution
WHERE id = $1 AND lease_token = $2 AND next_attempt_at > clock_timestamp()
  AND state IN ('PENDING', 'RUNNING') FOR UPDATE;

-- name: UpdateScheduledRunExecution :execrows
UPDATE scheduled_run_execution
SET state = sqlc.arg(state), task_id = COALESCE(task_id, sqlc.narg(task_id)), data = sqlc.arg(data),
    completed_at = CASE WHEN sqlc.arg(state)::text IN ('SUCCEEDED', 'FAILED', 'TIMED_OUT') THEN clock_timestamp() END,
    next_attempt_at = clock_timestamp() + interval '1 second', lease_token = NULL
WHERE id = $1 AND lease_token = sqlc.arg(lease_token)::uuid AND next_attempt_at > clock_timestamp()
    AND state IN ('PENDING', 'RUNNING')
    AND (task_id IS NULL OR sqlc.narg(task_id)::text IS NULL OR task_id = sqlc.narg(task_id));
