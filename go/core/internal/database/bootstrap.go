package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/kagent-dev/kagent/go/api/bootstrap"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
)

// BootstrapBinding identifies a live revision's observed template incarnation.
type BootstrapBinding struct {
	Revision                string
	ActorTemplateUID        string
	ActorTemplateSpecDigest string
}

// PublishBootstrapRevision atomically publishes immutable revision inputs and their
// bundle for the active desired source. Identical retries renew a two-minute lease;
// conflicting bytes and deleting revisions fail. The lease expires naturally so
// abandoned or ambiguous compute requests cannot race immediate collection.
func (c *Client) PublishBootstrapRevision(ctx context.Context, revision RuntimeRevision, bundle *runtimev1.BootstrapServiceGetBootstrapConfigResponse) error {
	if err := bootstrap.ValidateBundle(bundle); err != nil {
		return err
	}
	if bundle.Revision != revision.Revision {
		return fmt.Errorf("bootstrap revision mismatch: %w", ErrConflict)
	}
	if revision.AgentCard == nil {
		return fmt.Errorf("runtime revision %s has no Agent Card", revision.Revision)
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(bundle)
	if err != nil {
		return fmt.Errorf("encode bootstrap bundle: %w", err)
	}
	card, err := proto.Marshal(revision.AgentCard)
	if err != nil {
		return fmt.Errorf("encode runtime revision Agent Card: %w", err)
	}
	// Publication never changes an observed template incarnation.
	revision.ActorTemplateUID = ""
	return c.withTx(ctx, func(tx pgx.Tx) error {
		desired, err := queryOne(ctx, tx, `
			SELECT desired_revision FROM agent_definition
			WHERE namespace = $1 AND agent_uid = $2 AND retired_at IS NULL FOR UPDATE
		`, pgx.RowTo[string], revision.Namespace, revision.AgentUID)
		if err != nil {
			return fmt.Errorf("resolve bootstrap source: %w", notFoundOr(err))
		}
		if desired != revision.Revision {
			return fmt.Errorf("bootstrap source revision changed: %w", ErrConflict)
		}
		if err := recordAgentRevision(ctx, tx, revision, card); err != nil {
			return err
		}
		result, err := tx.Exec(ctx, `
			INSERT INTO runtime_bootstrap (revision, bundle, preparing_until)
			VALUES ($1, $2, statement_timestamp() + INTERVAL '2 minutes')
			ON CONFLICT (revision) DO UPDATE SET preparing_until = EXCLUDED.preparing_until
			WHERE runtime_bootstrap.bundle = EXCLUDED.bundle
		`, revision.Revision, raw)
		if err != nil {
			return fmt.Errorf("publish bootstrap bundle: %w", err)
		}
		if result.RowsAffected() != 1 {
			return fmt.Errorf("bootstrap bundle is immutable: %w", ErrConflict)
		}
		return nil
	})
}

// GetBootstrapBinding returns a non-deleting published template binding, including
// an empty UID while initial template creation has not yet been recorded.
func (c *Client) GetBootstrapBinding(ctx context.Context, atespace, name string) (*BootstrapBinding, error) {
	row, err := queryOne(ctx, c.db, `
 SELECT r.revision, r.actor_template_uid, r.actor_template_spec_digest
 FROM runtime_revision r JOIN runtime_bootstrap b USING (revision)
 WHERE r.actor_template_atespace = $1 AND r.actor_template_name = $2 AND r.deleted_at IS NULL
 `, pgx.RowToStructByName[BootstrapBinding], atespace, name)
	if err != nil {
		return nil, fmt.Errorf("get bootstrap binding: %w", notFoundOr(err))
	}
	return &row, nil
}

// GetBootstrapBundle rechecks the authorized template incarnation and deletion
// state while reading the payload. Callers must authorize the binding first.
func (c *Client) GetBootstrapBundle(ctx context.Context, binding BootstrapBinding) (*runtimev1.BootstrapServiceGetBootstrapConfigResponse, error) {
	raw, err := queryOne(ctx, c.db, `
 SELECT b.bundle FROM runtime_bootstrap b JOIN runtime_revision r USING (revision)
 WHERE r.revision = $1 AND r.actor_template_uid = $2 AND r.actor_template_spec_digest = $3
 AND r.actor_template_uid <> '' AND r.deleted_at IS NULL
 `, pgx.RowTo[[]byte], binding.Revision, binding.ActorTemplateUID, binding.ActorTemplateSpecDigest)
	if err != nil {
		return nil, fmt.Errorf("get bootstrap bundle: %w", notFoundOr(err))
	}
	bundle := &runtimev1.BootstrapServiceGetBootstrapConfigResponse{}
	if err := proto.Unmarshal(raw, bundle); err != nil {
		return nil, fmt.Errorf("decode bootstrap bundle: %w", err)
	}
	if err := bootstrap.ValidateBundle(bundle); err != nil {
		return nil, fmt.Errorf("validate persisted bootstrap bundle: %w", err)
	}
	if bundle.Revision != binding.Revision {
		return nil, fmt.Errorf("persisted bootstrap revision mismatch")
	}
	return bundle, nil
}
