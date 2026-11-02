package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
)

// UpsertAgentDefinition records the desired runtime revision for a
// Kubernetes Agent identity. Updating an existing definition revives it if retired and
// preserves its latest successful revision. It atomically retires older identities
// at the same names and rejects revisions whose deletion has started. Grants
// are canonicalized before writing; errors may include an uncertain commit outcome.
func (c *Client) UpsertAgentDefinition(ctx context.Context, definition AgentDefinition) error {
	grants, err := canonicalStoredGrants(definition.EgressGrant)
	if err != nil {
		return fmt.Errorf("validate source grants: %w", err)
	}
	return c.withTx(ctx, func(tx pgx.Tx) error {
		if err := retireAgentIdentities(ctx, tx, definition.Namespace, definition.AgentName, definition.AgentUID); err != nil {
			return err
		}
		if err := execSQL(ctx, tx, `
			INSERT INTO agent_definition (namespace, agent_name, agent_uid, desired_revision, grant_destinations, grant_credentials)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (namespace, agent_uid) DO UPDATE SET
			    agent_name = EXCLUDED.agent_name, desired_revision = EXCLUDED.desired_revision,
			    grant_destinations = EXCLUDED.grant_destinations, grant_credentials = EXCLUDED.grant_credentials,
			    retired_at = NULL, updated_at = NOW()
		`, definition.Namespace, definition.AgentName, definition.AgentUID, definition.DesiredRevision, grants.Destinations, grants.Credentials); err != nil {
			return err
		}

		// Lock Agents before revisions. Validate the resulting definition, including
		// its retained last-good pointer when reactivating a retired identity.
		type revisionStatus struct {
			Kind      string
			DeletedAt *time.Time
		}
		revisions, err := queryMany(ctx, tx, `
			SELECT r.kind, r.deleted_at FROM runtime_revision r
			JOIN agent_definition p
			  ON r.revision IN (p.desired_revision, p.latest_successful_revision)
			WHERE p.namespace = $1 AND p.agent_uid = $2
			ORDER BY r.revision
			FOR UPDATE OF r
		`, pgx.RowToStructByName[revisionStatus], definition.Namespace, definition.AgentUID)
		if err != nil {
			return err
		}
		for _, revision := range revisions {
			if revision.Kind != "agent" {
				return fmt.Errorf("agent preparation references a %s revision: %w", revision.Kind, ErrConflict)
			}
			if revision.DeletedAt != nil {
				return ErrObjectDeleting
			}
		}
		return nil
	})
}

// GetAgentEgressGrant returns the active identity's last admitted grants.
// Missing or retired identities return empty access; malformed stored grants fail.
func (c *Client) GetAgentEgressGrant(ctx context.Context, namespace, uid string) (egress.Access, error) {
	grants, err := queryOne(ctx, c.db, `
		SELECT grant_destinations AS destinations, grant_credentials AS credentials
		FROM agent_definition WHERE namespace = $1 AND agent_uid = $2 AND retired_at IS NULL
	`, pgx.RowToStructByName[egress.Access], namespace, uid)
	return decodeEgressGrant(grants, err)
}

// ListAgentEgressSources returns names for active Agent definitions and retired
// identities that still have non-deleted instances. Startup recovery includes
// retired identities because a crash may interrupt revocation after retirement.
func (c *Client) ListAgentEgressSources(ctx context.Context) ([]RuntimeEgressSource, error) {
	return queryMany(ctx, c.db, `
  SELECT DISTINCT 'agent' AS kind, d.namespace, d.agent_name AS name
  FROM agent_definition d
  WHERE d.retired_at IS NULL OR EXISTS (
    SELECT 1 FROM agent_revision a
    JOIN runtime_revision r ON r.revision = a.revision
    JOIN runtime_instance i ON i.prepared_revision = r.revision
    WHERE r.namespace = d.namespace AND a.agent_uid = d.agent_uid
      AND i.state <> 'RUNTIME_STATE_DELETED'
  )
  ORDER BY namespace, name
 `, pgx.RowToStructByName[RuntimeEgressSource])
}

// RecordRevisionOptions reports the state of the recorded ActorTemplate's golden build.
type RecordRevisionOptions struct {
	// Ready promotes the revision only while its source is active and still
	// desires it, and clears its golden retry state.
	Ready bool
	// GoldenFailedAt, when set, marks the recorded ActorTemplate's golden build
	// as failed at that time. Repeat reports for the same template keep the first.
	GoldenFailedAt time.Time
}

