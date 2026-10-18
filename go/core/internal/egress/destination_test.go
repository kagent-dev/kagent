package egress

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalAccess(t *testing.T) {
	credential := Credential{Origin: "https://API.example.com", Header: "Authorization", URI: "ate-secret://k8s.io/default/team/auth/token"}
	access, err := CanonicalAccess([]string{"https://*.example.com", "https://api.example.com:0443", "https://API.example.com.", "https://api.example.com:8443", "http://api.example.com"}, []Credential{credential, credential}, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"http://api.example.com:80", "https://*.example.com:443", "https://api.example.com:443", "https://api.example.com:8443"}, access.Destinations)
	require.Len(t, access.Credentials, 1)
	require.Equal(t, "https://api.example.com:443", access.Credentials[0].Origin)
	_, err = CanonicalAccess([]string{credential.Origin}, []Credential{credential}, []string{"https://api.example.com:443"})
	require.ErrorContains(t, err, "cannot combine caller-token passthrough with gateway credentials")
	_, err = CanonicalAccess([]string{credential.Origin}, []Credential{credential}, []string{"https://api.example.com:8443", "http://api.example.com:443"})
	require.NoError(t, err, "caller tokens on different origins do not conflict")
	for _, raw := range []string{"https://*", "https://*.com", "https://foo.*.com", "https://192.0.2.1", "https://[::1]", "https://user@api.example.com", "https://api.example.com/", "https://api.example.com?", "https://api.example.com#", "https://api.example.com:", "https://api.example.com:0", "https://api.example.com:65536", "ftp://api.example.com"} {
		t.Run(raw, func(t *testing.T) {
			_, err := ParseOrigin(raw)
			require.Error(t, err)
		})
	}
	for _, allowed := range []string{"https://*.example.com", "http://api.example.com", "https://api.example.com:8443"} {
		_, err := CanonicalAccess([]string{allowed}, []Credential{credential}, nil)
		require.ErrorContains(t, err, "not explicitly allowed", "credential must require its exact origin")
	}
	_, err = CanonicalAccess([]string{"http://api.example.com:443", "https://api.example.com"}, nil, nil)
	require.ErrorContains(t, err, "both HTTP and HTTPS")
	var origins []string
	for i := range 257 {
		origins = append(origins, fmt.Sprintf("https://api%d.example.com", i))
	}
	_, err = CanonicalAccess(origins, nil, nil)
	require.ErrorContains(t, err, "256 rules")
}

func TestCredentialsIsolatePortsAndRejectProtectedHeaders(t *testing.T) {
	one := Credential{Origin: "https://api.example.com", Header: "Authorization", URI: "ate-secret://k8s.io/default/team/auth/token"}
	two := one
	two.Origin, two.URI = "https://api.example.com:8443", "ate-secret://k8s.io/default/team/other/token"
	bindings, err := CanonicalCredentials([]Credential{one, two})
	require.NoError(t, err)
	require.Len(t, bindings, 2)
	for _, header := range []string{"hOsT", "Content-Length", "Connection", "Transfer-Encoding", "Cookie", "Proxy-Authorization"} {
		one.Header = header
		_, err := CanonicalCredentials([]Credential{one})
		require.Error(t, err)
	}
}

func TestOrigin(t *testing.T) {
	for _, tt := range []struct{ raw, want string }{
		{"https://API.Example.com./v1?key=private#fragment", "https://api.example.com:443"},
		{"http://user:password@model.example:8080/v1", "http://model.example:8080"},
		{"https://model.example:8443/v1", "https://model.example:8443"},
		{"http://collector/v1/traces", "http://collector:80"},
		{"http://[2001:db8::1]:4317", "http://[2001:db8::1]:4317"},
	} {
		t.Run(tt.raw, func(t *testing.T) {
			u, err := url.Parse(tt.raw)
			require.NoError(t, err)
			require.Equal(t, tt.want, Origin(u))
		})
	}
}
