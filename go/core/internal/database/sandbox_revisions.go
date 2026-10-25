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
	Namespace           string
	SandboxTemplateName string
	SandboxTemplateUID  string
	EgressGrant         egress.Access
	DesiredRevision     string
}

// UpsertSandboxTemplateDefinition pins desired inputs before any backend mutation. A new
// Kubernetes UID retires the former identity without inheriting its last success.
// Grants are canonicalized before writing; errors may include an uncertain commit outcome.
func (c *Client) UpsertSandboxTemplateDefinition(ctx context.Context, definition SandboxTemplateDefinition) error {
	grants, err := canonicalStoredGrants(definition.EgressGrant)
	if err != nil {
		return fmt.Errorf("validate source grants: %w", err)
	}
	return c.withTx(ctx, func(tx pgx.Tx) error {
		if err := retireSandboxTemplateIdentities(ctx, tx, definition.Namespace, definition.SandboxTemplateName, definition.SandboxTemplateUID); err != nil {
			return err
		}
		if err := execSQL(ctx, tx, `
			INSERT INTO sandbox_template_definition (namespace, sandbox_template_name, sandbox_template_uid, desired_revision, grant_destinations, grant_credentials, grant_token_passthrough_targets)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (namespace, sandbox_template_uid) DO UPDATE SET
			    sandbox_template_name = EXCLUDED.sandbox_template_name, desired_revision = EXCLUDED.desired_revision,
			    grant_destinations = EXCLUDED.grant_destinations, grant_credentials = EXCLUDED.grant_credentials,
			    grant_token_passthrough_targets = EXCLUDED.grant_token_passthrough_targets,
			    retired_at = NULL, updated_at = NOW()
		`, definition.Namespace, definition.SandboxTemplateName, definition.SandboxTemplateUID, definition.DesiredRevision, grants.Destinations, grants.Credentials, grants.TokenPassthroughTargets); err != nil {
			return err
		}

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
}

// GetSandboxEgressGrant returns the active identity's last admitted grants.
// Missing or retired identities return empty access; malformed stored grants fail.
func (c *Client) GetSandboxEgressGrant(ctx context.Context, namespace, uid string) (egress.Access, error) {
	grants, err := queryOne(ctx, c.db, `
		SELECT grant_destinations AS destinations, grant_credentials AS credentials, grant_token_passthrough_targets AS token_passthrough_targets
		FROM sandbox_template_definition WHERE namespace = $1 AND sandbox_template_uid = $2 AND retired_at IS NULL
	`, pgx.RowToStructByName[egress.Access], namespace, uid)
	return decodeEgressGrant(grants, err)
}

// RetireSandboxTemplateIdentities retires active identities at this source name.
// Missing/already-retired identities are a no-op; retained actors lose live grants.
func (c *Client) RetireSandboxTemplateIdentities(ctx context.Context, namespace, name string) error {
	return retireSandboxTemplateIdentities(ctx, c.db, namespace, name, "")
}

// retireSandboxTemplateIdentities shares retirement with definition preparation
// in the caller's transaction. An empty retainedUID retires all active identities.
func retireSandboxTemplateIdentities(ctx context.Context, db dbExecutor, namespace, name, retainedUID string) error {
	return execSQL(ctx, db, `
		UPDATE sandbox_template_definition SET retired_at = NOW(), updated_at = NOW()
		WHERE namespace = $1 AND sandbox_template_name = $2 AND retired_at IS NULL
		  AND sandbox_template_uid IS DISTINCT FROM NULLIF($3, '')
	`, namespace, name, retainedUID)
}

// RecordSandboxRevision atomically stores immutable inputs and, when Ready,
// promotes the revision for its active source if still desired. Existing inputs
// are preserved; actor UID and update time refresh. Stale reports cannot replace
// the last successful revision. Deleting revisions return ErrObjectDeleting.
func (c *Client) RecordSandboxRevision(ctx context.Context, revision SandboxRevision, options RecordRevisionOptions) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		if options.Ready {
			// Match definition preparation and GC: source before revision.
			if _, err := queryMany(ctx, tx, `
				SELECT 1 FROM sandbox_template_definition p
				WHERE p.namespace = $1 AND p.sandbox_template_uid = $2 FOR UPDATE OF p
			`, pgx.RowTo[int], revision.Namespace, revision.SandboxTemplateUID); err != nil {
				return err
			}
		}
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
		if !options.Ready {
			return nil
		}
		return execSQL(ctx, tx, `
			UPDATE sandbox_template_definition p SET latest_successful_revision = r.revision, updated_at = NOW()
			FROM runtime_revision r JOIN sandbox_revision s USING (revision)
			WHERE r.revision = $1 AND p.namespace = $2 AND p.sandbox_template_uid = $3
			    AND p.namespace = r.namespace AND p.sandbox_template_uid = s.sandbox_template_uid
			    AND p.desired_revision = r.revision AND p.retired_at IS NULL
			    AND p.latest_successful_revision IS DISTINCT FROM r.revision
		`, revision.Revision, revision.Namespace, revision.SandboxTemplateUID)
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