// RecordRuntimeRevision atomically stores immutable inputs and the golden
// build's retry state and, when Ready, promotes the revision for its active
// source if still desired. Existing inputs are preserved; actor UID, spec digest
// and update time refresh. Stale reports cannot replace the last successful
// revision. Deleting revisions return ErrObjectDeleting.
func (c *Client) RecordRuntimeRevision(ctx context.Context, revision RuntimeRevision, options RecordRevisionOptions) (GoldenRetry, error) {
	if revision.AgentCard == nil {
		return GoldenRetry{}, fmt.Errorf("runtime revision %s has no Agent Card", revision.Revision)
	}
	card, err := proto.Marshal(revision.AgentCard)
	if err != nil {
		return GoldenRetry{}, fmt.Errorf("encode runtime revision Agent Card: %w", err)
	}
	var retry GoldenRetry
	err = c.withTx(ctx, func(tx pgx.Tx) error {
		if options.Ready {
			// Match definition preparation and GC: source before revision.
			if _, err := queryMany(ctx, tx, `
				SELECT 1 FROM agent_definition p
				WHERE p.namespace = $1 AND p.agent_uid = $2 FOR UPDATE OF p
			`, pgx.RowTo[int], revision.Namespace, revision.AgentUID); err != nil {
				return err
			}
		}
		if err := recordRuntimeRevision(ctx, tx, runtimeRevisionRecord{
			RuntimeArtifact: RuntimeArtifact{Revision: revision.Revision, Kind: runtimeKindAgent, Namespace: revision.Namespace,
				ActorTemplateAtespace: revision.ActorTemplateAtespace, ActorTemplateName: revision.ActorTemplateName, ActorTemplateUID: revision.ActorTemplateUID, ActorTemplateSpecDigest: revision.ActorTemplateSpecDigest},
			SourceSnapshot: revision.SourceSnapshot, EgressDestinations: revision.EgressDestinations, Credentials: revision.Credentials, TokenPassthroughTargets: revision.TokenPassthroughTargets,
		}); err != nil {
			return err
		}
		if err := execSQL(ctx, tx, `
			INSERT INTO agent_revision (revision, agent_name, agent_uid, agent_card)
			VALUES ($1, $2, $3, $4) ON CONFLICT (revision) DO NOTHING
		`, revision.Revision, revision.AgentName, revision.AgentUID, card); err != nil {
			return err
		}
		var err error
		if retry, err = recordGoldenRetry(ctx, tx, revision.Revision, revision.ActorTemplateUID, options); err != nil || !options.Ready {
			return err
		}
		return execSQL(ctx, tx, `
			UPDATE agent_definition p
			SET latest_successful_revision = r.revision, updated_at = NOW()
			FROM agent_runtime_revision r
			WHERE r.revision = $1 AND p.namespace = $2 AND p.agent_uid = $3
			  AND p.namespace = r.namespace
			  AND p.agent_uid = r.agent_uid
			  AND p.desired_revision = r.revision
			  AND p.retired_at IS NULL
			  AND p.latest_successful_revision IS DISTINCT FROM r.revision
		`, revision.Revision, revision.Namespace, revision.AgentUID)
	})
	return retry, err
}

// GetRuntimeRevision returns a prepared revision and its decoded agent card, or
// ErrNotFound if absent.
func (c *Client) GetRuntimeRevision(ctx context.Context, revision string) (*RuntimeRevision, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT revision, namespace, agent_name, agent_uid,
		    source_snapshot, egress_destinations, token_passthrough_targets, credentials, actor_template_atespace, actor_template_name, actor_template_uid,
		    actor_template_spec_digest, agent_card, deleted_at FROM agent_runtime_revision WHERE revision = $1
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
	targets, err := egress.CanonicalGatewayTargets(row.TokenPassthroughTargets)
	if err != nil {
		return nil, fmt.Errorf("decode runtime revision %s tokenPassthrough targets: %w", row.Revision, err)
	}
	return &RuntimeRevision{
		Revision: row.Revision, Namespace: row.Namespace,
		AgentName: row.AgentName, AgentUID: row.AgentUID,
		SourceSnapshot: row.SourceSnapshot, AgentCard: card,
		EgressDestinations:      row.EgressDestinations,
		TokenPassthroughTargets: targets,
		Credentials:             credentials,
		ActorTemplateAtespace:   row.ActorTemplateAtespace, ActorTemplateName: row.ActorTemplateName,
		ActorTemplateUID: row.ActorTemplateUID, ActorTemplateSpecDigest: row.ActorTemplateSpecDigest,
	}, nil
}

// RetireAgentIdentities retires identities at the given namespace/Agent name.
// Existing sessions retain their pinned revisions. Missing and already-retired
// identities are a no-op.
func (c *Client) RetireAgentIdentities(ctx context.Context, namespace, name string) error {
	return retireAgentIdentities(ctx, c.db, namespace, name, "")
}

