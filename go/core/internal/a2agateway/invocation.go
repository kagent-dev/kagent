package a2agateway

import (
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/pkg/invocationauth"
)

// invocationTargets snapshots admitted headers without copying credential bytes.
// ADKs retain this snapshot only for the invocation being dispatched.
func invocationTargets(access egress.Access) []invocationauth.Target {
	targets := make(map[string]*invocationauth.Target)
	get := func(origin string) *invocationauth.Target {
		if targets[origin] == nil {
			targets[origin] = &invocationauth.Target{Origin: origin, Headers: map[string]string{}}
		}
		return targets[origin]
	}
	for _, c := range access.Credentials {
		get(c.Origin).Headers[c.Header] = c.Prefix + "kagent-credential-injected"
	}
	for _, c := range access.TokenPassthroughTargets {
		t := get(c.Origin)
		t.Passthrough = append(t.Passthrough, invocationauth.Header{Name: c.Header, Prefix: c.Prefix})
	}
	result := make([]invocationauth.Target, 0, len(targets))
	for _, target := range targets {
		result = append(result, *target)
	}
	slices.SortFunc(result, func(a, b invocationauth.Target) int { return strings.Compare(a.Origin, b.Origin) })
	return result
}
