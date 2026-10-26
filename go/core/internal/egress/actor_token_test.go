package egress

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActorTokenCredentials(t *testing.T) {
	base := Credential{Origin: "https://mcp.example.com", Header: "Authorization", Prefix: "Bearer ", ActorToken: &ActorToken{Audiences: []string{"mcp", "kagent-agent-info"}, ExpirationSeconds: 900}}
	other := base
	other.ActorToken = &ActorToken{Audiences: slices.Clone(base.ActorToken.Audiences), ExpirationSeconds: base.ActorToken.ExpirationSeconds}
	other.ActorToken.Audiences = []string{"kagent-agent-info", "mcp"}
	canonical, err := CanonicalCredentials([]Credential{base, other})
	require.NoError(t, err)
	require.Len(t, canonical, 1, "equivalent audience sets merge across allocations")
	require.Equal(t, []string{"mcp", "kagent-agent-info"}, base.ActorToken.Audiences, "canonicalization does not mutate inputs")
	access := Access{Destinations: []string{canonical[0].Origin}, Credentials: canonical}
	cloned, err := CanonicalAccess(access.Destinations, access.Credentials, nil)
	require.NoError(t, err)
	require.Equal(t, access, IntersectAccess(access, cloned), "decoded credentials compare by value")
	other.ActorToken.ExpirationSeconds = 600
	_, err = CanonicalCredentials([]Credential{base, other})
	require.ErrorContains(t, err, "conflicting credentials")
	for _, tc := range []struct {
		name   string
		modify func(*Credential)
	}{
		{"mixed sources", func(c *Credential) { c.URI = "ate-secret://k8s.io/default/team/auth/token" }},
		{"missing audience", func(c *Credential) { c.ActorToken.Audiences = nil }},
		{"empty audience", func(c *Credential) { c.ActorToken.Audiences = []string{""} }},
		{"duplicate audience", func(c *Credential) { c.ActorToken.Audiences = []string{"mcp", "mcp"} }},
		{"runtime authority", func(c *Credential) { c.ActorToken.Audiences = []string{"mcp", "kagent-runtime"} }},
		{"short lifetime", func(c *Credential) { c.ActorToken.ExpirationSeconds = 299 }},
		{"long lifetime", func(c *Credential) { c.ActorToken.ExpirationSeconds = 3601 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			c.ActorToken = &ActorToken{Audiences: slices.Clone(base.ActorToken.Audiences), ExpirationSeconds: base.ActorToken.ExpirationSeconds}
			tc.modify(&c)
			_, err := CanonicalCredentials([]Credential{c})
			require.Error(t, err)
		})
	}
}