// retireAgentIdentities uses the caller's executor so retirement can commit
// atomically with definition preparation. An empty
// retainedUID retires every active identity at this source name.
func retireAgentIdentities(ctx context.Context, db dbExecutor, namespace, name, retainedUID string) error {
	return execSQL(ctx, db, `
		UPDATE agent_definition
		SET retired_at = NOW(), updated_at = NOW()
		WHERE namespace = $1 AND agent_name = $2
		  AND retired_at IS NULL AND agent_uid IS DISTINCT FROM NULLIF($3, '')
	`, namespace, name, retainedUID)
}

// ListUnreferencedRuntimeRevisions lists revisions unused by active Agents,
// instances, or checkpoints, including deletions still awaiting compute cleanup.
func (c *Client) ListUnreferencedRuntimeRevisions(ctx context.Context) ([]RuntimeArtifact, error) {
	return queryMany(ctx, c.db, `
		SELECT revision, kind, namespace, actor_template_atespace, actor_template_name, actor_template_uid, actor_template_spec_digest, golden_failure_uid, deleted_at
		FROM runtime_revision WHERE revision IN (SELECT revision FROM unreferenced_runtime_revision)
	`, pgx.RowToStructByName[RuntimeArtifact])
}

func getRuntimeArtifactForUpdate(ctx context.Context, tx pgx.Tx, revision string) (RuntimeArtifact, error) {
	return queryOne(ctx, tx, `
		SELECT revision, kind, namespace, actor_template_atespace, actor_template_name, actor_template_uid, actor_template_spec_digest, golden_failure_uid, deleted_at
		FROM runtime_revision WHERE revision = $1 FOR UPDATE
	`, pgx.RowToStructByName[RuntimeArtifact], revision)
}

// getAvailableRuntimeRevisionForUpdate pins agent inputs in the caller's transaction.
func getAvailableRuntimeRevisionForUpdate(ctx context.Context, tx pgx.Tx, revision string) (runtimeRevisionRow, error) {
	if err := lockRuntimeRevisionForReference(ctx, tx, revision, runtimeKindAgent); err != nil {
		return runtimeRevisionRow{}, err
	}
	return queryOne(ctx, tx, `
  SELECT revision, namespace, agent_name, agent_uid,
   source_snapshot, egress_destinations, token_passthrough_targets, credentials, actor_template_atespace, actor_template_name, actor_template_uid,
   actor_template_spec_digest, agent_card, deleted_at FROM agent_runtime_revision WHERE revision = $1
 `, pgx.RowToStructByName[runtimeRevisionRow], revision)
}

// lockRuntimeRevisionForReference excludes artifact cleanup until the new
// reference commits. Kind checks belong to the owning transactional write.
func lockRuntimeRevisionForReference(ctx context.Context, tx pgx.Tx, revision, kind string) error {
	artifact, err := getRuntimeArtifactForUpdate(ctx, tx, revision)
	if err != nil {
		return notFoundOr(err)
	}
	if artifact.Kind != kind {
		return fmt.Errorf("expected %s revision, got %s: %w", kind, artifact.Kind, ErrConflict)
	}
	if artifact.DeletedAt != nil {
		return ErrObjectDeleting
	}
	return nil
}

type runtimeRevisionRecord struct {
	RuntimeArtifact
	SourceSnapshot          json.RawMessage
	EgressDestinations      []string
	TokenPassthroughTargets []egresspolicy.GatewayTarget
	Credentials             []egress.Credential
}

// recordRuntimeRevision writes immutable common inputs and mutable artifact status.
// The caller writes its typed extension in the same transaction.
func recordRuntimeRevision(ctx context.Context, tx pgx.Tx, revision runtimeRevisionRecord) error {
	if revision.Credentials == nil {
		revision.Credentials = []egress.Credential{}
	}
	if revision.EgressDestinations == nil {
		revision.EgressDestinations = []string{}
	}
	if revision.TokenPassthroughTargets == nil {
		revision.TokenPassthroughTargets = []egresspolicy.GatewayTarget{}
	}
	result, err := tx.Exec(ctx, `
  INSERT INTO runtime_revision (revision, kind, namespace, source_snapshot, egress_destinations,
   actor_template_atespace, actor_template_name, actor_template_uid, actor_template_spec_digest, credentials, token_passthrough_targets)
  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
  ON CONFLICT (revision) DO UPDATE SET actor_template_uid = EXCLUDED.actor_template_uid,
   actor_template_spec_digest = EXCLUDED.actor_template_spec_digest, updated_at = NOW()
  WHERE runtime_revision.deleted_at IS NULL AND runtime_revision.kind = EXCLUDED.kind
 `, revision.Revision, revision.Kind, revision.Namespace, revision.SourceSnapshot, revision.EgressDestinations,
		revision.ActorTemplateAtespace, revision.ActorTemplateName, revision.ActorTemplateUID, revision.ActorTemplateSpecDigest, revision.Credentials, revision.TokenPassthroughTargets)
	if err != nil {
		return fmt.Errorf("record runtime revision %s: %w", revision.Revision, err)
	}
	if result.RowsAffected() != 0 {
		return nil
	}
	return lockRuntimeRevisionForReference(ctx, tx, revision.Revision, revision.Kind)
}

