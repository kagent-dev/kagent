package translator

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestMCPPassthroughCredentialCompilation(t *testing.T) {
	secret := func(name string) v1alpha3.ValueRef {
		return v1alpha3.ValueRef{Name: name, ValueFrom: &v1alpha3.ValueSource{
			Type: v1alpha3.SecretValueSource, Name: "auth", Key: "token",
		}}
	}
	for _, tc := range []struct {
		name         string
		enabled      bool
		headers      []v1alpha3.ValueRef
		model        v1alpha3.ModelConfigSpec
		wantError    bool
		wantBindings int
	}{
		{name: "discovery secret omitted", enabled: true, headers: []v1alpha3.ValueRef{secret("aUtHoRiZaTiOn")}},
		{name: "disabled keeps secret", headers: []v1alpha3.ValueRef{secret("Authorization")}, wantBindings: 1},
		{name: "own other secret conflicts", enabled: true, headers: []v1alpha3.ValueRef{secret("X-Key")}, wantError: true},
		{name: "static model conflicts on normalized origin", enabled: true,
			model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, APIKeySecret: "model", APIKeySecretKey: "key",
				OpenAI: &v1alpha3.OpenAIConfig{BaseURL: "https://TOOLS.EXAMPLE.COM.:443/v1"}}, wantError: true},
		{name: "static model on different origin allowed", enabled: true,
			model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, APIKeySecret: "model", APIKeySecretKey: "key",
				OpenAI: &v1alpha3.OpenAIConfig{BaseURL: "http://TOOLS.EXAMPLE.COM.:8080/v1"}}, wantBindings: 1},
		{name: "model and MCP passthrough can share origin", enabled: true,
			model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, TokenPassthrough: true,
				OpenAI: &v1alpha3.OpenAIConfig{BaseURL: "https://TOOLS.EXAMPLE.COM.:443/v1"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Namespace: "team"},
				Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://tools.example.com/mcp",
					TokenPassthrough: new(tc.enabled), HeadersFrom: tc.headers}}
			input := credentialInput(tc.model)
			input.Root.MCPTools = []ResolvedMCPTool{{Server: server}}
			_, bindings, origins, err := CompileCredentials(input, nil, nil)
			if tc.wantError {
				require.ErrorContains(t, err, "cannot combine caller-token passthrough")
			} else {
				require.NoError(t, err)
				require.Len(t, bindings, tc.wantBindings)
				if tc.enabled {
					require.Contains(t, origins, "https://tools.example.com:443")
				} else {
					require.Empty(t, origins)
				}
			}
			require.Equal(t, tc.headers, server.Spec.HeadersFrom)
		})
	}
}

func TestRuntimeMCPHeadersFiltersBeforeResolution(t *testing.T) {
	server := &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{
		TokenPassthrough: new(true),
		HeadersFrom: []v1alpha3.ValueRef{
			{Name: "Authorization", Value: "discovery"},
			{Name: "authorization", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.ConfigMapValueSource, Name: "absent", Key: "key"}},
			{Name: "X-Tenant", Value: "tenant"},
		},
	}}
	require.Equal(t, []v1alpha3.ValueRef{{Name: "X-Tenant", Value: "tenant"}}, RuntimeMCPHeaders(server))
	server.Spec.TokenPassthrough = nil
	require.Equal(t, server.Spec.HeadersFrom, RuntimeMCPHeaders(server))
}

func TestMCPPassthroughConflictsWithOtherServer(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{})
	passthrough := &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{
		URL: "https://TOOLS.EXAMPLE.COM.:8443/mcp", TokenPassthrough: new(true),
	}}
	static := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Namespace: "team"}, Spec: v1alpha3.RemoteMCPServerSpec{
		URL: "https://tools.example.com:8443/other", HeadersFrom: []v1alpha3.ValueRef{{
			Name: "Authorization", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.SecretValueSource, Name: "auth", Key: "token"},
		}},
	}}
	input.Root.MCPTools = []ResolvedMCPTool{{Server: passthrough}, {Server: static}}
	environment, bindings, origins, err := CompileCredentials(input, nil, nil)
	require.ErrorContains(t, err, "cannot combine caller-token passthrough")
	require.Nil(t, environment)
	require.Nil(t, bindings)
	require.Nil(t, origins)
	static.Spec.TokenPassthrough = new(true)
	_, bindings, origins, err = CompileCredentials(input, nil, nil)
	require.NoError(t, err)
	require.Empty(t, bindings, "multiple passthrough servers may share an origin")
	require.Contains(t, origins, "https://tools.example.com:8443")
}
