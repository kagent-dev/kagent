package database

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
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
	DesiredRevision     string
}

// UpsertSandboxTemplateDefinition pins desired inputs before any backend mutation. A new
// Kubernetes UID retires the former identity without inheriting its last success.
func (c *Client) UpsertSandboxTemplateDefinition(ctx context.Context, definition SandboxTemplateDefinition) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		if err := execSQL(ctx, tx, `
			UPDATE sandbox_template_definition SET retired_at = NOW(), updated_at = NOW()
			WHERE namespace = $1 AND sandbox_template_name = $2 AND sandbox_template_uid <> $3 AND retired_at IS NULL
		`, definition.Namespace, definition.SandboxTemplateName, definition.SandboxTemplateUID); err != nil {
			return err
		}
		if err := execSQL(ctx, tx, `
			INSERT INTO sandbox_template_definition (namespace, sandbox_template_name, sandbox_template_uid, desired_revision)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (namespace, sandbox_template_uid) DO UPDATE SET
			    desired_revision = EXCLUDED.desired_revision, retired_at = NULL, updated_at = NOW()
		`, definition.Namespace, definition.SandboxTemplateName, definition.SandboxTemplateUID, definition.DesiredRevision); err != nil {
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

func (c *Client) RetireSandboxTemplateIdentities(ctx context.Context, namespace, name string) error {
	return execSQL(ctx, c.db, `
		UPDATE sandbox_template_definition SET retired_at = NOW(), updated_at = NOW()
		WHERE namespace = $1 AND sandbox_template_name = $2 AND retired_at IS NULL
	`, namespace, name)
}

// RecordSandboxRevision promotes only the active identity still desiring this
// revision. A late preparation response cannot replace a newer successful input.
func (c *Client) RecordSandboxRevision(ctx context.Context, revision SandboxRevision, ready bool) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := queryMany(ctx, tx, `
			SELECT 1 FROM sandbox_template_definition WHERE namespace = $1 AND sandbox_template_uid = $2 FOR UPDATE
		`, pgx.RowTo[int], revision.Namespace, revision.SandboxTemplateUID); err != nil {
			return err
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
		if !ready {
			return nil
		}
		return execSQL(ctx, tx, `
			UPDATE sandbox_template_definition p SET latest_successful_revision = r.revision, updated_at = NOW()
			FROM runtime_revision r JOIN sandbox_revision s USING (revision)
			WHERE r.revision = $1 AND p.namespace = r.namespace AND p.sandbox_template_uid = s.sandbox_template_uid
			    AND p.desired_revision = r.revision AND p.retired_at IS NULL
		`, revision.Revision)
	})
}

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
