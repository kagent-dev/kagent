package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
)

// canonicalStoredGrants validates and normalizes source grants for NOT NULL storage.
func canonicalStoredGrants(grants egress.Access) (egress.Access, error) {
	canonical, err := egress.CanonicalAccess(grants.Destinations, grants.Credentials, grants.TokenPassthroughTargets)
	if err != nil {
		return egress.Access{}, err
	}
	if canonical.Destinations == nil {
		canonical.Destinations = []string{}
	}
	if canonical.Credentials == nil {
		canonical.Credentials = []egress.Credential{}
	}
	if canonical.TokenPassthroughTargets == nil {
		canonical.TokenPassthroughTargets = []egresspolicy.GatewayTarget{}
	}
	return canonical, nil
}

// GetRuntimeEgressPolicy merges pinned inputs with the active source UID's grants
// in one database snapshot. Missing/deleting revisions and malformed inputs fail
// closed. Retired or replaced identities contribute no grants. Older revisions
// retain pinned destinations and Secret bindings. Their passthrough targets are
// header reservations, not authority: only compatible live grants permit tokens.
func (c *Client) GetRuntimeEgressPolicy(ctx context.Context, revision string) (egress.Access, error) {
	type policyInputs struct {
		EgressDestinations           []string
		Credentials                  []egress.Credential
		TokenPassthroughTargets      []egresspolicy.GatewayTarget
		GrantDestinations            []string
		GrantCredentials             []egress.Credential
		GrantTokenPassthroughTargets []egresspolicy.GatewayTarget
	}
	row, err := queryOne(ctx, c.db, `
  SELECT r.egress_destinations, r.credentials, r.token_passthrough_targets,
    COALESCE(ad.grant_destinations, sd.grant_destinations, '{}') AS grant_destinations,
    COALESCE(ad.grant_credentials, sd.grant_credentials, '[]') AS grant_credentials,
    COALESCE(ad.grant_token_passthrough_targets, sd.grant_token_passthrough_targets, '[]') AS grant_token_passthrough_targets
  FROM runtime_revision r
  LEFT JOIN agent_revision a ON r.kind = 'agent' AND a.revision = r.revision
  LEFT JOIN sandbox_revision s ON r.kind = 'sandbox' AND s.revision = r.revision
  LEFT JOIN agent_definition ad ON ad.namespace = r.namespace AND ad.agent_uid = a.agent_uid AND ad.retired_at IS NULL
  LEFT JOIN sandbox_template_definition sd ON sd.namespace = r.namespace AND sd.sandbox_template_uid = s.sandbox_template_uid AND sd.retired_at IS NULL
  WHERE r.revision = $1 AND r.deleted_at IS NULL
    AND (a.revision IS NOT NULL OR s.revision IS NOT NULL)
 `, pgx.RowToStructByName[policyInputs], revision)
	if err != nil {
		return egress.Access{}, fmt.Errorf("get runtime egress policy %s: %w", revision, notFoundOr(err))
	}
	base, err := egress.CanonicalAccess(row.EgressDestinations, row.Credentials, row.TokenPassthroughTargets)
	if err != nil {
		return egress.Access{}, fmt.Errorf("decode runtime egress base %s: %w", revision, err)
	}
	grants, err := egress.CanonicalAccess(row.GrantDestinations, row.GrantCredentials, row.GrantTokenPassthroughTargets)
	if err != nil {
		return egress.Access{}, fmt.Errorf("decode runtime egress grants %s: %w", revision, err)
	}
	return egress.EffectiveAccess(base, grants)
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

// ListRuntimeEgressSourceTargets pages allocated instances across a source's UIDs
// in ID order, excluding deletions and unissued creations. Invalid sources return
// an error; a missing source yields an empty page.
func (c *Client) ListRuntimeEgressSourceTargets(ctx context.Context, source RuntimeEgressSource, afterID string, limit int) ([]RuntimeEgressTarget, error) {
	if source.Namespace == "" || source.Name == "" {
		return nil, fmt.Errorf("runtime egress source requires namespace and name")
	}
	switch source.Kind {
	case RuntimeEgressAgent:
		return queryMany(ctx, c.db, `
  SELECT i.id::text AS id, i.prepared_revision AS revision, r.namespace,
    r.actor_template_atespace AS atespace, r.actor_template_name AS template_name,
    COALESCE(s.actor_uid, '') AS actor_uid
  FROM runtime_instance i
  JOIN runtime_revision r ON r.revision = i.prepared_revision
  JOIN agent_revision a ON a.revision = r.revision
  JOIN session s ON s.id = i.id
  WHERE i.kind = 'agent' AND r.kind = 'agent' AND r.namespace = $1 AND a.agent_name = $2
    AND (i.operation <> 'RUNTIME_OPERATION_CREATE' OR i.executor_id IS NOT NULL)
    AND i.state <> 'RUNTIME_STATE_DELETED' AND i.operation <> 'RUNTIME_OPERATION_DELETE'
    AND i.id > COALESCE(NULLIF($3, '')::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
  ORDER BY i.id LIMIT $4
 `, pgx.RowToStructByName[RuntimeEgressTarget], source.Namespace, source.Name, afterID, limit)
	case RuntimeEgressSandbox:
		return queryMany(ctx, c.db, `
  SELECT i.id::text AS id, i.prepared_revision AS revision, r.namespace,
    r.actor_template_atespace AS atespace, r.actor_template_name AS template_name,
    '' AS actor_uid
  FROM runtime_instance i
  JOIN runtime_revision r ON r.revision = i.prepared_revision
  JOIN sandbox_revision s ON s.revision = r.revision
  WHERE i.kind = 'sandbox' AND r.kind = 'sandbox' AND r.namespace = $1 AND s.sandbox_template_name = $2
    AND (i.operation <> 'RUNTIME_OPERATION_CREATE' OR i.executor_id IS NOT NULL)
    AND i.state <> 'RUNTIME_STATE_DELETED' AND i.operation <> 'RUNTIME_OPERATION_DELETE'
    AND i.id > COALESCE(NULLIF($3, '')::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
  ORDER BY i.id LIMIT $4
 `, pgx.RowToStructByName[RuntimeEgressTarget], source.Namespace, source.Name, afterID, limit)
	default:
		return nil, fmt.Errorf("unknown runtime egress source kind %q", source.Kind)
	}
}

// decodeEgressGrant treats absent definitions as empty grants and rejects invalid
// durable rules. Callers scope the read to an active Kubernetes source identity.
func decodeEgressGrant(grants egress.Access, err error) (egress.Access, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return egress.Access{}, nil
	}
	if err != nil {
		return egress.Access{}, err
	}
	canonical, err := egress.CanonicalAccess(grants.Destinations, grants.Credentials, grants.TokenPassthroughTargets)
	if err != nil {
		return egress.Access{}, fmt.Errorf("decode source egress grants: %w", err)
	}
	return canonical, nil
}
