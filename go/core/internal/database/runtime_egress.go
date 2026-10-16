package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kagent-dev/kagent/go/core/internal/egress"
)

// RuntimeEgress retains integration access and the source identity of a pinned
// revision. Explicit policy grants are resolved from the current Kubernetes source.
type RuntimeEgress struct {
	Kind                    RuntimeEgressSourceKind
	Namespace               string
	SourceName              string
	SourceUID               string
	EgressDestinations      []string
	Credentials             []egress.Credential
	TokenPassthroughOrigins []string
}

// GetRuntimeEgress reads immutable access and source identity for either runtime
// kind. Missing revisions return ErrNotFound; malformed grants return an error.
func (c *Client) GetRuntimeEgress(ctx context.Context, revision string) (*RuntimeEgress, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT r.kind, r.namespace, r.egress_destinations, r.credentials, r.token_passthrough_origins,
		    COALESCE(a.agent_name, s.sandbox_template_name) AS source_name,
		    COALESCE(a.agent_uid, s.sandbox_template_uid) AS source_uid
		FROM runtime_revision r
		LEFT JOIN agent_revision a USING (revision)
		LEFT JOIN sandbox_revision s USING (revision)
		WHERE r.revision = $1 AND r.deleted_at IS NULL
	`, pgx.RowToStructByName[RuntimeEgress], revision)
	if err != nil {
		return nil, fmt.Errorf("get runtime egress %s: %w", revision, notFoundOr(err))
	}
	access, err := egress.MergeAccess(egress.Access{Destinations: row.EgressDestinations, Credentials: row.Credentials}, egress.Access{}, row.TokenPassthroughOrigins)
	if err != nil {
		return nil, fmt.Errorf("decode runtime egress %s: %w", revision, err)
	}
	row.EgressDestinations, row.Credentials = access.Destinations, access.Credentials
	return &row, nil
}

// RuntimeEgressSourceKind identifies the Kubernetes owner of a runtime revision.
type RuntimeEgressSourceKind string

const (
	RuntimeEgressAgent   RuntimeEgressSourceKind = runtimeKindAgent
	RuntimeEgressSandbox RuntimeEgressSourceKind = runtimeKindSandbox
)

// RuntimeEgressSource selects all revisions of an Agent or SandboxTemplate,
// including former UIDs whose grants must be revoked after source recreation.
type RuntimeEgressSource struct {
	Kind      RuntimeEgressSourceKind
	Namespace string
	Name      string
}

// ListRuntimeEgressRevisions lists retained revisions in key order for recovery,
// including revisions without actors. Deleting revisions are excluded.
func (c *Client) ListRuntimeEgressRevisions(ctx context.Context, after string, limit int) ([]string, error) {
	return queryMany(ctx, c.db, `
        SELECT revision FROM runtime_revision
        WHERE revision > $1 AND deleted_at IS NULL ORDER BY revision LIMIT $2
    `, pgx.RowTo[string], after, limit)
}

// ListRuntimeEgressSourceRevisions lists a source's retained revisions across
// Kubernetes UIDs. Empty and unsupported kinds are errors, never sweep selectors.
func (c *Client) ListRuntimeEgressSourceRevisions(ctx context.Context, source RuntimeEgressSource, after string, limit int) ([]string, error) {
	if source.Namespace == "" || source.Name == "" {
		return nil, fmt.Errorf("runtime egress source requires namespace and name")
	}
	switch source.Kind {
	case RuntimeEgressAgent:
		return queryMany(ctx, c.db, `
            SELECT r.revision FROM runtime_revision r JOIN agent_revision a USING (revision)
            WHERE r.namespace = $1 AND a.agent_name = $2 AND r.revision > $3
              AND r.deleted_at IS NULL ORDER BY r.revision LIMIT $4
        `, pgx.RowTo[string], source.Namespace, source.Name, after, limit)
	case RuntimeEgressSandbox:
		return queryMany(ctx, c.db, `
            SELECT r.revision FROM runtime_revision r JOIN sandbox_revision s USING (revision)
            WHERE r.namespace = $1 AND s.sandbox_template_name = $2 AND r.revision > $3
              AND r.deleted_at IS NULL ORDER BY r.revision LIMIT $4
        `, pgx.RowTo[string], source.Namespace, source.Name, after, limit)
	default:
		return nil, fmt.Errorf("unknown runtime egress source kind %q", source.Kind)
	}
}

// SetRuntimeEgressPolicy publishes controller output for a retained revision.
// It writes and returns true only when the canonical policy changes.
// It does not mutate the revision's pinned inputs or readiness. Missing/deleting
// revisions return ErrNotFound; malformed policy input is rejected before writing.
func (c *Client) SetRuntimeEgressPolicy(ctx context.Context, revision string, access egress.Access) (bool, error) {
	canonical, err := egress.CanonicalAccess(access.Destinations, access.Credentials)
	if err != nil {
		return false, fmt.Errorf("validate runtime egress policy %s: %w", revision, err)
	}
	if canonical.Destinations == nil {
		canonical.Destinations = []string{}
	}
	if canonical.Credentials == nil {
		canonical.Credentials = []egress.Credential{}
	}
	changed, err := queryOne(ctx, c.db, `
        WITH retained AS (
            SELECT revision FROM runtime_revision WHERE revision = $1 AND deleted_at IS NULL
        ), changed AS (
            INSERT INTO runtime_egress_policy (revision, destinations, credentials)
            SELECT revision, $2, $3 FROM retained
            ON CONFLICT (revision) DO UPDATE SET destinations = EXCLUDED.destinations, credentials = EXCLUDED.credentials
            WHERE (runtime_egress_policy.destinations, runtime_egress_policy.credentials)
                IS DISTINCT FROM (EXCLUDED.destinations, EXCLUDED.credentials)
            RETURNING revision
        )
        SELECT EXISTS (SELECT 1 FROM changed) FROM retained
    `, pgx.RowTo[bool], revision, canonical.Destinations, canonical.Credentials)
	if err != nil {
		return false, fmt.Errorf("store runtime egress policy %s: %w", revision, notFoundOr(err))
	}
	return changed, nil
}

// GetRuntimeEgressPolicy reads the latest controller output, independent of any
// replica's Kubernetes cache. Missing and malformed policies fail closed.
func (c *Client) GetRuntimeEgressPolicy(ctx context.Context, revision string) (egress.Access, error) {
	access, err := queryOne(ctx, c.db, `
        SELECT destinations, credentials FROM runtime_egress_policy WHERE revision = $1
    `, pgx.RowToStructByName[egress.Access], revision)
	if err != nil {
		return egress.Access{}, fmt.Errorf("get runtime egress policy %s: %w", revision, notFoundOr(err))
	}
	canonical, err := egress.CanonicalAccess(access.Destinations, access.Credentials)
	if err != nil {
		return egress.Access{}, fmt.Errorf("decode runtime egress policy %s: %w", revision, err)
	}
	return canonical, nil
}

var _ egress.PolicyStore = (*Client)(nil)

// RuntimeEgressTarget identifies an allocated runtime, including suspended or
// partially created actors that still need live policy reconciliation.
type RuntimeEgressTarget struct {
	ID           string
	Revision     string
	Atespace     string
	TemplateName string
	ActorUID     string
}

// ListRuntimeEgressTargets lists a revision's allocated instances in ID order.
// Deleting instances and unissued creations are excluded. Concurrent creation
// reads the persisted policy itself and rechecks it after applying.
func (c *Client) ListRuntimeEgressTargets(ctx context.Context, revision, afterID string, limit int) ([]RuntimeEgressTarget, error) {
	rows, err := queryMany(ctx, c.db, `
        SELECT i.id::text AS id, i.prepared_revision AS revision, r.actor_template_atespace AS atespace,
            r.actor_template_name AS template_name, COALESCE(s.actor_uid, '') AS actor_uid
        FROM runtime_instance i JOIN runtime_revision r ON r.revision = i.prepared_revision
        LEFT JOIN session s ON s.id = i.id
        WHERE i.prepared_revision = $1
          AND i.id > COALESCE(NULLIF($2, '')::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
          AND (i.operation <> 'RUNTIME_OPERATION_CREATE' OR i.executor_id IS NOT NULL)
          AND i.state <> 'RUNTIME_STATE_DELETED' AND i.operation <> 'RUNTIME_OPERATION_DELETE'
        ORDER BY i.id LIMIT $3
    `, pgx.RowToStructByName[RuntimeEgressTarget], revision, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list runtime egress targets: %w", err)
	}
	return rows, nil
}
