package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kagent-dev/kagent/go/core/internal/egress"
)

type definitionGrantRow struct {
	GrantDestinations []string
	GrantCredentials  []egress.Credential
	RetiredAt         *time.Time
}

// canonicalStoredGrants validates and normalizes source grants for NOT NULL storage.
func canonicalStoredGrants(grants egress.Access) (egress.Access, error) {
	canonical, err := egress.CanonicalAccess(grants.Destinations, grants.Credentials)
	if err != nil {
		return egress.Access{}, err
	}
	if canonical.Destinations == nil {
		canonical.Destinations = []string{}
	}
	if canonical.Credentials == nil {
		canonical.Credentials = []egress.Credential{}
	}
	return canonical, nil
}

// GetRuntimeEgressPolicy merges pinned inputs with the active source UID's grants
// in one database snapshot. Missing/deleting revisions and malformed inputs fail
// closed. Retired or replaced identities contribute no grants. Merge conflicts
// retain pinned access and return a diagnostic, not a failed read.
func (c *Client) GetRuntimeEgressPolicy(ctx context.Context, revision string) (egress.Policy, error) {
	type policyInputs struct {
		EgressDestinations      []string
		Credentials             []egress.Credential
		TokenPassthroughOrigins []string
		GrantDestinations       []string
		GrantCredentials        []egress.Credential
	}
	row, err := queryOne(ctx, c.db, `
  SELECT r.egress_destinations, r.credentials, r.token_passthrough_origins,
    COALESCE(ad.grant_destinations, sd.grant_destinations, '{}') AS grant_destinations,
    COALESCE(ad.grant_credentials, sd.grant_credentials, '[]') AS grant_credentials
  FROM runtime_revision r
  LEFT JOIN agent_revision a ON r.kind = 'agent' AND a.revision = r.revision
  LEFT JOIN sandbox_revision s ON r.kind = 'sandbox' AND s.revision = r.revision
  LEFT JOIN agent_definition ad ON ad.namespace = r.namespace AND ad.agent_uid = a.agent_uid AND ad.retired_at IS NULL
  LEFT JOIN sandbox_template_definition sd ON sd.namespace = r.namespace AND sd.sandbox_template_uid = s.sandbox_template_uid AND sd.retired_at IS NULL
  WHERE r.revision = $1 AND r.deleted_at IS NULL
    AND (a.revision IS NOT NULL OR s.revision IS NOT NULL)
 `, pgx.RowToStructByName[policyInputs], revision)
	if err != nil {
		return egress.Policy{}, fmt.Errorf("get runtime egress policy %s: %w", revision, notFoundOr(err))
	}
	for i, raw := range row.TokenPassthroughOrigins {
		origin, err := egress.ParseOrigin(raw)
		if err != nil {
			return egress.Policy{}, fmt.Errorf("decode runtime token origins %s: %w", revision, err)
		}
		row.TokenPassthroughOrigins[i] = origin.String()
	}
	base, err := egress.MergeAccess(egress.Access{Destinations: row.EgressDestinations, Credentials: row.Credentials}, egress.Access{}, row.TokenPassthroughOrigins)
	if err != nil {
		return egress.Policy{}, fmt.Errorf("decode runtime egress base %s: %w", revision, err)
	}
	grants, err := egress.CanonicalAccess(row.GrantDestinations, row.GrantCredentials)
	if err != nil {
		return egress.Policy{}, fmt.Errorf("decode runtime egress grants %s: %w", revision, err)
	}
	access, withheld := egress.EffectiveAccess(base, grants, row.TokenPassthroughOrigins)
	return egress.Policy{Access: access, WithheldGrants: withheld}, nil
}

var _ egress.PolicyStore = (*Client)(nil)

// RuntimeEgressSourceKind identifies a Kubernetes owner of runtime instances.
type RuntimeEgressSourceKind string

const (
	RuntimeEgressAgent   RuntimeEgressSourceKind = runtimeKindAgent
	RuntimeEgressSandbox RuntimeEgressSourceKind = runtimeKindSandbox
)

// RuntimeEgressSource includes former UIDs whose grants must be revoked on recreation.
type RuntimeEgressSource struct {
	Kind      RuntimeEgressSourceKind
	Namespace string
	Name      string
}

// RuntimeEgressTarget identifies allocated instances, including suspended actors.
type RuntimeEgressTarget struct {
	ID           string
	Revision     string
	Namespace    string
	Atespace     string
	TemplateName string
	ActorUID     string
}

// ListRuntimeEgressTargets pages all allocated instances in ID order for startup
// recovery. Deleting instances and unissued creations are excluded; concurrent
// creation reads effective policy itself and rechecks it after applying.
func (c *Client) ListRuntimeEgressTargets(ctx context.Context, afterID string, limit int) ([]RuntimeEgressTarget, error) {
	return queryMany(ctx, c.db, `
  SELECT target.id::text AS id, revision, namespace, atespace, template_name, actor_uid
  FROM runtime_egress_target target
  WHERE target.id > COALESCE(NULLIF($1, '')::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
  ORDER BY target.id LIMIT $2
 `, pgx.RowToStructByName[RuntimeEgressTarget], afterID, limit)
}

// ListRuntimeEgressSourceTargets pages allocated instances across a source's UIDs
// in ID order, excluding deletions and unissued creations. Invalid sources return
// an error; a missing source yields an empty page.
func (c *Client) ListRuntimeEgressSourceTargets(ctx context.Context, source RuntimeEgressSource, afterID string, limit int) ([]RuntimeEgressTarget, error) {
	if source.Namespace == "" || source.Name == "" {
		return nil, fmt.Errorf("runtime egress source requires namespace and name")
	}
	switch source.Kind {
	case RuntimeEgressAgent, RuntimeEgressSandbox:
	default:
		return nil, fmt.Errorf("unknown runtime egress source kind %q", source.Kind)
	}
	return queryMany(ctx, c.db, `
  SELECT target.id::text AS id, revision, namespace, atespace, template_name, actor_uid
  FROM runtime_egress_target target
  WHERE source_kind = $1 AND namespace = $2 AND source_name = $3
    AND target.id > COALESCE(NULLIF($4, '')::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
  ORDER BY target.id LIMIT $5
 `, pgx.RowToStructByName[RuntimeEgressTarget], source.Kind, source.Namespace, source.Name, afterID, limit)
}
