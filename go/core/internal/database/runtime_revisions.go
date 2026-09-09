package database

import (
	"context"
	"fmt"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
)

// UpsertAgentTemplateHarnessPair records the desired runtime revision for a
// template/harness identity. Updating an existing pair revives it if retired and
// preserves its latest successful revision.
func (c *Client) UpsertAgentTemplateHarnessPair(ctx context.Context, pair AgentTemplateHarnessPair) error {
	return execSQL(ctx, c.db, `
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
	)
}

// RecordRuntimeRevision stores a prepared revision and, when ready, atomically promotes
// it for an active pair that still desires it. Stale readiness reports do not change the
// pair. Existing revisions retain immutable inputs; only the actor-template UID and
// update time are refreshed.
func (c *Client) RecordRuntimeRevision(ctx context.Context, revision RuntimeRevision, ready bool) error {
	if revision.AgentCard == nil {
		return fmt.Errorf("runtime revision %s has no Agent Card", revision.Revision)
	}
	card, err := proto.Marshal(revision.AgentCard)
	if err != nil {
		return fmt.Errorf("encode runtime revision Agent Card: %w", err)
	}
	if err := execSQL(ctx, c.db, `
		WITH stored AS (
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
			RETURNING revision, namespace, agent_template_uid, harness_uid
		)
		UPDATE agent_template_harness_pair p
		SET latest_successful_revision = r.revision, updated_at = NOW()
		FROM stored r
		WHERE $13::boolean
		  AND p.namespace = r.namespace
		  AND p.agent_template_uid = r.agent_template_uid
		  AND p.harness_uid = r.harness_uid
		  AND p.desired_revision = r.revision
		  AND p.retired_at IS NULL
	`,
		revision.Revision, revision.Namespace, revision.AgentTemplateName, revision.AgentTemplateUID,
		revision.HarnessName, revision.HarnessUID, revision.SourceSnapshot, card, revision.EgressDestinations,
		revision.ActorTemplateAtespace, revision.ActorTemplateName, revision.ActorTemplateUID, ready,
	); err != nil {
		return fmt.Errorf("record runtime revision %s: %w", revision.Revision, err)
	}
	return nil
}

// GetRuntimeRevision returns a prepared revision and its decoded agent card, or
// ErrNotFound if absent.
func (c *Client) GetRuntimeRevision(ctx context.Context, revision string) (*RuntimeRevision, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT revision, namespace, agent_template_name, agent_template_uid, harness_name, harness_uid,
		    source_snapshot, egress_destinations, actor_template_atespace, actor_template_name, actor_template_uid,
		    agent_card FROM runtime_revision WHERE revision = $1
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

// RetireAgentTemplateHarnessPair excludes matching namespace/template/harness pairs from
// new instance creation. Existing instances retain their pinned revisions; missing pairs
// are a no-op.
func (c *Client) RetireAgentTemplateHarnessPair(ctx context.Context, namespace, template, harness string) error {
	return execSQL(ctx, c.db, `
		UPDATE agent_template_harness_pair
		SET retired_at = COALESCE(retired_at, NOW()), updated_at = NOW()
		WHERE namespace = $1 AND agent_template_name = $2 AND harness_name = $3
	`, namespace, template, harness)
}

// ListUnreferencedRuntimeRevisions lists revisions unused by instances or active pairs'
// desired/latest-successful pointers. Checkpoint references are not excluded and can still
// prevent deletion.
func (c *Client) ListUnreferencedRuntimeRevisions(ctx context.Context) ([]RuntimeRevision, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT revision, namespace, agent_template_name, agent_template_uid, harness_name, harness_uid,
		    source_snapshot, egress_destinations, actor_template_atespace, actor_template_name, actor_template_uid,
		    agent_card FROM runtime_revision r
		WHERE NOT EXISTS (
		    SELECT 1 FROM agent_template_harness_pair p
		    WHERE p.retired_at IS NULL
		      AND (p.desired_revision = r.revision OR p.latest_successful_revision = r.revision)
		)
		AND NOT EXISTS (
		    SELECT 1 FROM agent_instance i WHERE i.prepared_revision = r.revision
		)
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

// DeleteUnreferencedRuntimeRevision deletes a revision only if no instance or active pair
// uses it. Missing or still-used revisions are a no-op; checkpoint foreign keys can also
// reject deletion.
func (c *Client) DeleteUnreferencedRuntimeRevision(ctx context.Context, revision string) error {
	return execSQL(ctx, c.db, `
		DELETE FROM runtime_revision r
		WHERE r.revision = $1
		  AND NOT EXISTS (
		      SELECT 1 FROM agent_template_harness_pair p
		      WHERE p.retired_at IS NULL
		        AND (p.desired_revision = r.revision OR p.latest_successful_revision = r.revision)
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM agent_instance i WHERE i.prepared_revision = r.revision
		  )
	`, revision)
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
}
