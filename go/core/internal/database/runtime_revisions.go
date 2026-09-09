package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
)

// UpsertAgentTemplateHarnessPair records the desired runtime revision for a
// template/harness identity. Updating an existing pair revives it if retired and
// preserves its latest successful revision. It atomically retires older identities
// at the same names and rejects revisions whose deletion has started.
func (c *Client) UpsertAgentTemplateHarnessPair(ctx context.Context, pair AgentTemplateHarnessPair) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		if err := retirePairIdentitiesExcept(ctx, tx, pair); err != nil {
			return fmt.Errorf("retire replaced AgentTemplate/Harness pair: %w", err)
		}
		if err := execSQL(ctx, tx, `
			INSERT INTO agent_template_harness_pair (
			    namespace, agent_template_name, agent_template_uid,
			    harness_name, harness_uid, desired_revision, retired_at
			) VALUES ($1, $2, $3, $4, $5, $6, NULL)
			ON CONFLICT (namespace, agent_template_uid, harness_uid) DO UPDATE SET
			    agent_template_name = EXCLUDED.agent_template_name,
			    harness_name = EXCLUDED.harness_name,
			    desired_revision = EXCLUDED.desired_revision,
			    retired_at = NULL,
			    updated_at = NOW()
		`,
			pair.Namespace, pair.AgentTemplateName, pair.AgentTemplateUID, pair.HarnessName, pair.HarnessUID,
			pair.DesiredRevision,
		); err != nil {
			return err
		}
		// Lock pairs before revisions. Validate the resulting pair, including
		// its retained last-good pointer when reactivating a retired identity.
		deletedAt, err := queryMany(ctx, tx, `
			SELECT r.deleted_at FROM runtime_revision r
			JOIN agent_template_harness_pair p
			  ON r.revision IN (p.desired_revision, p.latest_successful_revision)
			WHERE p.namespace = $1 AND p.agent_template_uid = $2 AND p.harness_uid = $3
			ORDER BY r.revision
			FOR UPDATE OF r
		`, pgx.RowTo[*time.Time], pair.Namespace, pair.AgentTemplateUID, pair.HarnessUID)
		if err != nil {
			return err
		}
		for _, timestamp := range deletedAt {
			if timestamp != nil {
				return ErrObjectDeleting
			}
		}
		return nil
	})
}

// UpsertRuntimeRevision stores a prepared revision's configuration and agent card. An
// existing revision retains those immutable inputs; only its actor-template UID and update
// time are refreshed. Revisions whose deletion has started return ErrObjectDeleting.
func (c *Client) UpsertRuntimeRevision(ctx context.Context, revision RuntimeRevision) error {
	if revision.AgentCard == nil {
		return fmt.Errorf("runtime revision %s has no Agent Card", revision.Revision)
	}
	card, err := proto.Marshal(revision.AgentCard)
	if err != nil {
		return fmt.Errorf("encode runtime revision Agent Card: %w", err)
	}
	result, err := c.db.Exec(ctx, `
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
		WHERE runtime_revision.deleted_at IS NULL
	`,
		revision.Revision, revision.Namespace, revision.AgentTemplateName, revision.AgentTemplateUID,
		revision.HarnessName, revision.HarnessUID, revision.SourceSnapshot, card, revision.EgressDestinations,
		revision.ActorTemplateAtespace, revision.ActorTemplateName, revision.ActorTemplateUID,
	)
	if err != nil {
		return fmt.Errorf("upsert runtime revision %s: %w", revision.Revision, err)
	}
	if result.RowsAffected() == 0 {
		return ErrObjectDeleting
	}
	return nil
}

// GetRuntimeRevision returns a prepared revision and its decoded agent card, or
// ErrNotFound if absent.
func (c *Client) GetRuntimeRevision(ctx context.Context, revision string) (*RuntimeRevision, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT revision, namespace, agent_template_name, agent_template_uid, harness_name, harness_uid,
		    source_snapshot, egress_destinations, actor_template_atespace, actor_template_name, actor_template_uid,
		    agent_card, deleted_at FROM runtime_revision WHERE revision = $1
	`, pgx.RowToStructByName[runtimeRevisionRow], revision)
	if err != nil {
		return nil, fmt.Errorf("get runtime revision %s: %w", revision, notFoundOr(err))
	}
	return toRuntimeRevision(row)
}

// toRuntimeRevision converts a prepared revision and decodes its agent card, returning an
// error for malformed protobuf data.
func toRuntimeRevision(row runtimeRevisionRow) (*RuntimeRevision, error) {
	card := &a2apb.AgentCard{}
	if err := proto.Unmarshal(row.AgentCard, card); err != nil {
		return nil, fmt.Errorf("decode runtime revision %s Agent Card: %w", row.Revision, err)
	}
	return &RuntimeRevision{
		Revision: row.Revision, Namespace: row.Namespace,
		AgentTemplateName: row.AgentTemplateName, AgentTemplateUID: row.AgentTemplateUID,
		HarnessName: row.HarnessName, HarnessUID: row.HarnessUID,
		SourceSnapshot: row.SourceSnapshot, AgentCard: card,
		EgressDestinations:    row.EgressDestinations,
		ActorTemplateAtespace: row.ActorTemplateAtespace, ActorTemplateName: row.ActorTemplateName,
		ActorTemplateUID: row.ActorTemplateUID,
	}, nil
}

// ListActorTemplateHarnesses returns actor-template and harness identities from all stored
// revisions. Results can contain duplicates and have no guaranteed order.
func (c *Client) ListActorTemplateHarnesses(ctx context.Context) ([]ActorTemplateHarness, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT actor_template_atespace AS atespace, actor_template_name AS name, actor_template_uid AS uid, harness_name
		FROM runtime_revision
	`, pgx.RowToStructByName[ActorTemplateHarness])
	if err != nil {
		return nil, fmt.Errorf("list ActorTemplate harnesses: %w", err)
	}
	return rows, nil
}

