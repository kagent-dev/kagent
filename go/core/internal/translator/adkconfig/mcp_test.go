package adkconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	"k8s.io/apimachinery/pkg/types"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/pkg/egressext"
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

// The extension runs before Build, including shared-agent and SSE clients.
type mcpPolicyTranslator struct{}

var _ egressext.PolicyTranslator = mcpPolicyTranslator{}

func (mcpPolicyTranslator) Translate(_ types.NamespacedName, draft egressext.PolicyDraft) (egressext.PolicyDraft, error) {
	draft.Credentials = append(draft.Credentials, egressext.Credential{Origin: "https://tools.example.com:443", Header: "authorization", URI: "ate-secret://provider.example/token"})
	return draft, nil
}

func TestBuildResolvedCredentialHeaders(t *testing.T) {
	server := func(path string, protocol v1alpha3.RemoteMCPServerProtocol) translator.ResolvedMCPTool {
		return translator.ResolvedMCPTool{Server: &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://tools.example.com/" + path, Protocol: protocol}}}
	}
	input := &translator.HarnessInput{
		Harness: &translator.HarnessConfiguration{Spec: v1alpha3.HarnessSpec{BYO: &v1alpha3.BYOHarness{}}},
		Root: &translator.AgentInput{Template: &translator.TemplateConfiguration{}, MCPTools: []translator.ResolvedMCPTool{server("http", v1alpha3.RemoteMCPServerProtocolStreamableHttp)}, Shared: []translator.AgentInputBinding{
			{Name: "child", Agent: &translator.AgentInput{Template: &translator.TemplateConfiguration{}, MCPTools: []translator.ResolvedMCPTool{server("sse", v1alpha3.RemoteMCPServerProtocolSse)}}},
		}},
	}
	var err error
	input.Policy, err = translator.ResolvePolicy(input, nil, mcpPolicyTranslator{})
	require.NoError(t, err)
	result, err := NewBuilder(krt.TestingDummyContext{}, translator.Collections{}).Build(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, translator.CredentialPlaceholder, result.Config.HttpTools[0].Params.Headers["authorization"])
	require.Equal(t, translator.CredentialPlaceholder, result.Config.SubAgents[0].SseTools[0].Params.Headers["authorization"])
}
