package egress

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalCredentials(t *testing.T) {
	base := Credential{Hostname: "API.Example.com.", Header: "Authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/team/auth/token"}
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
		{"missing key", func(c *Credential) { c.URI = "ate-secret://k8s.io/default/team/auth" }},
		{"old provider", func(c *Credential) { c.URI = "ate-secret://kubernetes.io/team/auth/token" }},
		{"unknown authority", func(c *Credential) { c.URI = "ate-secret://vault.k8s.io/default/team/auth/token" }},
		{"missing locator", func(c *Credential) { c.URI = "ate-secret://k8s.io/team/auth/token" }},
		{"unsupported locator", func(c *Credential) { c.URI = "ate-secret://k8s.io/remote/team/auth/token" }},
		{"query", func(c *Credential) { c.URI += "?key=other" }},
		{"unknown scope", func(c *Credential) { c.Scope = "session" }},
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

	// The Google provider mints an access token from the key the URI names.
	google := Credential{Hostname: "us-east5-aiplatform.googleapis.com", Header: "authorization", Prefix: "Bearer ", URI: CredentialURI(GoogleAccessTokenAuthority, "team", "vertex", "credentials.json")}
	got, err = CanonicalCredentials([]Credential{google})
	require.NoError(t, err)
	require.Equal(t, "ate-secret://google-access-token.k8s.io/default/team/vertex/credentials.json", got[0].URI)
	golden := base
	golden.Scope = ScopeGolden
	_, err = CanonicalCredentials([]Credential{base, golden})
	require.ErrorContains(t, err, "conflicting credentials", "a scope does not let two bindings share a host and header")
}

func TestForActors(t *testing.T) {
	model := Credential{Hostname: "api.example.com", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/team/model/key"}
	artifact := Credential{Hostname: "git.example.com", Header: "authorization", Prefix: "Basic ", URI: "ate-secret://k8s.io/default/team/git/token", Scope: ScopeGolden}
	bindings := []Credential{model, artifact}
	require.Equal(t, []Credential{model}, ForActors(bindings))
	require.Equal(t, []Credential{model, artifact}, bindings, "the revision's own bindings are left as they are")
	require.Empty(t, ForActors(nil))
}