// MarkRuntimeRevisionSuccessful promotes a revision only if its pair is still active and
// still desires that revision. Missing pairs are a no-op; deleting revisions are
// rejected. Pair and revision locks serialize promotion with GC finalization.
func (c *Client) MarkRuntimeRevisionSuccessful(ctx context.Context, pair AgentTemplateHarnessPair) error {
	revision := pair.DesiredRevision
	return c.withTx(ctx, func(tx pgx.Tx) error {
		_, err := queryOne(ctx, tx, `
			SELECT 1 FROM agent_template_harness_pair
			WHERE namespace = $1 AND agent_template_uid = $2 AND harness_uid = $3
			FOR UPDATE
		`, pgx.RowTo[int], pair.Namespace, pair.AgentTemplateUID, pair.HarnessUID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := getAvailableRuntimeRevisionForUpdate(ctx, tx, revision); err != nil {
			return err
		}
		return execSQL(ctx, tx, `
			UPDATE agent_template_harness_pair
			SET latest_successful_revision = $1, updated_at = NOW()
			WHERE namespace = $2
			  AND agent_template_uid = $3
			  AND harness_uid = $4
			  AND desired_revision = $1
			  AND retired_at IS NULL
		`, revision, pair.Namespace, pair.AgentTemplateUID, pair.HarnessUID)
	})
}

// RetireAllPairIdentities excludes matching namespace/template/harness pairs from
// new instance creation. Existing instances retain their pinned revisions; missing pairs
// are a no-op.
func (c *Client) RetireAllPairIdentities(ctx context.Context, namespace, template, harness string) error {
	return execSQL(ctx, c.db, `
		UPDATE agent_template_harness_pair
		SET retired_at = COALESCE(retired_at, NOW()), updated_at = NOW()
		WHERE namespace = $1 AND agent_template_name = $2 AND harness_name = $3
	`, namespace, template, harness)
}

// RetirePairIdentitiesExcept retires older identities at keep's template/harness
// names, preserving the current UID pair and its last-good revision. Missing or
// already-retired identities are a no-op.
func (c *Client) RetirePairIdentitiesExcept(ctx context.Context, keep AgentTemplateHarnessPair) error {
	return retirePairIdentitiesExcept(ctx, c.db, keep)
}

// retirePairIdentitiesExcept uses the caller's executor so retirement can commit
// atomically with pair preparation. It never retires the supplied UID pair.
func retirePairIdentitiesExcept(ctx context.Context, db dbExecutor, keep AgentTemplateHarnessPair) error {
	return execSQL(ctx, db, `
		UPDATE agent_template_harness_pair
		SET retired_at = NOW(), updated_at = NOW()
		WHERE namespace = $1 AND agent_template_name = $2 AND harness_name = $3
		  AND retired_at IS NULL
		  AND (agent_template_uid, harness_uid) IS DISTINCT FROM ($4::text, $5::text)
	`, keep.Namespace, keep.AgentTemplateName, keep.HarnessName, keep.AgentTemplateUID, keep.HarnessUID)
}

// RetireOtherAgentTemplateHarnessPairs retires a template's pairs whose harness names are
// absent from harnesses. An empty non-nil slice retires every pair for that template;
// a nil slice matches no pairs. Existing instances retain their pinned revisions.
func (c *Client) RetireOtherAgentTemplateHarnessPairs(ctx context.Context, namespace, templateUID string, harnesses []string) error {
	return execSQL(ctx, c.db, `
		UPDATE agent_template_harness_pair
		SET retired_at = COALESCE(retired_at, NOW()), updated_at = NOW()
		WHERE namespace = $1
		  AND agent_template_uid = $2
		  AND NOT (harness_name = ANY($3::text[]))
	`, namespace, templateUID, harnesses)
}

// ListUnreferencedRuntimeRevisions lists revisions unused by active pairs,
// instances, or checkpoints, including deletions still awaiting compute cleanup.
func (c *Client) ListUnreferencedRuntimeRevisions(ctx context.Context) ([]RuntimeRevision, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT revision, namespace, agent_template_name, agent_template_uid, harness_name, harness_uid,
		    source_snapshot, egress_destinations, actor_template_atespace, actor_template_name, actor_template_uid,
		    agent_card, deleted_at FROM runtime_revision r
		WHERE r.revision IN (SELECT revision FROM unreferenced_runtime_revision)
	`, pgx.RowToStructByName[runtimeRevisionRow])
	if err != nil {
		return nil, fmt.Errorf("list unreferenced runtime revisions: %w", err)
	}
	result := make([]RuntimeRevision, 0, len(rows))
	for _, row := range rows {
		revision, err := toRuntimeRevision(row)
		if err != nil {
			return nil, err
		}
		result = append(result, *revision)
	}
	return result, nil
}

// getRuntimeRevisionForUpdate locks a revision until the supplied transaction ends.
// Missing revisions return pgx.ErrNoRows. Check eligibility in a separate statement
// after this lock so references committed during a lock wait are visible.
func getRuntimeRevisionForUpdate(ctx context.Context, tx pgx.Tx, revision string) (runtimeRevisionRow, error) {
	return queryOne(ctx, tx, `
		SELECT revision, namespace, agent_template_name, agent_template_uid, harness_name, harness_uid,
		    source_snapshot, egress_destinations, actor_template_atespace, actor_template_name, actor_template_uid,
		    agent_card, deleted_at FROM runtime_revision WHERE revision = $1 FOR UPDATE
	`, pgx.RowToStructByName[runtimeRevisionRow], revision)
}

// getAvailableRuntimeRevisionForUpdate locks a revision for reference acquisition.
// The caller must commit the reference in this transaction. Missing revisions return
// ErrNotFound; logically deleted revisions return ErrObjectDeleting.
func getAvailableRuntimeRevisionForUpdate(ctx context.Context, tx pgx.Tx, revision string) (runtimeRevisionRow, error) {
	row, err := getRuntimeRevisionForUpdate(ctx, tx, revision)
	if err != nil {
		return row, notFoundOr(err)
	}
	if row.DeletedAt != nil {
		return row, ErrObjectDeleting
	}
	return row, nil
}

// BeginRuntimeRevisionDeletion marks an unreferenced revision as deleting,
// preventing new references before runtime cleanup. Retrying returns the pending
// revision; nil means the revision is missing or still referenced.
func (c *Client) BeginRuntimeRevisionDeletion(ctx context.Context, revision string) (*RuntimeRevision, error) {
	var result *RuntimeRevision
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := getRuntimeRevisionForUpdate(ctx, tx, revision)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		updated, err := tx.Exec(ctx, `
			UPDATE runtime_revision
			SET deleted_at = COALESCE(deleted_at, NOW())
			WHERE revision = $1 AND revision IN (SELECT revision FROM unreferenced_runtime_revision)
		`, revision)
		if err != nil || updated.RowsAffected() == 0 {
			return err
		}
		result, err = toRuntimeRevision(row)
		return err
	})
	return result, err
}

// DeleteRuntimeRevision atomically releases retired success pointers and removes
// a revision after compute cleanup. Missing, unmarked, or changed-UID revisions
// are a no-op so retries cannot finalize a replacement runtime.
func (c *Client) DeleteRuntimeRevision(ctx context.Context, revision, actorTemplateUID string) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		// Match pair preparation's lock order: pairs before revisions.
		if _, err := queryMany(ctx, tx, `
			SELECT 1 FROM agent_template_harness_pair
			WHERE retired_at IS NOT NULL AND latest_successful_revision = $1
			ORDER BY namespace, agent_template_uid, harness_uid
			FOR UPDATE
		`, pgx.RowTo[int], revision); err != nil {
			return err
		}
		row, err := getRuntimeRevisionForUpdate(ctx, tx, revision)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if row.DeletedAt == nil || row.ActorTemplateUID != actorTemplateUID {
			return nil
		}
		if err := execSQL(ctx, tx, `
			UPDATE agent_template_harness_pair
			SET latest_successful_revision = NULL, updated_at = NOW()
			WHERE retired_at IS NOT NULL AND latest_successful_revision = $1
		`, revision); err != nil {
			return fmt.Errorf("release retired runtime revision references: %w", err)
		}
		return execSQL(ctx, tx, `
			DELETE FROM runtime_revision WHERE revision = $1 AND deleted_at IS NOT NULL
		`, revision)
	})
}

type runtimeRevisionRow struct {
	Revision              string
	Namespace             string
	AgentTemplateName     string
	AgentTemplateUID      string
	HarnessName           string
	HarnessUID            string
	SourceSnapshot        []byte
	EgressDestinations    []string
	ActorTemplateAtespace string
	ActorTemplateName     string
	ActorTemplateUID      string
	AgentCard             []byte
	DeletedAt             *time.Time
}
