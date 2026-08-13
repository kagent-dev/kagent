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
    id, namespace, user_id, request_id, prepared_revision, state, labels, data
) VALUES ($1, $2, $3, $4, $5, 'CREATING', $6, $7)
ON CONFLICT (user_id, namespace, request_id) DO NOTHING
RETURNING *;

-- name: GetAgentInstanceByID :one
SELECT * FROM agent_instance WHERE id = $1;

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
SET state = 'READY', data = $2
WHERE id = $1 AND state = 'CREATING'
RETURNING *;

-- name: DeleteAgentInstance :exec
DELETE FROM agent_instance WHERE id = $1;

-- name: RecordAgentInstanceActorUID :one
UPDATE agent_instance
SET actor_uid = COALESCE(NULLIF(actor_uid, ''), sqlc.arg(actor_uid))
WHERE id = sqlc.arg(id)
RETURNING actor_uid;

-- name: CreateAgentInstanceShare :one
INSERT INTO agent_instance_share (
    id, namespace, instance_id, creator, permission, token_hash
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListAgentInstanceShares :many
SELECT s.* FROM agent_instance_share s
JOIN agent_instance i ON i.id = s.instance_id
WHERE s.namespace = $1 AND s.instance_id = $2 AND i.user_id = $3
ORDER BY s.id;

-- name: DeleteAgentInstanceShare :execrows
DELETE FROM agent_instance_share s
USING agent_instance i
WHERE s.namespace = $1 AND s.id = $2
  AND i.id = s.instance_id AND i.user_id = $3;
