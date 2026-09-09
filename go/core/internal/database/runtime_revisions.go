package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
)

func (c *Client) UpsertAgentTemplateHarnessPair(ctx context.Context, pair AgentTemplateHarnessPair) error {
	if pair.AgentTemplateLabels == nil {
		pair.AgentTemplateLabels = map[string]string{}
	}
	labels, err := json.Marshal(pair.AgentTemplateLabels)
	if err != nil {
		return fmt.Errorf("marshal AgentTemplate labels: %w", err)
	}
	return execSQL(ctx, c.db, `
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
		    updated_at = NOW()
	`,
		pair.Namespace, pair.AgentTemplateName, pair.AgentTemplateUID, pair.HarnessName, pair.HarnessUID,
		pair.DesiredRevision, labels,
	)
}

// UpsertRuntimeRevision refreshes runtime identity; the revision digest pins the card.
func (c *Client) UpsertRuntimeRevision(ctx context.Context, revision RuntimeRevision) error {
	if revision.AgentCard == nil {
		return fmt.Errorf("runtime revision %s has no Agent Card", revision.Revision)
	}
	card, err := proto.Marshal(revision.AgentCard)
	if err != nil {
		return fmt.Errorf("encode runtime revision Agent Card: %w", err)
	}
	if err := execSQL(ctx, c.db, `
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
	`,
		revision.Revision, revision.Namespace, revision.AgentTemplateName, revision.AgentTemplateUID,
		revision.HarnessName, revision.HarnessUID, revision.SourceSnapshot, card, revision.EgressDestinations,
		revision.ActorTemplateAtespace, revision.ActorTemplateName, revision.ActorTemplateUID,
	); err != nil {
		return fmt.Errorf("upsert runtime revision %s: %w", revision.Revision, err)
	}
	return nil
}

func (c *Client) GetRuntimeRevision(ctx context.Context, revision string) (*RuntimeRevision, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT revision, namespace, agent_template_name, agent_template_uid, harness_name, harness_uid,
		    source_snapshot, egress_destinations, actor_template_atespace, actor_template_name, actor_template_uid,
		    created_at, updated_at, agent_card FROM runtime_revision WHERE revision = $1
	`, pgx.RowToStructByName[runtimeRevisionRow], revision)
	if err != nil {
		return nil, fmt.Errorf("get runtime revision %s: %w", revision, notFoundOr(err))
	}
	return toRuntimeRevision(row)
}

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

func (c *Client) ListActorTemplateHarnesses(ctx context.Context) ([]ActorTemplateHarness, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT actor_template_atespace, actor_template_name, actor_template_uid, harness_name
		FROM runtime_revision
	`, pgx.RowToStructByName[actorTemplateHarnessRow])
	if err != nil {
		return nil, fmt.Errorf("list ActorTemplate harnesses: %w", err)
	}
	result := make([]ActorTemplateHarness, 0, len(rows))
	for _, row := range rows {
		result = append(result, ActorTemplateHarness{
			Atespace: row.ActorTemplateAtespace, Name: row.ActorTemplateName,
			UID: row.ActorTemplateUID, HarnessName: row.HarnessName,
		})
	}
	return result, nil
}

func (c *Client) MarkRuntimeRevisionSuccessful(ctx context.Context, pair AgentTemplateHarnessPair) error {
	revision := pair.DesiredRevision
	return execSQL(ctx, c.db, `
		UPDATE agent_template_harness_pair
		SET latest_successful_revision = $1, updated_at = NOW()
		WHERE namespace = $2
		  AND agent_template_uid = $3
		  AND harness_uid = $4
		  AND desired_revision = $1
		  AND retired_at IS NULL
	`, &revision, pair.Namespace, pair.AgentTemplateUID, pair.HarnessUID)
}

func (c *Client) RetireAgentTemplateHarnessPair(ctx context.Context, namespace, template, harness string) error {
	return execSQL(ctx, c.db, `
		UPDATE agent_template_harness_pair
		SET retired_at = COALESCE(retired_at, NOW()), updated_at = NOW()
		WHERE namespace = $1 AND agent_template_name = $2 AND harness_name = $3
	`, namespace, template, harness)
}

func (c *Client) RetireOtherAgentTemplateHarnessPairs(ctx context.Context, namespace, templateUID string, harnesses []string) error {
	return execSQL(ctx, c.db, `
		UPDATE agent_template_harness_pair
		SET retired_at = COALESCE(retired_at, NOW()), updated_at = NOW()
		WHERE namespace = $1
		  AND agent_template_uid = $2
		  AND NOT (harness_name = ANY($3::text[]))
	`, namespace, templateUID, harnesses)
}

func (c *Client) ListUnreferencedRuntimeRevisions(ctx context.Context) ([]RuntimeRevision, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT revision, namespace, agent_template_name, agent_template_uid, harness_name, harness_uid,
		    source_snapshot, egress_destinations, actor_template_atespace, actor_template_name, actor_template_uid,
		    created_at, updated_at, agent_card FROM runtime_revision r
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

type instanceRuntimeRevisionRow struct {
	runtimeRevisionRow
	AgentTemplateLabels []byte
	DBTime              time.Time
}

type actorTemplateHarnessRow struct {
	ActorTemplateAtespace string
	ActorTemplateName     string
	ActorTemplateUID      string
	HarnessName           string
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
	CreatedAt             time.Time
	UpdatedAt             time.Time
	AgentCard             []byte
}
