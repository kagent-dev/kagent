-- name: GetAgentInstanceByRequest :one
SELECT * FROM agent_instance
WHERE user_id = $1 AND request_id = $2;

-- name: GetLatestRuntimeRevisionForInstance :one
SELECT r.*, p.agent_template_labels
FROM agent_template_harness_pair p
JOIN runtime_revision r ON r.revision = p.latest_successful_revision
WHERE p.namespace = sqlc.arg(harness_namespace)
  AND p.namespace = sqlc.arg(agent_template_namespace)
  AND p.agent_template_name = sqlc.arg(agent_template_name)
  AND p.harness_name = sqlc.arg(harness_name)
  AND p.retired_at IS NULL;

-- name: InsertAgentInstance :one
INSERT INTO agent_instance (id, user_id, request_id, context_id, history_id, prepared_revision, state, operation, labels, data) VALUES ($1, $2, $3, $4, $5, $6, 'CREATING', 'CREATE', $7, $8)
ON CONFLICT (user_id, request_id) DO NOTHING
RETURNING *;

-- name: InsertA2AContext :exec
INSERT INTO a2a_context (id, user_id, context_id) VALUES ($1, $2, $3);

-- name: GetA2AContext :one
SELECT * FROM a2a_context WHERE id = $1;

-- name: GetAgentInstanceHistoryID :one
SELECT history_id FROM agent_instance WHERE id = $1;

-- name: InsertForkedAgentInstance :one
INSERT INTO agent_instance (id, user_id, request_id, context_id, history_id, prepared_revision, source_checkpoint_id, state, operation, labels, data) VALUES ($1, $2, $3, $4, $5, $6, $7, 'CREATING', 'CREATE', $8, $9)
ON CONFLICT (user_id, request_id) DO NOTHING
RETURNING *;

-- name: GetAgentInstanceByID :one
SELECT * FROM agent_instance WHERE id = $1;

-- name: LockAgentInstance :one
SELECT * FROM agent_instance WHERE id = $1 FOR UPDATE;

-- name: GetAgentInstanceForUser :one
SELECT * FROM agent_instance WHERE id = $1 AND user_id = $2;

-- Lists the conversations an instance is, optionally narrowed to one agent.
--
-- An agent is an (AgentTemplate, Harness) pair, and the instance row carries
-- neither name as a column -- both live inside `data`. They are resolved through
-- `prepared_revision`, which is a foreign key to `runtime_revision` and does
-- carry them, so the filter needs no new column and matches rows written before
-- it existed. An instance with no prepared revision belongs to no pair and
-- therefore matches no template or harness filter.
-- name: ListAgentInstances :many
SELECT i.* FROM agent_instance i
LEFT JOIN runtime_revision r ON r.revision = i.prepared_revision
WHERE (sqlc.arg(all_users)::boolean OR i.user_id = sqlc.arg(user_id))
  AND (NULLIF(sqlc.arg(after_id)::text, '') IS NULL OR i.id > NULLIF(sqlc.arg(after_id)::text, '')::uuid)
  AND i.labels @> sqlc.arg(match_labels)::jsonb
  AND (sqlc.arg(agent_template)::text = '' OR (r.agent_template_name = sqlc.arg(agent_template) AND r.namespace = sqlc.arg(agent_template_namespace)))
  AND (sqlc.arg(harness)::text = '' OR (r.harness_name = sqlc.arg(harness) AND r.namespace = sqlc.arg(harness_namespace)))
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
  AND (
    sqlc.arg(expected_operation)::text <> 'NONE'
    OR NOT EXISTS (
      SELECT 1 FROM agent_instance_checkpoint c
      WHERE c.source_instance_id = agent_instance.id AND c.state = 'CREATING'
    )
  )
RETURNING *;

-- The store locks the row and changes only the display name in the payload.
-- name: UpdateAgentInstanceName :one
UPDATE agent_instance
SET data = sqlc.arg(data)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)
RETURNING *;

-- name: DeleteAgentInstance :exec
DELETE FROM agent_instance WHERE id = $1;

-- name: CreateAgentInstanceShare :one
INSERT INTO agent_instance_share (id, instance_id, permission, token_hash, data) VALUES ($1, $2, $3, $4, $5)
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
WHERE s.token_hash = $1;

-- name: ListAgentInstanceShares :many
SELECT s.* FROM agent_instance_share s
JOIN agent_instance i ON i.id = s.instance_id
WHERE s.instance_id = $1 AND i.user_id = $2
  AND (NULLIF(sqlc.arg(after_id)::text, '') IS NULL OR s.id > NULLIF(sqlc.arg(after_id)::text, '')::uuid)
ORDER BY s.id
LIMIT sqlc.arg(page_size);

-- name: DeleteAgentInstanceShare :execrows
DELETE FROM agent_instance_share s
USING agent_instance i
WHERE s.id = $1
  AND i.id = s.instance_id AND i.user_id = $2;
