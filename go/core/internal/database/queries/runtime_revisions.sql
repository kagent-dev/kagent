-- name: UpsertAgentTemplateHarnessPair :exec
INSERT INTO agent_template_harness_pair (
    namespace, agent_template_name, agent_template_uid,
    harness_name, harness_uid, desired_revision, agent_template_labels, retired_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, NULL)
ON CONFLICT (namespace, agent_template_uid, harness_uid) DO UPDATE SET
    agent_template_name = EXCLUDED.agent_template_name,
    harness_name = EXCLUDED.harness_name,
    desired_revision = EXCLUDED.desired_revision,
    agent_template_labels = EXCLUDED.agent_template_labels,
    retired_at = NULL,
    updated_at = NOW();

-- name: UpsertRuntimeRevision :exec
INSERT INTO runtime_revision (
    revision, namespace, agent_template_name, agent_template_uid,
    harness_name, harness_uid, source_snapshot, agent_card, egress_destinations,
    actor_template_atespace, actor_template_name, actor_template_uid
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8,
    $9, $10, $11, $12
)
ON CONFLICT (revision) DO UPDATE SET
    agent_card = EXCLUDED.agent_card,
    actor_template_uid = EXCLUDED.actor_template_uid,
    updated_at = NOW();

-- name: MarkRuntimeRevisionSuccessful :exec
UPDATE agent_template_harness_pair
SET latest_successful_revision = sqlc.arg(revision), updated_at = NOW()
WHERE namespace = sqlc.arg(namespace)
  AND agent_template_uid = sqlc.arg(agent_template_uid)
  AND harness_uid = sqlc.arg(harness_uid)
  AND desired_revision = sqlc.arg(revision)
  AND retired_at IS NULL;

-- name: RetireAgentTemplateHarnessPairs :exec
UPDATE agent_template_harness_pair
SET retired_at = COALESCE(retired_at, NOW()), updated_at = NOW()
WHERE namespace = $1 AND agent_template_name = $2;

-- name: RetireAgentTemplateHarnessPair :exec
UPDATE agent_template_harness_pair
SET retired_at = COALESCE(retired_at, NOW()), updated_at = NOW()
WHERE namespace = $1 AND agent_template_name = $2 AND harness_name = $3;

-- name: RetireOtherAgentTemplateHarnessPairs :exec
UPDATE agent_template_harness_pair
SET retired_at = COALESCE(retired_at, NOW()), updated_at = NOW()
WHERE namespace = sqlc.arg(namespace)
  AND agent_template_uid = sqlc.arg(agent_template_uid)
  AND NOT (harness_name = ANY(sqlc.arg(harness_names)::text[]));

-- name: GetRuntimeRevision :one
SELECT * FROM runtime_revision WHERE revision = $1;

-- name: ListUnreferencedRuntimeRevisions :many
SELECT * FROM runtime_revision r
WHERE NOT EXISTS (
    SELECT 1 FROM agent_template_harness_pair p
    WHERE p.retired_at IS NULL
      AND (p.desired_revision = r.revision OR p.latest_successful_revision = r.revision)
)
AND NOT EXISTS (
    SELECT 1 FROM agent_instance i WHERE i.prepared_revision = r.revision
);

-- name: DeleteUnreferencedRuntimeRevision :exec
DELETE FROM runtime_revision r
WHERE r.revision = $1
  AND NOT EXISTS (
      SELECT 1 FROM agent_template_harness_pair p
      WHERE p.retired_at IS NULL
        AND (p.desired_revision = r.revision OR p.latest_successful_revision = r.revision)
  )
  AND NOT EXISTS (
      SELECT 1 FROM agent_instance i WHERE i.prepared_revision = r.revision
  );
