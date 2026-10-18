package codex

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestCompileMCPBearerPassthrough(t *testing.T) {
	for _, tt := range []struct {
		name        string
		passthrough *bool
		wantErr     bool
	}{
		{name: "enabled", passthrough: new(true), wantErr: true},
		{name: "disabled", passthrough: new(false)},
		{name: "absent"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := &v1alpha3.RemoteMCPServer{
				ObjectMeta: metav1.ObjectMeta{Name: "tools"},
				Spec: v1alpha3.RemoteMCPServerSpec{
					URL: "https://mcp.example.com", TokenPassthrough: tt.passthrough,
					HeadersFrom: []v1alpha3.ValueRef{
						{Name: "aUtHoRiZaTiOn", Value: "static-auth"},
						{Name: "X-Tenant", Value: "tenant"},
					},
				},
			}
			if tt.wantErr {
				// Reject before looking up discovery credentials.
				server.Spec.HeadersFrom = append(server.Spec.HeadersFrom, v1alpha3.ValueRef{
					Name: "Authorization", ValueFrom: &v1alpha3.ValueSource{
						Type: v1alpha3.ConfigMapValueSource, Name: "missing", Key: "token",
					},
				})
			}
			result, err := (&Compiler{}).compileMCP(t.Context(), "test", []translator.ResolvedMCPTool{{Server: server}})
			if tt.wantErr {
				var validationErr *translator.ValidationError
				require.ErrorAs(t, err, &validationErr)
				require.EqualError(t, err, `Codex does not support RemoteMCPServer "tools" tokenPassthrough`)
				require.Equal(t, mcpCompilation{}, result)
				return
			}
			require.NoError(t, err)
			require.Empty(t, result.environment)
			require.Equal(t, map[string]string{"aUtHoRiZaTiOn": "static-auth", "X-Tenant": "tenant"}, result.servers["tools"].Headers)
			require.Equal(t, server.Spec.URL, result.servers["tools"].URL)
		})
	}
}
