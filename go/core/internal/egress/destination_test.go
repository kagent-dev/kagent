package egress

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

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

func TestParseOrigin(t *testing.T) {
	for _, tt := range []struct{ raw, want string }{
		{"https://proxy.golang.org", "https://proxy.golang.org:443"},
		{"https://Proxy.Golang.org.", "https://proxy.golang.org:443"},
		{"http://mirror.internal", "http://mirror.internal:80"},
		{"https://git.internal:8443", "https://git.internal:8443"},
		{"https://*.githubusercontent.com", "https://*.githubusercontent.com:443"},
	} {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := ParseOrigin(tt.raw)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
	for _, raw := range []string{
		"proxy.golang.org", "ftp://proxy.golang.org", "https://", "https://*", "https://*.",
		"https://a.*.example.com", "https://*.*.example.com", "https://*github.com", "https://*.com", "https://-bad.example.com",
		"https://192.0.2.1", "https://[2001:db8::1]", "https://proxy.golang.org/", "https://proxy.golang.org/path",
		"https://proxy.golang.org?x=1", "https://proxy.golang.org?", "https://proxy.golang.org#f",
		"https://user@proxy.golang.org", "https://proxy.golang.org:0", "https://proxy.golang.org:70000",
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := ParseOrigin(raw)
			require.Error(t, err)
		})
	}
}
