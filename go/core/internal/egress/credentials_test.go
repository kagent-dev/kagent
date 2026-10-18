package egress

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalCredentials(t *testing.T) {
	base := Credential{Origin: "https://API.Example.com.", Header: "Authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/team/auth/token"}
	got, err := CanonicalCredentials([]Credential{base, base})
	require.NoError(t, err)
	require.Equal(t, []Credential{{Origin: "https://api.example.com:443", Header: "authorization", Prefix: "Bearer ", URI: base.URI}}, got)
	require.Equal(t, "https://API.Example.com.", base.Origin)
	for _, test := range []struct {
		name   string
		change func(*Credential)
	}{
		{"wildcard", func(c *Credential) { c.Origin = "https://*.example.com" }},
		{"IP", func(c *Credential) { c.Origin = "https://192.0.2.1" }},
		{"header", func(c *Credential) { c.Header = "Authorization\r\nInjected" }},
		{"prefix", func(c *Credential) { c.Prefix = "Bearer\n" }},
		{"provider", func(c *Credential) { c.URI = "https://example.com/secret" }},
		{"missing key", func(c *Credential) { c.URI = "ate-secret://k8s.io/default/team/auth" }},
		{"old provider", func(c *Credential) { c.URI = "ate-secret://kubernetes.io/team/auth/token" }},
		{"authority with port", func(c *Credential) { c.URI = "ate-secret://k8s.io:50051/default/team/auth/token" }},
		{"uppercase authority", func(c *Credential) { c.URI = "ate-secret://Vault.Example.com/default/team/auth/token" }},
		{"IP authority", func(c *Credential) { c.URI = "ate-secret://192.0.2.1/default/team/auth/token" }},
		{"authority with underscore", func(c *Credential) { c.URI = "ate-secret://vault_provider/default/team/auth/token" }},
		{"missing locator", func(c *Credential) { c.URI = "ate-secret://k8s.io/team/auth/token" }},
		{"unsupported locator", func(c *Credential) { c.URI = "ate-secret://k8s.io/remote/team/auth/token" }},
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
	other.URI = "ate-secret://k8s.io/default/team/other/token"
	_, err = CanonicalCredentials([]Credential{base, other})
	require.ErrorContains(t, err, "conflicting credentials")

	// Any provider class the gateway routes is a DNS subdomain authority.
	minted := Credential{Origin: "https://us-east5-aiplatform.googleapis.com", Header: "authorization", Prefix: "Bearer ", URI: CredentialURI("tokens.example.com", "team", "vertex", "credentials.json")}
	got, err = CanonicalCredentials([]Credential{minted})
	require.NoError(t, err)
	require.Equal(t, "ate-secret://tokens.example.com/default/team/vertex/credentials.json", got[0].URI)
}
