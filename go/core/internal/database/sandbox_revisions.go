package database

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kagent-dev/kagent/go/core/internal/egress"
)

type SandboxRevision struct {
	RuntimeArtifact
	SandboxTemplateName string
	SandboxTemplateUID  string
	SourceSnapshot      json.RawMessage
}

// SandboxTemplateDefinition tracks the desired and last successful runtime for a SandboxTemplate UID.
type SandboxTemplateDefinition struct {
	Grants              egress.Access
	Namespace           string
	SandboxTemplateName string
	SandboxTemplateUID  string
	DesiredRevision     string
}

// UpsertSandboxTemplateDefinition pins desired inputs before any backend mutation. A new
// Kubernetes UID retires the former identity without inheriting its last success.
// Grants are canonicalized before writing. The bool reports changed active grants
// or retirement of another UID; errors may include an uncertain commit outcome.
func (c *Client) UpsertSandboxTemplateDefinition(ctx context.Context, definition SandboxTemplateDefinition) (bool, error) {
	grants, err := canonicalStoredGrants(definition.Grants)
	if err != nil {
		return false, fmt.Errorf("validate source grants: %w", err)
	}
	changed := false
	err = c.withTx(ctx, func(tx pgx.Tx) error {
		retired, err := tx.Exec(ctx, `
			UPDATE sandbox_template_definition SET retired_at = NOW(), updated_at = NOW()
			WHERE namespace = $1 AND sandbox_template_name = $2 AND sandbox_template_uid <> $3 AND retired_at IS NULL
		`, definition.Namespace, definition.SandboxTemplateName, definition.SandboxTemplateUID)
		if err != nil {
			return err
		}
		changed = retired.RowsAffected() > 0
		grantsChanged, err := queryOne(ctx, tx, `
			WITH previous AS MATERIALIZED (
			  SELECT grant_destinations, grant_credentials, retired_at FROM sandbox_template_definition
			  WHERE namespace = $1 AND sandbox_template_uid = $3 FOR UPDATE
			), updated AS (
			  INSERT INTO sandbox_template_definition (namespace, sandbox_template_name, sandbox_template_uid, desired_revision, grant_destinations, grant_credentials)
			  SELECT $1, $2, $3, $4, $5, $6 FROM (SELECT COUNT(*) FROM previous) locked
			  ON CONFLICT (namespace, sandbox_template_uid) DO UPDATE SET
			    sandbox_template_name = EXCLUDED.sandbox_template_name, desired_revision = EXCLUDED.desired_revision,
			    grant_destinations = EXCLUDED.grant_destinations, grant_credentials = EXCLUDED.grant_credentials,
			    retired_at = NULL, updated_at = NOW()
			  RETURNING namespace
			)
			SELECT COALESCE((SELECT grant_destinations FROM previous WHERE retired_at IS NULL), '{}'::text[]) IS DISTINCT FROM $5
			    OR COALESCE((SELECT grant_credentials FROM previous WHERE retired_at IS NULL), '[]'::jsonb) IS DISTINCT FROM $6
			FROM updated
		`, pgx.RowTo[bool], definition.Namespace, definition.SandboxTemplateName, definition.SandboxTemplateUID, definition.DesiredRevision, grants.Destinations, grants.Credentials)
		if err != nil {
			return err
		}

		changed = changed || grantsChanged
		revisions, err := queryMany(ctx, tx, `
			SELECT r.revision, r.kind, r.namespace, r.actor_template_atespace, r.actor_template_name, r.actor_template_uid, r.deleted_at
			FROM runtime_revision r JOIN sandbox_template_definition p
			    ON r.revision IN (p.desired_revision, p.latest_successful_revision)
			WHERE p.namespace = $1 AND p.sandbox_template_uid = $2 ORDER BY r.revision FOR UPDATE OF r
		`, pgx.RowToStructByName[RuntimeArtifact], definition.Namespace, definition.SandboxTemplateUID)
		if err != nil {
			return err
		}
		for _, revision := range revisions {
			if revision.Kind != "sandbox" {
				return fmt.Errorf("SandboxTemplate definition references an agent revision: %w", ErrConflict)
			}
			if revision.DeletedAt != nil {
				return ErrObjectDeleting
			}
		}
		return nil
	})
	return changed, err
}

