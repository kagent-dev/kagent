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
    id, namespace, user_id, request_id, context_id, prepared_revision, state, operation, labels, name, data, agent_template_name, harness_name
) VALUES ($1, $2, $3, $4, $5, $6, 'CREATING', 'CREATE', $7, $8, $9, $10, $11)
ON CONFLICT (user_id, namespace, request_id) DO NOTHING
RETURNING *;

-- name: InsertA2AContext :exec
INSERT INTO a2a_context (id, namespace, user_id)
VALUES ($1, $2, $3);

-- name: InsertForkedAgentInstance :one
INSERT INTO agent_instance (
    id, namespace, user_id, request_id, context_id, prepared_revision, source_checkpoint_id,
    state, operation, labels, data, agent_template_name, harness_name
) VALUES ($1, $2, $3, $4, $5, $6, $7, 'CREATING', 'CREATE', $8, $9, $10, $11)
ON CONFLICT (user_id, namespace, request_id) DO NOTHING
RETURNING *;

-- name: GetAgentInstanceByID :one
SELECT * FROM agent_instance WHERE id = $1;

-- name: LockAgentInstance :one
SELECT * FROM agent_instance WHERE id = $1 FOR UPDATE;

-- name: GetAgentInstanceForUser :one
SELECT * FROM agent_instance WHERE namespace = $1 AND id = $2 AND user_id = $3;

-- Pair names remain queryable after a tombstone releases its runtime revision.
-- name: ListAgentInstances :many
SELECT i.* FROM agent_instance i
WHERE i.namespace = sqlc.arg(namespace)
  AND (sqlc.arg(include_deleted)::boolean OR i.deleted_at IS NULL)
  AND (sqlc.arg(all_users)::boolean OR i.user_id = sqlc.arg(user_id))
  AND (NULLIF(sqlc.arg(after_id)::text, '') IS NULL OR i.id > NULLIF(sqlc.arg(after_id)::text, '')::uuid)
  AND i.labels @> sqlc.arg(match_labels)::jsonb
  AND (sqlc.arg(agent_template)::text = '' OR i.agent_template_name = sqlc.arg(agent_template))
  AND (sqlc.arg(harness)::text = '' OR i.harness_name = sqlc.arg(harness))
ORDER BY i.id
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
  AND agent_instance.deleted_at IS NULL
  AND (
    sqlc.arg(expected_operation)::text <> 'NONE'
    OR NOT EXISTS (
      SELECT 1 FROM agent_instance_checkpoint c
      WHERE c.source_instance_id = agent_instance.id AND c.state = 'CREATING'
    )
  )
RETURNING *;

-- Renames an instance in place. The row's `data` blob also carries the message,
-- but `toAgentInstance` reads the name from this column, exactly as it does for
-- `state` and `operation`, so the column is the single authority and the two
-- cannot drift.
-- name: UpdateAgentInstanceName :one
UPDATE agent_instance
SET name = sqlc.arg(name)
WHERE namespace = sqlc.arg(namespace) AND id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND deleted_at IS NULL AND operation <> 'DELETE'
RETURNING *;

-- name: TombstoneAgentInstance :one
UPDATE agent_instance
SET state = 'DELETED', operation = 'NONE', deleted_at = clock_timestamp(),
    prepared_revision = NULL, source_checkpoint_id = NULL, data = $2
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: DeleteAgentInstanceShares :exec
DELETE FROM agent_instance_share WHERE instance_id = $1;

-- name: CreateAgentInstanceShare :one
INSERT INTO agent_instance_share (
    id, namespace, instance_id, permission, token_hash
) VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- Resolves a share token to the share and the instance's owner.
--
-- The owner is joined in because that is what the share grants: the reader is
-- authenticated as themselves, and the token widens what that account may read to
-- what the *owner* can see. Without the owner's user id the instance lookup would
-- run as the visitor and find nothing.
-- name: GetAgentInstanceShareByTokenHash :one
SELECT s.*, i.user_id AS owner_user_id
FROM agent_instance_share s
JOIN agent_instance i ON i.id = s.instance_id
WHERE s.token_hash = $1 AND i.deleted_at IS NULL AND i.operation <> 'DELETE';

-- name: ListAgentInstanceShares :many
SELECT s.* FROM agent_instance_share s
JOIN agent_instance i ON i.id = s.instance_id
WHERE s.namespace = $1 AND s.instance_id = $2 AND i.user_id = $3 AND i.deleted_at IS NULL
  AND (NULLIF(sqlc.arg(after_id)::text, '') IS NULL OR s.id > NULLIF(sqlc.arg(after_id)::text, '')::uuid)
ORDER BY s.id
LIMIT sqlc.arg(page_size);

-- name: DeleteAgentInstanceShare :execrows
DELETE FROM agent_instance_share s
USING agent_instance i
WHERE s.namespace = $1 AND s.id = $2
  AND i.id = s.instance_id AND i.user_id = $3;
