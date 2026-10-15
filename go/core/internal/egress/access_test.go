package egress

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalAccess(t *testing.T) {
	credential := Credential{Hostname: "API.example.com", Header: "Authorization", URI: "ate-secret://k8s.io/default/team/auth/token"}
	access, err := CanonicalAccess([]string{"https://*.example.com", "https://api.example.com:0443", "https://API.example.com.", "https://api.example.com:8443", "http://api.example.com"}, []Credential{credential, credential})
	require.NoError(t, err)
	require.Equal(t, []string{"http://api.example.com:80", "https://*.example.com:443", "https://api.example.com:443", "https://api.example.com:8443"}, access.Destinations)
	require.Equal(t, []Credential{{Hostname: "api.example.com", Header: "authorization", URI: credential.URI}}, access.Credentials)

	for _, allowed := range []string{"https://*.example.com", "http://api.example.com", "https://other.example.com"} {
		_, err := CanonicalAccess([]string{allowed}, []Credential{credential})
		require.ErrorContains(t, err, "not explicitly allowed", "a credential requires an exact HTTPS destination of its host")
	}
	_, err = CanonicalAccess([]string{"http://api.example.com:443", "https://api.example.com"}, nil)
	require.ErrorContains(t, err, "both HTTP and HTTPS")
	var origins []string
	for i := range 257 {
		origins = append(origins, fmt.Sprintf("https://api%d.example.com", i))
	}
	_, err = CanonicalAccess(origins, nil)
	require.ErrorContains(t, err, "256 rules")
}

func TestValidCredentialHeader(t *testing.T) {
	for _, header := range []string{"Authorization", "x-api-key"} {
		require.True(t, ValidCredentialHeader(header), header)
	}
	for _, header := range []string{"hOsT", "Content-Length", "Connection", "Transfer-Encoding", "Cookie", "Proxy-Authorization", "Bad\r\nHeader", ""} {
		require.False(t, ValidCredentialHeader(header), header)
	}
}
