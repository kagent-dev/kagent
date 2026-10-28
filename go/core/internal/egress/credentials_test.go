package egress

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
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
		{"mixed case external provider", func(c *Credential) { c.URI = "ate-secret://Provider.example/default/token" }},
		{"external provider port", func(c *Credential) { c.URI = "ate-secret://provider.example:443/default/token" }},
		{"empty provider port", func(c *Credential) { c.URI = "ate-secret://provider.example:/default/token" }},
		{"IPv6 provider", func(c *Credential) { c.URI = "ate-secret://[2001:db8::1]/default/token" }},
		{"missing provider", func(c *Credential) { c.URI = "ate-secret:///team/auth/token" }},
		{"userinfo", func(c *Credential) { c.URI = "ate-secret://user@provider.example/default/token" }},
		{"opaque", func(c *Credential) { c.URI = "ate-secret:provider.example/token" }},
		{"empty query", func(c *Credential) { c.URI += "?" }},
		{"fragment", func(c *Credential) { c.URI += "#token" }},
		{"empty fragment", func(c *Credential) { c.URI += "#" }},
		{"invalid escape", func(c *Credential) { c.URI = "ate-secret://provider.example/%xx" }},
		{"invalid provider DNS name", func(c *Credential) { c.URI = "ate-secret://provider_name/default/token" }},
		{"provider trailing dot", func(c *Credential) { c.URI = "ate-secret://provider.example./default/token" }},
		{"missing path", func(c *Credential) { c.URI = "ate-secret://provider.example" }},
		{"single path segment", func(c *Credential) { c.URI = "ate-secret://provider.example/token" }},
		{"empty path segment", func(c *Credential) { c.URI = "ate-secret://provider.example/default//token" }},
		{"empty first path segment", func(c *Credential) { c.URI = "ate-secret://provider.example//default/token" }},
		{"trailing slash", func(c *Credential) { c.URI = "ate-secret://provider.example/default/token/" }},
		{"encoded path", func(c *Credential) {
			c.URI = "ate-secret://provider.example/default/https%3A%2F%2Fapi.example.com%3A8443"
		}},
		{"encoded unreserved character", func(c *Credential) { c.URI = "ate-secret://provider.example/default/%74oken" }},
		{"unescaped space", func(c *Credential) { c.URI = "ate-secret://provider.example/default/session token" }},
		{"encoded Kubernetes key", func(c *Credential) { c.URI = "ate-secret://k8s.io/default/team/auth/%74oken" }},
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

func TestCanonicalGatewayTargets(t *testing.T) {
	target := egresspolicy.GatewayTarget{Origin: "https://API.Example.com.:0443", Header: "Authorization", Prefix: "Bearer "}
	got, err := CanonicalGatewayTargets([]egresspolicy.GatewayTarget{target, target, {Origin: "http://api.example.com", Header: "x-api-key"}})
	require.NoError(t, err)
	require.Equal(t, []egresspolicy.GatewayTarget{
		{Origin: "http://api.example.com:80", Header: "x-api-key"},
		{Origin: "https://api.example.com:443", Header: "authorization", Prefix: "Bearer "},
	}, got)
	for _, test := range []struct {
		name   string
		change func(*egresspolicy.GatewayTarget)
	}{
		{"wildcard", func(g *egresspolicy.GatewayTarget) { g.Origin = "https://*.example.com" }},
		{"path", func(g *egresspolicy.GatewayTarget) { g.Origin = "https://api.example.com/mcp" }},
		{"header", func(g *egresspolicy.GatewayTarget) { g.Header = "Authorization\r\nInjected" }},
		{"prefix", func(g *egresspolicy.GatewayTarget) { g.Prefix = "Bearer\n" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalid := target
			test.change(&invalid)
			_, err := CanonicalGatewayTargets([]egresspolicy.GatewayTarget{invalid})
			require.Error(t, err)
		})
	}
	other := target
	other.Prefix = ""
	_, err = CanonicalGatewayTargets([]egresspolicy.GatewayTarget{target, other})
	require.ErrorContains(t, err, "conflicting gateway targets")
}

func TestCanonicalCredentialsExternalProvider(t *testing.T) {
	for _, uri := range []string{
		"ate-secret://provider.example/default/session-token",
		"ate-secret://caller-token.kagent.dev/default/token",
		"ate-secret://provider.example/default/api.example.com:8443",
		"ate-secret://caller-token.example/v1/https/api.example.com:8443",
	} {
		t.Run(uri, func(t *testing.T) {
			binding := Credential{Origin: "https://api.example.com:8443", Header: "authorization", Prefix: "Bearer ", URI: uri}
			got, err := CanonicalCredentials([]Credential{binding})
			require.NoError(t, err)
			require.Equal(t, []Credential{binding}, got)
		})
	}
}
