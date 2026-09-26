package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/jackc/pgx/v5"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"google.golang.org/protobuf/proto"
)

// UpsertAgentDefinition records the desired runtime revision for a
// Agent identity. Updating an existing definition revives it if retired and
// preserves its latest successful revision. It atomically retires older identities
// at the same names and rejects revisions whose deletion has started.
func (c *Client) UpsertAgentDefinition(ctx context.Context, definition AgentDefinition) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		if err := retireAgentIdentities(ctx, tx, definition.Namespace, definition.AgentName, &definition); err != nil {
			return fmt.Errorf("retire replaced Agent: %w", err)
		}
		if err := execSQL(ctx, tx, `
			INSERT INTO agent_definition (
			    namespace, agent_name, agent_uid, desired_revision, retired_at
			) VALUES ($1, $2, $3, $4, NULL)
			ON CONFLICT (namespace, agent_uid) DO UPDATE SET
			    agent_name = EXCLUDED.agent_name,
			    desired_revision = EXCLUDED.desired_revision,
			    retired_at = NULL,
			    updated_at = NOW()
		`,
			definition.Namespace, definition.AgentName, definition.AgentUID,
			definition.DesiredRevision,
		); err != nil {
			return err
		}
		// Lock Agents before revisions. Validate the resulting definition, including
		// its retained last-good pointer when reactivating a retired identity.
		deletedAt, err := queryMany(ctx, tx, `
			SELECT r.deleted_at FROM runtime_revision r
			JOIN agent_definition p
			  ON r.revision IN (p.desired_revision, p.latest_successful_revision)
			WHERE p.namespace = $1 AND p.agent_uid = $2
			ORDER BY r.revision
			FOR UPDATE OF r
		`, pgx.RowTo[*time.Time], definition.Namespace, definition.AgentUID)
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

