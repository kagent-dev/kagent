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

-- The revision digest pins the card. Reconciliation only refreshes runtime identity.
-- name: UpsertRuntimeRevision :execrows
INSERT INTO runtime_revision (
    revision, namespace, agent_template_name, agent_template_uid,
    harness_name, harness_uid, source_snapshot, agent_card, egress_destinations,
    actor_template_atespace, actor_template_name, actor_template_uid
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8,
    $9, $10, $11, $12
)
ON CONFLICT (revision) DO UPDATE SET
    actor_template_uid = EXCLUDED.actor_template_uid,
    updated_at = NOW()
WHERE runtime_revision.deletion_started_at IS NULL;

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

-- name: RetireReplacedAgentTemplateHarnessPairs :exec
UPDATE agent_template_harness_pair
SET retired_at = NOW(), updated_at = NOW()
WHERE namespace = $1 AND agent_template_name = $2 AND harness_name = $3
  AND retired_at IS NULL
  AND (agent_template_uid, harness_uid) IS DISTINCT FROM (sqlc.arg(agent_template_uid)::text, sqlc.arg(harness_uid)::text);

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

-- name: ListActorTemplateHarnesses :many
SELECT actor_template_atespace, actor_template_name, actor_template_uid, harness_name
FROM runtime_revision;

-- name: ListUnreferencedRuntimeRevisions :many
SELECT * FROM runtime_revision r
WHERE r.revision IN (SELECT revision FROM unreferenced_runtime_revision);

-- The store locks first, then checks eligibility in a separate statement so
-- references committed while waiting for the lock are visible to the claim.
-- name: LockRuntimeRevision :one
SELECT * FROM runtime_revision WHERE revision = $1 FOR UPDATE;

-- name: ClaimRuntimeRevisionDeletion :execrows
UPDATE runtime_revision
SET deletion_started_at = COALESCE(deletion_started_at, NOW())
WHERE runtime_revision.revision = $1
  AND runtime_revision.revision IN (SELECT revision FROM unreferenced_runtime_revision);

-- Retired pairs no longer retain runtime inputs. Release their historical
-- success pointers in the same transaction as deletion, preserving RESTRICT
-- protection for active pairs, instances, and checkpoints.
-- name: ReleaseRetiredRuntimeRevisionReferences :exec
UPDATE agent_template_harness_pair
SET latest_successful_revision = NULL, updated_at = NOW()
WHERE retired_at IS NOT NULL AND latest_successful_revision = $1;

-- name: DeleteUnreferencedRuntimeRevision :exec
DELETE FROM runtime_revision r
WHERE r.revision = $1
  AND r.deletion_started_at IS NOT NULL;
