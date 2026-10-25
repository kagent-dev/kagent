package egress

import (
	"fmt"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
)

const (
	IntegrationModelConfig     = "ModelConfig"
	IntegrationRemoteMCPServer = "RemoteMCPServer"
)

// IntegrationGrant records the source identity and passthrough bindings of an
// applied Agent configuration. It survives failed compilation independently of
// policy grants; the live source can still revoke these bindings.
type IntegrationGrant struct {
	Kind    string                       `json:"kind"`
	Name    string                       `json:"name"`
	UID     string                       `json:"uid"`
	Targets []egresspolicy.GatewayTarget `json:"targets"`
	// RevokedTargets records successful live-source removals independently of
	// the compiled bindings. Invalid source input must not restore these targets.
	RevokedTargets []egresspolicy.GatewayTarget `json:"revokedTargets,omitempty"`
}

// Access returns the destinations required by these passthrough bindings.
func (g IntegrationGrant) Access() Access {
	var access Access
	for _, target := range g.Targets {
		if slices.Contains(g.RevokedTargets, target) {
			continue
		}
		access.Destinations = append(access.Destinations, target.Origin)
		access.TokenPassthroughTargets = append(access.TokenPassthroughTargets, target)
	}
	return access
}

// WithAccess remembers a successful source resolution, including an empty grant.
// A later valid resolution can restore a binding; errors preserve this result.
func (g IntegrationGrant) WithAccess(access Access) IntegrationGrant {
	g.RevokedTargets = nil
	for _, target := range g.Targets {
		if !slices.Contains(access.TokenPassthroughTargets, target) {
			g.RevokedTargets = append(g.RevokedTargets, target)
		}
	}
	return g
}

// CanonicalIntegrationGrants validates durable source identities and bindings.
// Empty input becomes an empty array for NOT NULL storage.
func CanonicalIntegrationGrants(grants []IntegrationGrant) ([]IntegrationGrant, error) {
	result := make([]IntegrationGrant, 0, len(grants))
	seen := make(map[string]bool)
	for _, grant := range grants {
		if grant.Kind != IntegrationModelConfig && grant.Kind != IntegrationRemoteMCPServer {
			return nil, fmt.Errorf("invalid integration kind %q", grant.Kind)
		}
		key := grant.Kind + "/" + grant.Name
		if grant.Name == "" || grant.UID == "" || seen[key] || len(grant.Targets) == 0 {
			return nil, fmt.Errorf("invalid integration identity or bindings for %s", key)
		}
		seen[key] = true
		configured := grant
		configured.RevokedTargets = nil
		access := configured.Access()
		canonical, err := CanonicalAccess(access.Destinations, nil, access.TokenPassthroughTargets)
		if err != nil {
			return nil, fmt.Errorf("invalid integration %s: %w", key, err)
		}
		grant.Targets = canonical.TokenPassthroughTargets
		for _, target := range grant.RevokedTargets {
			if !slices.Contains(grant.Targets, target) {
				return nil, fmt.Errorf("revoked binding is not part of integration %s", key)
			}
		}
		grant = grant.WithAccess(grant.Access())
		result = append(result, grant)
	}
	slices.SortFunc(result, func(a, b IntegrationGrant) int { return strings.Compare(a.Kind+"/"+a.Name, b.Kind+"/"+b.Name) })
	return result, nil
}
