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
	Kind               string
	Namespace          string
	SourceName         string
	SourceUID          string
	EgressDestinations []string
	Credentials        []egress.Credential
	PassthroughOrigins []string
}

// GetRuntimeEgress reads immutable access and source identity for either runtime
// kind. Missing revisions return ErrNotFound; malformed grants return an error.
func (c *Client) GetRuntimeEgress(ctx context.Context, revision string) (*RuntimeEgress, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT r.kind, r.namespace, r.egress_destinations, r.credentials, r.passthrough_origins,
		    COALESCE(a.agent_name, s.sandbox_template_name) AS source_name,
		    COALESCE(a.agent_uid, s.sandbox_template_uid) AS source_uid
		FROM runtime_revision r
		LEFT JOIN agent_revision a USING (revision)
		LEFT JOIN sandbox_revision s USING (revision)
		WHERE r.revision = $1
	`, pgx.RowToStructByName[RuntimeEgress], revision)
	if err != nil {
		return nil, fmt.Errorf("get runtime egress %s: %w", revision, notFoundOr(err))
	}
	access, err := egress.MergeAccess(egress.Access{Destinations: row.EgressDestinations, Credentials: row.Credentials}, egress.Access{}, row.PassthroughOrigins)
	if err != nil {
		return nil, fmt.Errorf("decode runtime egress %s: %w", revision, err)
	}
	row.EgressDestinations, row.Credentials = access.Destinations, access.Credentials
	return &row, nil
}

// RuntimeEgressTarget identifies an allocated runtime, including suspended or
// partially created actors that still need live policy reconciliation.
type RuntimeEgressTarget struct {
	ID           string
	Revision     string
	Atespace     string
	TemplateName string
	ActorUID     string
}

// ListRuntimeEgressTargets lists a bounded page in ID order. Deleting instances
// and unissued creations are excluded; a concurrent creation is picked up by the next reconciliation.
func (c *Client) ListRuntimeEgressTargets(ctx context.Context, afterID string, limit int) ([]RuntimeEgressTarget, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT i.id::text AS id, i.prepared_revision AS revision, r.actor_template_atespace AS atespace,
		    r.actor_template_name AS template_name, COALESCE(s.actor_uid, '') AS actor_uid
		FROM runtime_instance i JOIN runtime_revision r ON r.revision = i.prepared_revision
		LEFT JOIN session s ON s.id = i.id
		WHERE i.id > COALESCE(NULLIF($1, '')::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
		  AND (i.operation <> 'RUNTIME_OPERATION_CREATE' OR i.executor_id IS NOT NULL)
		  AND i.state <> 'RUNTIME_STATE_DELETED' AND i.operation <> 'RUNTIME_OPERATION_DELETE'
		ORDER BY i.id LIMIT $2
	`, pgx.RowToStructByName[RuntimeEgressTarget], afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list runtime egress targets: %w", err)
	}
	return rows, nil
}
