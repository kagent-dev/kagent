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
		{"empty key", func(c *Credential) { c.URI = "ate-secret://k8s.io/default/team/auth/" }},
		{"mixed case Kubernetes provider", func(c *Credential) { c.URI = "ate-secret://K8s.io/default/team/auth/token" }},
		{"Kubernetes provider port", func(c *Credential) { c.URI = "ate-secret://k8s.io:443/default/team/auth/token" }},
		{"mixed case external provider", func(c *Credential) { c.URI = "ate-secret://Provider.example/token" }},
		{"external provider port", func(c *Credential) { c.URI = "ate-secret://provider.example:443/token" }},
		{"empty provider port", func(c *Credential) { c.URI = "ate-secret://provider.example:/token" }},
		{"IPv6 provider", func(c *Credential) { c.URI = "ate-secret://[2001:db8::1]/token" }},
		{"missing provider", func(c *Credential) { c.URI = "ate-secret:///team/auth/token" }},
		{"userinfo", func(c *Credential) { c.URI = "ate-secret://user@provider.example/token" }},
		{"opaque", func(c *Credential) { c.URI = "ate-secret:provider.example/token" }},
		{"empty query", func(c *Credential) { c.URI += "?" }},
		{"fragment", func(c *Credential) { c.URI += "#token" }},
		{"empty fragment", func(c *Credential) { c.URI += "#" }},
		{"invalid escape", func(c *Credential) { c.URI = "ate-secret://provider.example/%xx" }},
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
}

func TestCanonicalCredentialsExternalProvider(t *testing.T) {
	for _, uri := range []string{
		"ate-secret://provider.example/session-token",
		"ate-secret://caller-token.kagent.dev/token",
		"ate-secret://provider.example/default/https%3A%2F%2Fapi.example.com%3A8443",
	} {
		t.Run(uri, func(t *testing.T) {
			binding := Credential{Origin: "https://api.example.com:8443", Header: "authorization", Prefix: "Bearer ", URI: uri}
			got, err := CanonicalCredentials([]Credential{binding})
			require.NoError(t, err)
			require.Equal(t, []Credential{binding}, got)
		})
	}
}
