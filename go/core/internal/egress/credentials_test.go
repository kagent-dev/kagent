package egress

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalCredentials(t *testing.T) {
	base := Credential{Hostname: "API.Example.com.", Header: "Authorization", Prefix: "Bearer ", URI: "ate-secret://kubernetes.io/team/auth/token"}
	got, err := CanonicalCredentials([]Credential{base, base})
	require.NoError(t, err)
	require.Equal(t, []Credential{{Hostname: "api.example.com", Header: "authorization", Prefix: "Bearer ", URI: base.URI}}, got)
	require.Equal(t, "API.Example.com.", base.Hostname)
	for _, test := range []struct {
		name   string
		change func(*Credential)
	}{
		{"wildcard", func(c *Credential) { c.Hostname = "*.example.com" }},
		{"IP", func(c *Credential) { c.Hostname = "192.0.2.1" }},
		{"header", func(c *Credential) { c.Header = "Authorization\r\nInjected" }},
		{"prefix", func(c *Credential) { c.Prefix = "Bearer\n" }},
		{"provider", func(c *Credential) { c.URI = "https://example.com/secret" }},
		{"unknown authority", func(c *Credential) { c.URI = "ate-secret://vault.kubernetes.io/team/auth/token" }},
		{"missing key", func(c *Credential) { c.URI = "ate-secret://kubernetes.io/team/auth" }},
		{"query", func(c *Credential) { c.URI += "?key=other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			binding := base
			test.change(&binding)
			_, err := CanonicalCredentials([]Credential{binding})
			require.Error(t, err)
		})
	}
	other := base
	other.URI = "ate-secret://kubernetes.io/team/other/token"
	_, err = CanonicalCredentials([]Credential{base, other})
	require.ErrorContains(t, err, "conflicting credentials")

	google := Credential{Hostname: "us-east5-aiplatform.googleapis.com", Header: "authorization", Prefix: "Bearer ", URI: CredentialURI(GoogleAccessTokenAuthority, "team", "vertex", "credentials.json")}
	got, err = CanonicalCredentials([]Credential{google})
	require.NoError(t, err)
	require.Equal(t, "ate-secret://google-access-token.kubernetes.io/team/vertex/credentials.json", got[0].URI)
}
