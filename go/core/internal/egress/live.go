package egress

import (
	"context"
	"slices"
)

// PolicyStore reads effective policy from persisted revision inputs and source grants.
// A missing or malformed policy is an error, never permission to retain old access.
type PolicyStore interface {
	GetRuntimeEgressPolicy(context.Context, string) (Access, error)
}

// EffectiveAccess retains the base and all grants compatible with this revision.
// Stored grants can conflict with an older actor's pinned base.
func EffectiveAccess(base, grants Access, tokenPassthroughOrigins []string) (Access, error) {
	filtered, _ := FilterConflicts(base, grants, tokenPassthroughOrigins)
	return MergeAccess(base, filtered, tokenPassthroughOrigins)
}

// MergeAccess validates live grants against the pinned runtime's credentials and
// caller-token origins. Inputs are never modified.
func MergeAccess(base, grants Access, tokenPassthroughOrigins []string) (Access, error) {
	return CanonicalAccess(append(slices.Clone(base.Destinations), grants.Destinations...), append(slices.Clone(base.Credentials), grants.Credentials...), tokenPassthroughOrigins)
}

// IntersectAccess retains only destinations and complete credential bindings
// present in both inputs. Inputs contain canonical rules; b may be an invalid
// union of otherwise valid policies. Taking a subset of valid a remains valid.
func IntersectAccess(a, b Access) Access {
	var result Access
	for _, origin := range a.Destinations {
		if slices.Contains(b.Destinations, origin) {
			result.Destinations = append(result.Destinations, origin)
		}
	}
	for _, credential := range a.Credentials {
		if slices.Contains(result.Destinations, credential.Origin) && slices.Contains(b.Credentials, credential) {
			result.Credentials = append(result.Credentials, credential)
		}
	}
	return result
}

// FilterConflicts retains grants compatible with a valid base and caller-token
// origins. Inputs must be individually canonical and valid. The base wins;
// sorted grant order determines which rules fit when the combined limits are
// exceeded. A conflicting credential does not remove its network destination.
// Neither input is modified; dropped origins are sorted and deduplicated.
func FilterConflicts(base, grants Access, tokenOrigins []string) (Access, []string) {
	if _, err := MergeAccess(base, grants, tokenOrigins); err == nil {
		return grants, nil
	}
	var result Access
	var dropped []string
	for _, origin := range grants.Destinations {
		candidate := Access{Destinations: append(slices.Clone(result.Destinations), origin)}
		if _, err := MergeAccess(base, candidate, tokenOrigins); err != nil {
			dropped = append(dropped, origin)
		} else {
			result = candidate
		}
	}
	for _, credential := range grants.Credentials {
		if !slices.Contains(result.Destinations, credential.Origin) {
			continue
		}
		candidate := Access{
			Destinations: result.Destinations,
			Credentials:  append(slices.Clone(result.Credentials), credential),
		}
		if _, err := MergeAccess(base, candidate, tokenOrigins); err != nil {
			dropped = append(dropped, credential.Origin)
		} else {
			result = candidate
		}
	}
	slices.Sort(dropped)
	return result, slices.Compact(dropped)
}
