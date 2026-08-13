-- name: GetAgentInstanceByRequest :one
SELECT * FROM agent_instance
WHERE creator = $1 AND namespace = $2 AND request_id = $3;

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
    id, namespace, creator, request_id, prepared_revision, state, labels, data
) VALUES ($1, $2, $3, $4, $5, 'CREATING', $6, $7)
ON CONFLICT (creator, namespace, request_id) DO NOTHING
RETURNING *;

-- name: GetAgentInstance :one
SELECT * FROM agent_instance WHERE namespace = $1 AND id = $2;

-- name: GetAgentInstanceByID :one
SELECT * FROM agent_instance WHERE id = $1;

-- name: GetOwnedAgentInstance :one
SELECT * FROM agent_instance WHERE namespace = $1 AND id = $2 AND creator = $3;

-- name: ListOwnedAgentInstances :many
SELECT * FROM agent_instance
WHERE namespace = sqlc.arg(namespace)
  AND creator = sqlc.arg(creator)
  AND id > sqlc.arg(after_id)
  AND labels @> sqlc.arg(match_labels)::jsonb
ORDER BY id
LIMIT sqlc.arg(page_size);

-- name: ListAllAgentInstances :many
SELECT * FROM agent_instance
WHERE namespace = sqlc.arg(namespace)
  AND id > sqlc.arg(after_id)
  AND labels @> sqlc.arg(match_labels)::jsonb
ORDER BY id
LIMIT sqlc.arg(page_size);

-- name: MarkAgentInstanceReady :one
UPDATE agent_instance
SET state = 'READY', data = $2
WHERE id = $1 AND state = 'CREATING'
RETURNING *;

-- name: MarkAgentInstanceDeleting :one
UPDATE agent_instance
SET state = 'DELETING', data = $4
WHERE namespace = $1 AND id = $2 AND creator = $3 AND state <> 'DELETED'
RETURNING *;

-- name: MarkAgentInstanceDeleted :one
UPDATE agent_instance
SET state = 'DELETED', prepared_revision = NULL, data = $2
WHERE id = $1 AND state = 'DELETING'
RETURNING *;

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
WHERE s.namespace = $1 AND s.instance_id = $2 AND i.creator = $3
ORDER BY s.id;

-- name: DeleteAgentInstanceShare :execrows
DELETE FROM agent_instance_share s
USING agent_instance i
WHERE s.namespace = $1 AND s.id = $2
  AND i.id = s.instance_id AND i.creator = $3;
