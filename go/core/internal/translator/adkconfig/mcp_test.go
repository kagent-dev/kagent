package adkconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestCompileMCPFiltersDiscoveryAuthorizationBeforeLookup(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled resolves headers", true: "enabled skips discovery credentials"}[enabled], func(t *testing.T) {
			server := &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{
				URL: "https://mcp.example.test", TokenPassthrough: new(enabled),
				HeadersFrom: []v1alpha3.ValueRef{
					{Name: "aUtHoRiZaTiOn", ValueFrom: &v1alpha3.ValueSource{
						Type: v1alpha3.ConfigMapValueSource, Name: "absent-discovery-config", Key: "token",
					}},
					{Name: "Authorization", ValueFrom: &v1alpha3.ValueSource{
						Type: v1alpha3.SecretValueSource, Name: "discovery-secret", Key: "token",
					}},
					{Name: "X-Tenant", Value: "team"},
				},
			}}
			builder := NewBuilder(krt.TestingDummyContext{}, contextTestCollections(t))
			result, err := builder.compileAgent(t.Context(), &translator.AgentInput{
				Template: &translator.TemplateConfiguration{},
				MCPTools: []translator.ResolvedMCPTool{{Server: server}},
			})
			if !enabled {
				require.ErrorContains(t, err, "absent-discovery-config")
				return
			}
			require.NoError(t, err)
			require.Len(t, result.Config.HttpTools, 1)
			require.Equal(t, map[string]string{"X-Tenant": "team"}, result.Config.HttpTools[0].Params.Headers)
			require.Empty(t, result.Environment)
		})
	}
}

func TestAddRemoteMCPServerPreservesBindingApproval(t *testing.T) {
	tests := []struct {
		name     string
		protocol v1alpha3.RemoteMCPServerProtocol
		assert   func(*testing.T, *configv1.ADKAgentConfig)
	}{
		{
			name:     "streamable HTTP",
			protocol: v1alpha3.RemoteMCPServerProtocolStreamableHttp,
			assert: func(t *testing.T, config *configv1.ADKAgentConfig) {
				require.Len(t, config.HttpTools, 1)
				require.True(t, config.HttpTools[0].RequireApproval)
				require.True(t, config.HttpTools[0].BearerTokenPassthrough)
				require.Nil(t, config.HttpTools[0].Tools)
			},
		},
		{
			name:     "SSE with exposure filter",
			protocol: v1alpha3.RemoteMCPServerProtocolSse,
			assert: func(t *testing.T, config *configv1.ADKAgentConfig) {
				require.Len(t, config.SseTools, 1)
				require.True(t, config.SseTools[0].RequireApproval)
				require.True(t, config.SseTools[0].BearerTokenPassthrough)
				require.Equal(t, []string{"write"}, config.SseTools[0].Tools)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := &configv1.ADKAgentConfig{}
			server := &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{
				URL: "https://mcp.example.test", Protocol: test.protocol,
				TokenPassthrough: new(true),
			}}
			var tools []string
			if test.protocol == v1alpha3.RemoteMCPServerProtocolSse {
				tools = []string{"write"}
			}

			err := (&Builder{}).addRemoteMCPServer(config, server, tools, true, nil)
			require.NoError(t, err)
			test.assert(t, config)
		})
	}
}
