package egress

import (
	"context"
	"fmt"
	"slices"
)

// PolicyStore reads the controller's persisted intended policy for a revision.
// A missing or malformed policy is an error, never permission to retain old access.
type PolicyStore interface {
	GetRuntimeEgressPolicy(context.Context, string) (Access, error)
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