// RecordRuntimeRevision stores a prepared revision and, when ready, atomically
// promotes it for an active definition that still desires it. Stale reports leave the
// definition unchanged. Existing revisions retain immutable inputs; only their actor
// UID and update time are refreshed. Deleting revisions return ErrObjectDeleting.
func (c *Client) RecordRuntimeRevision(ctx context.Context, revision RuntimeRevision, ready bool) error {
	if revision.AgentCard == nil {
		return fmt.Errorf("runtime revision %s has no Agent Card", revision.Revision)
	}
	card, err := proto.Marshal(revision.AgentCard)
	if err != nil {
		return fmt.Errorf("encode runtime revision Agent Card: %w", err)
	}
	credentials := revision.Credentials
	if credentials == nil {
		credentials = []egress.Credential{}
	}
	return c.withTx(ctx, func(tx pgx.Tx) error {
		if ready {
			// Match GC finalization's lock order: definition before revision. Use the
			// stored identity when present, since revision inputs are immutable.
			_, err := queryOne(ctx, tx, `
				SELECT 1 FROM agent_definition p
				LEFT JOIN runtime_revision r ON r.revision = $1
				WHERE p.namespace = COALESCE(r.namespace, $2)
				  AND p.agent_uid = COALESCE(r.agent_uid, $3)
				FOR UPDATE OF p
			`, pgx.RowTo[int], revision.Revision, revision.Namespace, revision.AgentUID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		result, err := tx.Exec(ctx, `
			INSERT INTO runtime_revision (
			    revision, namespace, agent_name, agent_uid, source_snapshot, agent_card, egress_destinations,
			    actor_template_atespace, actor_template_name, actor_template_uid, credentials
			) VALUES (
			    $1, $2, $3, $4, $5, $6, $7, $8,
			    $9, $10, $11
			)
			ON CONFLICT (revision) DO UPDATE SET
			    actor_template_uid = EXCLUDED.actor_template_uid,
			    updated_at = NOW()
			WHERE runtime_revision.deleted_at IS NULL
		`,
			revision.Revision, revision.Namespace, revision.AgentName, revision.AgentUID,
			revision.SourceSnapshot, card, revision.EgressDestinations,
			revision.ActorTemplateAtespace, revision.ActorTemplateName, revision.ActorTemplateUID, credentials,
		)
		if err != nil {
			return fmt.Errorf("record runtime revision %s: %w", revision.Revision, err)
		}
		if result.RowsAffected() == 0 {
			return ErrObjectDeleting
		}
		if !ready {
			return nil
		}
		return execSQL(ctx, tx, `
			UPDATE agent_definition p
			SET latest_successful_revision = r.revision, updated_at = NOW()
			FROM runtime_revision r
			WHERE r.revision = $1
			  AND p.namespace = r.namespace
			  AND p.agent_uid = r.agent_uid
			  AND p.desired_revision = r.revision
			  AND p.retired_at IS NULL
		`, revision.Revision)
	})
}

// GetRuntimeRevision returns a prepared revision and its decoded agent card, or
// ErrNotFound if absent.
func (c *Client) GetRuntimeRevision(ctx context.Context, revision string) (*RuntimeRevision, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT revision, namespace, agent_name, agent_uid,
		    source_snapshot, egress_destinations, credentials, actor_template_atespace, actor_template_name, actor_template_uid,
		    agent_card, deleted_at FROM runtime_revision WHERE revision = $1
	`, pgx.RowToStructByName[runtimeRevisionRow], revision)
	if err != nil {
		return nil, fmt.Errorf("get runtime revision %s: %w", revision, notFoundOr(err))
	}
	return toRuntimeRevision(row)
}

// toRuntimeRevision decodes the agent card and canonicalizes credential bindings,
// returning an error for malformed stored data.
func toRuntimeRevision(row runtimeRevisionRow) (*RuntimeRevision, error) {
	card := &a2apb.AgentCard{}
	if err := proto.Unmarshal(row.AgentCard, card); err != nil {
		return nil, fmt.Errorf("decode runtime revision %s Agent Card: %w", row.Revision, err)
	}
	credentials, err := egress.CanonicalCredentials(row.Credentials)
	if err != nil {
		return nil, fmt.Errorf("decode runtime revision %s credentials: %w", row.Revision, err)
	}
	return &RuntimeRevision{
		Revision: row.Revision, Namespace: row.Namespace,
		AgentName: row.AgentName, AgentUID: row.AgentUID,
		SourceSnapshot: row.SourceSnapshot, AgentCard: card,
		EgressDestinations:    row.EgressDestinations,
		Credentials:           credentials,
		ActorTemplateAtespace: row.ActorTemplateAtespace, ActorTemplateName: row.ActorTemplateName,
		ActorTemplateUID: row.ActorTemplateUID,
	}, nil
}

// RetireAgentIdentities retires identities at the given namespace/Agent
// names, except the supplied UID when non-nil. Existing sessions retain
// their pinned revisions. Missing and already-retired identities are a no-op.
func (c *Client) RetireAgentIdentities(ctx context.Context, namespace, name string, except *AgentDefinition) error {
	return retireAgentIdentities(ctx, c.db, namespace, name, except)
}

// retireAgentIdentities uses the caller's executor so retirement can commit
// atomically with definition preparation. A nil exception retires every matching identity.
func retireAgentIdentities(ctx context.Context, db dbExecutor, namespace, name string, except *AgentDefinition) error {
	var uid *string
	if except != nil {
		uid = &except.AgentUID
	}
	return execSQL(ctx, db, `
		UPDATE agent_definition
		SET retired_at = NOW(), updated_at = NOW()
		WHERE namespace = $1 AND agent_name = $2
		  AND retired_at IS NULL AND agent_uid IS DISTINCT FROM $3::text
	`, namespace, name, uid)
}

// ListUnreferencedRuntimeRevisions lists revisions unused by active Agents,
// sessions, or checkpoints, including deletions still awaiting compute cleanup.
func (c *Client) ListUnreferencedRuntimeRevisions(ctx context.Context) ([]RuntimeRevision, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT revision, namespace, agent_name, agent_uid,
		    source_snapshot, egress_destinations, credentials, actor_template_atespace, actor_template_name, actor_template_uid,
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
		SELECT revision, namespace, agent_name, agent_uid,
		    source_snapshot, egress_destinations, credentials, actor_template_atespace, actor_template_name, actor_template_uid,
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
		// Match definition preparation's lock order: Agents before revisions.
		if _, err := queryMany(ctx, tx, `
			SELECT 1 FROM agent_definition
			WHERE retired_at IS NOT NULL AND latest_successful_revision = $1
			ORDER BY namespace, agent_uid
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
			UPDATE agent_definition
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
	AgentName             string
	AgentUID              string
	SourceSnapshot        []byte
	EgressDestinations    []string
	Credentials           []egress.Credential
	ActorTemplateAtespace string
	ActorTemplateName     string
	ActorTemplateUID      string
	AgentCard             []byte
	DeletedAt             *time.Time
}
