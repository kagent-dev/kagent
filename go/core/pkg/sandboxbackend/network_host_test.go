package sandboxbackend

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeAllowedDomainHost(t *testing.T) {
	tests := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"api.openai.com", "api.openai.com", true},
		{"  *.anthropic.com  ", "*.anthropic.com", true},
		{"https://api.telegram.org/bot", "api.telegram.org", true},
		{"http://example.com:8080/path", "example.com", true},
		{"host.only:443", "host.only", true},
		{"", "", false},
		{"https://", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, ok := NormalizeAllowedDomainHost(tt.raw)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}
