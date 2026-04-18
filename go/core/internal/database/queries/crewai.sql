-- name: UpsertCrewAIMemory :exec
INSERT INTO crewai_agent_memory (user_id, thread_id, memory_data, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
ON CONFLICT (user_id, thread_id) DO UPDATE SET
    memory_data = EXCLUDED.memory_data,
    updated_at  = NOW(),
    deleted_at  = NULL;

-- name: SearchCrewAIMemoryByTask :many
SELECT * FROM crewai_agent_memory
WHERE user_id = $1 AND thread_id = $2 AND deleted_at IS NULL
  AND (memory_data ILIKE $3 OR (memory_data::jsonb)->>'task_description' ILIKE $3)
ORDER BY created_at DESC, (memory_data::jsonb)->>'score' ASC;

-- name: SearchCrewAIMemoryByTaskLimit :many
SELECT * FROM crewai_agent_memory
WHERE user_id = $1 AND thread_id = $2 AND deleted_at IS NULL
  AND (memory_data ILIKE $3 OR (memory_data::jsonb)->>'task_description' ILIKE $3)
ORDER BY created_at DESC, (memory_data::jsonb)->>'score' ASC
LIMIT $4;

-- name: HardDeleteCrewAIMemory :exec
DELETE FROM crewai_agent_memory
WHERE user_id = $1 AND thread_id = $2;

-- name: UpsertCrewAIFlowState :exec
INSERT INTO crewai_flow_state (user_id, thread_id, method_name, state_data, created_at, updated_at)
VALUES ($1, $2, $3, $4, NOW(), NOW())
ON CONFLICT (user_id, thread_id, method_name) DO UPDATE SET
    state_data = EXCLUDED.state_data,
    updated_at = NOW(),
    deleted_at = NULL;

-- name: GetLatestCrewAIFlowState :one
SELECT * FROM crewai_flow_state
WHERE user_id = $1 AND thread_id = $2 AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT 1;