// BeginRuntimeRevisionDeletion marks an unreferenced revision as deleting,
// preventing new references before runtime cleanup. Retrying returns the pending
// revision; nil means the revision is missing or still referenced.
func (c *Client) BeginRuntimeRevisionDeletion(ctx context.Context, revision string) (*RuntimeArtifact, error) {
	var result *RuntimeArtifact
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := getRuntimeArtifactForUpdate(ctx, tx, revision)
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
		result = &row
		return nil
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
		if _, err := queryMany(ctx, tx, `
			SELECT 1 FROM sandbox_template_definition
			WHERE retired_at IS NOT NULL AND latest_successful_revision = $1
			ORDER BY namespace, sandbox_template_uid FOR UPDATE
		`, pgx.RowTo[int], revision); err != nil {
			return err
		}
		row, err := getRuntimeArtifactForUpdate(ctx, tx, revision)
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
		if err := execSQL(ctx, tx, `
			UPDATE sandbox_template_definition SET latest_successful_revision = NULL, updated_at = NOW()
			WHERE retired_at IS NOT NULL AND latest_successful_revision = $1
		`, revision); err != nil {
			return err
		}
		return execSQL(ctx, tx, `
			DELETE FROM runtime_revision WHERE revision = $1 AND deleted_at IS NOT NULL
		`, revision)
	})
}

type runtimeRevisionRow struct {
	Revision                string
	Namespace               string
	AgentName               string
	AgentUID                string
	SourceSnapshot          []byte
	EgressDestinations      []string
	TokenPassthroughTargets []egresspolicy.GatewayTarget
	Credentials             []egress.Credential
	ActorTemplateAtespace   string
	ActorTemplateName       string
	ActorTemplateUID        string
	ActorTemplateSpecDigest string
	AgentCard               []byte
	DeletedAt               *time.Time
}

// GoldenRetry is a revision's golden build retry state since its last success.
type GoldenRetry struct {
	// Retries counts the golden builds that failed since the last success.
	Retries int
	// FailedAt is when the recorded ActorTemplate's golden build failed, or zero
	// when the recorded template has not failed.
	FailedAt time.Time
	// New reports that this record call saw the failure first.
	New bool
}

// recordGoldenRetry updates the retry state from one report of the recorded
// ActorTemplate and returns it. Ready clears it, so a later failure starts again
// from the first backoff delay. A failure counts once per template UID, because
// reconciles see the same failed template on every pass until it is replaced.
func recordGoldenRetry(ctx context.Context, tx pgx.Tx, revision, templateUID string, options RecordRevisionOptions) (GoldenRetry, error) {
	var retry GoldenRetry
	switch {
	case options.Ready:
		if err := execSQL(ctx, tx, `
			UPDATE runtime_revision SET golden_retries = 0, golden_failure_uid = '', golden_failed_at = NULL, updated_at = NOW()
			WHERE revision = $1 AND golden_retries > 0
		`, revision); err != nil {
			return GoldenRetry{}, err
		}
	case !options.GoldenFailedAt.IsZero():
		result, err := tx.Exec(ctx, `
			UPDATE runtime_revision
			SET golden_retries = golden_retries + 1, golden_failure_uid = $2, golden_failed_at = $3, updated_at = NOW()
			WHERE revision = $1 AND golden_failure_uid <> $2
		`, revision, templateUID, options.GoldenFailedAt)
		if err != nil {
			return GoldenRetry{}, fmt.Errorf("record golden failure for runtime revision %s: %w", revision, err)
		}
		retry.New = result.RowsAffected() > 0
	}
	type goldenRetryRow struct {
		GoldenRetries  int
		GoldenFailedAt *time.Time
	}
	row, err := queryOne(ctx, tx, `
		SELECT golden_retries, CASE WHEN golden_failure_uid = actor_template_uid THEN golden_failed_at END AS golden_failed_at
		FROM runtime_revision WHERE revision = $1
	`, pgx.RowToStructByName[goldenRetryRow], revision)
	if err != nil {
		return GoldenRetry{}, fmt.Errorf("get golden retry for runtime revision %s: %w", revision, notFoundOr(err))
	}
	retry.Retries = row.GoldenRetries
	if row.GoldenFailedAt != nil {
		retry.FailedAt = *row.GoldenFailedAt
	}
	return retry, nil
}
