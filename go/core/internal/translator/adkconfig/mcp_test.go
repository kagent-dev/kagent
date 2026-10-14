package adkconfig

import (
	"testing"

	"github.com/stretchr/testify/require"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

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
				require.Nil(t, config.HttpTools[0].Tools)
			},
		},
		{
			name:     "SSE with exposure filter",
			protocol: v1alpha3.RemoteMCPServerProtocolSse,
			assert: func(t *testing.T, config *configv1.ADKAgentConfig) {
				require.Len(t, config.SseTools, 1)
				require.True(t, config.SseTools[0].RequireApproval)
				require.Equal(t, []string{"write"}, config.SseTools[0].Tools)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := &configv1.ADKAgentConfig{}
			server := &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{
				URL: "https://mcp.example.test", Protocol: test.protocol,
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

func TestApplyCredentialHeadersToSharedAndSSEClients(t *testing.T) {
	config := &configv1.ADKAgentConfig{
		HttpTools: []*configv1.ADKHTTPMCPServerConfig{{Params: &configv1.ADKHTTPConnectionParams{Url: "https://tools.example.com/http", Headers: map[string]string{"X-Trace": "keep"}}}},
		SseTools:  []*configv1.ADKSSEMCPServerConfig{{Params: &configv1.ADKSSEConnectionParams{Url: "https://tools.example.com/sse"}}},
		SubAgents: []*configv1.ADKAgentConfig{{HttpTools: []*configv1.ADKHTTPMCPServerConfig{{Params: &configv1.ADKHTTPConnectionParams{Url: "https://tools.example.com/child"}}}}},
	}
	require.NoError(t, ApplyCredentialHeaders(config, []egress.Credential{{Origin: "https://tools.example.com:443", Header: "authorization", URI: "ate-secret://provider.example/token"}}))
	require.Equal(t, "keep", config.HttpTools[0].Params.Headers["X-Trace"])
	require.Equal(t, translator.CredentialPlaceholder, config.HttpTools[0].Params.Headers["authorization"])
	require.Equal(t, translator.CredentialPlaceholder, config.SseTools[0].Params.Headers["authorization"])
	require.Equal(t, translator.CredentialPlaceholder, config.SubAgents[0].HttpTools[0].Params.Headers["authorization"])
}
