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
