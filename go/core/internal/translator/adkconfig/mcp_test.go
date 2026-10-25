package adkconfig

import (
	"testing"

	"github.com/stretchr/testify/require"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
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

			err := addRemoteMCPServer(config, server, tools, true)
			require.NoError(t, err)
			test.assert(t, config)
		})
	}
}

func TestAddRemoteMCPServerTokenPassthrough(t *testing.T) {
	config := &configv1.ADKAgentConfig{}
	for _, protocol := range []v1alpha3.RemoteMCPServerProtocol{v1alpha3.RemoteMCPServerProtocolStreamableHttp, v1alpha3.RemoteMCPServerProtocolSse} {
		server := &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{
			URL: "https://mcp.example.test", Protocol: protocol, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}},
		}}
		require.NoError(t, addRemoteMCPServer(config, server, nil, false))
	}
	require.Equal(t, []*configv1.InvocationCredentialHeader{{Name: "Authorization", Prefix: "Bearer "}}, config.HttpTools[0].PassthroughHeaders)
	require.Equal(t, []*configv1.InvocationCredentialHeader{{Name: "Authorization", Prefix: "Bearer "}}, config.SseTools[0].PassthroughHeaders)
}
