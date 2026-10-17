package egress

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func canonicalTestAccess(t *testing.T, destinations []string, credentials ...Credential) Access {
	t.Helper()
	access, err := CanonicalAccess(destinations, credentials, nil)
	require.NoError(t, err)
	return access
}

func cloneAccess(a Access) Access {
	return Access{Destinations: slices.Clone(a.Destinations), Credentials: slices.Clone(a.Credentials)}
}

func TestIntersectAccess(t *testing.T) {
	binding := Credential{Origin: "https://a.example.com", Header: "authorization", URI: "ate-secret://k8s.io/default/team/key/token"}
	a := canonicalTestAccess(t, []string{"https://a.example.com:443", "https://removed.example.com"}, binding)
	changed := a.Credentials[0]
	changed.Prefix = "Bearer "
	b := Access{Destinations: []string{"https://a.example.com:443", "https://new.example.com:443"}, Credentials: []Credential{changed, a.Credentials[0]}}
	beforeA, beforeB := cloneAccess(a), cloneAccess(b)
	require.Equal(t, Access{Destinations: []string{a.Destinations[0]}, Credentials: a.Credentials}, IntersectAccess(a, b))
	b.Credentials = []Credential{changed}
	require.Empty(t, IntersectAccess(a, b).Credentials, "changed credential values are additions")
	b.Credentials = beforeB.Credentials
	require.Equal(t, beforeA, a)
	require.Equal(t, beforeB, b)
}

func TestFilterConflicts(t *testing.T) {
	binding := Credential{Origin: "https://model.example.com:443", Header: "authorization", URI: "ate-secret://k8s.io/default/team/model/token"}
	base := canonicalTestAccess(t, []string{binding.Origin}, binding)
	conflict := binding
	conflict.URI = "ate-secret://k8s.io/default/team/other/token"
	const extra = "https://tickets.example.com:443"
	for _, test := range []struct {
		name               string
		base, grants, want Access
		tokens, dropped    []string
	}{
		{name: "additive", base: base, grants: canonicalTestAccess(t, []string{extra}), want: canonicalTestAccess(t, []string{extra})},
		{name: "duplicate", base: base, grants: base, want: base},
		{name: "credential conflict keeps network access", base: base, grants: canonicalTestAccess(t, []string{binding.Origin, extra}, conflict),
			want: canonicalTestAccess(t, []string{binding.Origin, extra}), dropped: []string{binding.Origin}},
		{name: "protocol conflict keeps other grants", base: base, grants: canonicalTestAccess(t, []string{"http://model.example.com:443", extra}),
			want: canonicalTestAccess(t, []string{extra}), dropped: []string{"http://model.example.com:443"}},
		{name: "token passthrough", base: canonicalTestAccess(t, []string{binding.Origin}), tokens: []string{binding.Origin},
			grants: canonicalTestAccess(t, []string{binding.Origin, extra}, binding), want: canonicalTestAccess(t, []string{binding.Origin, extra}), dropped: []string{binding.Origin}},
	} {
		t.Run(test.name, func(t *testing.T) {
			beforeBase, beforeGrants := cloneAccess(test.base), cloneAccess(test.grants)
			filtered, dropped := FilterConflicts(test.base, test.grants, test.tokens)
			require.Equal(t, test.want, filtered)
			require.Equal(t, test.dropped, dropped)
			effective, err := EffectiveAccess(test.base, test.grants, test.tokens)
			require.NoError(t, err)
			want, err := MergeAccess(test.base, test.want, test.tokens)
			require.NoError(t, err)
			require.Equal(t, want, effective)
			require.Equal(t, beforeBase, test.base)
			require.Equal(t, beforeGrants, test.grants)
		})
	}
}

func TestFilterConflictsLimits(t *testing.T) {
	var destinations []string
	for i := range 255 {
		destinations = append(destinations, fmt.Sprintf("https://host%03d.example.com:443", i))
	}
	base := canonicalTestAccess(t, destinations)
	for _, input := range [][]string{
		{"https://z.example.com", "https://a.example.com"},
		{"https://a.example.com", "https://z.example.com"},
	} {
		filtered, dropped := FilterConflicts(base, canonicalTestAccess(t, input), nil)
		require.Equal(t, []string{"https://a.example.com:443"}, filtered.Destinations)
		require.Equal(t, []string{"https://z.example.com:443"}, dropped)
	}
	var credentials []Credential
	for i := range 16 {
		credentials = append(credentials, Credential{Origin: "https://model.example.com:443", Header: fmt.Sprintf("x-header-%02d", i), URI: "ate-secret://k8s.io/default/team/auth/token"})
	}
	base = canonicalTestAccess(t, []string{credentials[0].Origin}, credentials...)
	extra := credentials[0]
	extra.Header = "x-new"
	grants := canonicalTestAccess(t, base.Destinations, extra)
	filtered, dropped := FilterConflicts(base, grants, nil)
	require.Empty(t, filtered.Credentials)
	require.Equal(t, base.Destinations, filtered.Destinations)
	require.Equal(t, base.Destinations, dropped)
}
