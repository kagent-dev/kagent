-- name: UpsertAgentTemplateAttachment :exec
INSERT INTO agent_template_attachment (
    namespace, agent_template_name, agent_template_uid,
    harness_name, harness_uid, desired_revision, retired_at
) VALUES ($1, $2, $3, $4, $5, $6, NULL)
ON CONFLICT (namespace, agent_template_uid, harness_uid) DO UPDATE SET
    agent_template_name = EXCLUDED.agent_template_name,
    harness_name = EXCLUDED.harness_name,
    desired_revision = EXCLUDED.desired_revision,
    retired_at = NULL,
    updated_at = NOW();

-- name: UpsertRuntimeRevision :exec
INSERT INTO runtime_revision (
    revision, namespace, agent_template_name, agent_template_uid,
    harness_name, harness_uid, source_snapshot, egress_destinations,
    actor_template_namespace, actor_template_name, actor_template_uid,
    phase, golden_snapshot
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8,
    $9, $10, $11, $12, $13
)
ON CONFLICT (revision) DO UPDATE SET
    actor_template_uid = EXCLUDED.actor_template_uid,
    phase = EXCLUDED.phase,
    golden_snapshot = EXCLUDED.golden_snapshot,
    updated_at = NOW();

-- name: MarkRuntimeRevisionSuccessful :exec
UPDATE agent_template_attachment
SET latest_successful_revision = sqlc.arg(revision), updated_at = NOW()
WHERE namespace = sqlc.arg(namespace)
  AND agent_template_uid = sqlc.arg(agent_template_uid)
  AND harness_uid = sqlc.arg(harness_uid)
  AND desired_revision = sqlc.arg(revision)
  AND retired_at IS NULL;

-- name: RetireAgentTemplateAttachments :exec
UPDATE agent_template_attachment
SET retired_at = COALESCE(retired_at, NOW()), updated_at = NOW()
WHERE namespace = $1 AND agent_template_name = $2;

-- name: RetireHarnessAttachment :exec
UPDATE agent_template_attachment
SET retired_at = COALESCE(retired_at, NOW()), updated_at = NOW()
WHERE namespace = $1 AND agent_template_name = $2 AND harness_name = $3;

-- name: RetireOtherHarnessAttachments :exec
UPDATE agent_template_attachment
SET retired_at = COALESCE(retired_at, NOW()), updated_at = NOW()
WHERE namespace = sqlc.arg(namespace)
  AND agent_template_uid = sqlc.arg(agent_template_uid)
  AND NOT (harness_name = ANY(sqlc.arg(harness_names)::text[]));

-- name: GetRuntimeRevision :one
SELECT * FROM runtime_revision WHERE revision = $1;

-- name: ListUnreferencedRuntimeRevisions :many
SELECT * FROM runtime_revision r
WHERE NOT EXISTS (
    SELECT 1 FROM agent_template_attachment a
    WHERE a.retired_at IS NULL
      AND (a.desired_revision = r.revision OR a.latest_successful_revision = r.revision)
);

-- name: DeleteUnreferencedRuntimeRevision :exec
DELETE FROM runtime_revision r
WHERE r.revision = $1
  AND NOT EXISTS (
      SELECT 1 FROM agent_template_attachment a
      WHERE a.retired_at IS NULL
        AND (a.desired_revision = r.revision OR a.latest_successful_revision = r.revision)
  );
