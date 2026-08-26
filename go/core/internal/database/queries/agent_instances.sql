-- name: GetAgentInstanceByRequest :one
SELECT * FROM agent_instance
WHERE user_id = $1 AND namespace = $2 AND request_id = $3;

-- name: GetLatestRuntimeRevisionForInstance :one
SELECT r.*, p.agent_template_labels
FROM agent_template_harness_pair p
JOIN runtime_revision r ON r.revision = p.latest_successful_revision
WHERE p.namespace = $1
  AND p.agent_template_name = $2
  AND p.harness_name = $3
  AND p.retired_at IS NULL;

-- name: InsertAgentInstance :one
INSERT INTO agent_instance (
    id, namespace, user_id, request_id, context_id, prepared_revision, state, operation, labels, data
) VALUES ($1, $2, $3, $4, $5, $6, 'CREATING', 'CREATE', $7, $8)
ON CONFLICT (user_id, namespace, request_id) DO NOTHING
RETURNING *;

-- name: InsertA2AContext :exec
INSERT INTO a2a_context (id, namespace, user_id)
VALUES ($1, $2, $3);

-- name: InsertForkedAgentInstance :one
INSERT INTO agent_instance (
    id, namespace, user_id, request_id, context_id, prepared_revision, source_checkpoint_id,
    state, operation, labels, data
) VALUES ($1, $2, $3, $4, $5, $6, $7, 'CREATING', 'CREATE', $8, $9)
ON CONFLICT (user_id, namespace, request_id) DO NOTHING
RETURNING *;

-- name: GetAgentInstanceByID :one
SELECT * FROM agent_instance WHERE id = $1;

-- name: LockAgentInstance :one
SELECT * FROM agent_instance WHERE id = $1 FOR UPDATE;

-- name: GetAgentInstanceForUser :one
SELECT * FROM agent_instance WHERE namespace = $1 AND id = $2 AND user_id = $3;

-- name: ListAgentInstances :many
SELECT * FROM agent_instance
WHERE namespace = sqlc.arg(namespace)
  AND (sqlc.arg(all_users)::boolean OR user_id = sqlc.arg(user_id))
  AND id > sqlc.arg(after_id)
  AND labels @> sqlc.arg(match_labels)::jsonb
ORDER BY id
LIMIT sqlc.arg(page_size);

-- name: MarkAgentInstanceReady :one
UPDATE agent_instance
SET state = 'READY', operation = 'NONE', data = $2
WHERE id = $1 AND state = 'CREATING' AND operation = 'CREATE'
RETURNING *;

-- name: TransitionAgentInstance :one
UPDATE agent_instance
SET state = sqlc.arg(next_state), operation = sqlc.arg(next_operation), data = sqlc.arg(data)
WHERE agent_instance.id = sqlc.arg(id)
  AND agent_instance.state = sqlc.arg(expected_state)
  AND agent_instance.operation = sqlc.arg(expected_operation)
  AND (
    sqlc.arg(expected_operation)::text <> 'NONE'
    OR NOT EXISTS (
      SELECT 1 FROM agent_instance_checkpoint c
      WHERE c.source_instance_id = agent_instance.id AND c.state = 'CREATING'
    )
  )
RETURNING *;

-- name: DeleteAgentInstance :exec
DELETE FROM agent_instance WHERE id = $1;

-- name: CreateAgentInstanceShare :one
INSERT INTO agent_instance_share (
    id, namespace, instance_id, creator, permission, token_hash
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListAgentInstanceShares :many
SELECT s.* FROM agent_instance_share s
JOIN agent_instance i ON i.id = s.instance_id
WHERE s.namespace = $1 AND s.instance_id = $2 AND i.user_id = $3
  AND s.id > sqlc.arg(after_id)
ORDER BY s.id
LIMIT sqlc.arg(page_size);

-- name: DeleteAgentInstanceShare :execrows
DELETE FROM agent_instance_share s
USING agent_instance i
WHERE s.namespace = $1 AND s.id = $2
  AND i.id = s.instance_id AND i.user_id = $3;