// RetireSandboxTemplateIdentities retires active identities at this source name.
// Missing/already-retired identities are a no-op; retained actors lose live grants.
func (c *Client) RetireSandboxTemplateIdentities(ctx context.Context, namespace, name string) error {
	return execSQL(ctx, c.db, `
		UPDATE sandbox_template_definition SET retired_at = NOW(), updated_at = NOW()
		WHERE namespace = $1 AND sandbox_template_name = $2 AND retired_at IS NULL
	`, namespace, name)
}

// RecordSandboxRevision stores immutable sandbox inputs and refreshes artifact
// identity without promoting the revision. Deleting revisions return ErrObjectDeleting.
func (c *Client) RecordSandboxRevision(ctx context.Context, revision SandboxRevision) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		revision.Kind = runtimeKindSandbox
		if err := recordRuntimeRevision(ctx, tx, runtimeRevisionRecord{RuntimeArtifact: revision.RuntimeArtifact, SourceSnapshot: revision.SourceSnapshot}); err != nil {
			return err
		}
		if err := execSQL(ctx, tx, `
			INSERT INTO sandbox_revision (revision, sandbox_template_name, sandbox_template_uid)
			VALUES ($1, $2, $3) ON CONFLICT (revision) DO NOTHING
		`, revision.Revision, revision.SandboxTemplateName, revision.SandboxTemplateUID); err != nil {
			return err
		}
		return nil
	})
}

// PromoteSandboxRevision makes a recorded, prepared revision available only to
// the active template identity that still desires it. Missing/deleting revisions return
// ErrNotFound / ErrObjectDeleting; stale reports leave the definition unchanged.
func (c *Client) PromoteSandboxRevision(ctx context.Context, revision string) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		// Match GC finalization's lock order: definition before revision.
		if _, err := queryMany(ctx, tx, `
			SELECT 1 FROM sandbox_template_definition p
			JOIN runtime_revision r ON p.namespace = r.namespace
			JOIN sandbox_revision s ON r.revision = s.revision AND p.sandbox_template_uid = s.sandbox_template_uid
			WHERE r.revision = $1 FOR UPDATE OF p
		`, pgx.RowTo[int], revision); err != nil {
			return err
		}
		if err := lockRuntimeRevisionForReference(ctx, tx, revision, runtimeKindSandbox); err != nil {
			return err
		}
		return execSQL(ctx, tx, `
			UPDATE sandbox_template_definition p SET latest_successful_revision = r.revision, updated_at = NOW()
			FROM runtime_revision r JOIN sandbox_revision s USING (revision)
			WHERE r.revision = $1 AND p.namespace = r.namespace AND p.sandbox_template_uid = s.sandbox_template_uid
			    AND p.desired_revision = r.revision AND p.retired_at IS NULL
			    AND p.latest_successful_revision IS DISTINCT FROM r.revision
		`, revision)
	})
}

// GetSandboxRevision reads immutable sandbox inputs; missing revisions return ErrNotFound.
func (c *Client) GetSandboxRevision(ctx context.Context, revision string) (*SandboxRevision, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT r.revision, r.kind, r.namespace, r.actor_template_atespace, r.actor_template_name, r.actor_template_uid,
		    r.deleted_at, r.source_snapshot, s.sandbox_template_name, s.sandbox_template_uid
		FROM runtime_revision r JOIN sandbox_revision s USING (revision) WHERE r.revision = $1 AND r.kind = 'sandbox'
	`, pgx.RowToStructByName[SandboxRevision], revision)
	if err != nil {
		return nil, fmt.Errorf("get sandbox revision: %w", notFoundOr(err))
	}
	return &row, nil
}
