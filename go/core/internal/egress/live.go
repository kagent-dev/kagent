package egress

import (
	"context"
	"fmt"
	"slices"
)

// Policy is effective access plus a diagnostic when conflicting grants were withheld.
type Policy struct {
	Access
	WithheldGrants error
}

// PolicyStore reads effective policy from persisted revision inputs and source grants.
// A missing or malformed policy is an error, never permission to retain old access.
type PolicyStore interface {
	GetRuntimeEgressPolicy(context.Context, string) (Policy, error)
}

// EffectiveAccess merges validated pinned access and source grants. A conflict
// withholds all explicit grants while retaining the pinned base and its diagnostic.
func EffectiveAccess(base, grants Access, tokenPassthroughOrigins []string) (Access, error) {
	access, err := MergeAccess(base, grants, tokenPassthroughOrigins)
	if err != nil {
		return base, err
	}
	return access, nil
}

// MergeAccess validates live grants against the pinned runtime's credentials and
// caller-token origins. Inputs are never modified.
func MergeAccess(base, grants Access, tokenPassthroughOrigins []string) (Access, error) {
	access, err := CanonicalAccess(append(slices.Clone(base.Destinations), grants.Destinations...), append(slices.Clone(base.Credentials), grants.Credentials...))
	if err != nil {
		return Access{}, err
	}
	for _, binding := range access.Credentials {
		if slices.Contains(tokenPassthroughOrigins, binding.Origin) {
			return Access{}, fmt.Errorf("destination %q cannot combine caller-token passthrough with gateway credentials", binding.Origin)
		}
	}
	return access, nil
}
