package translator

import (
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/pkg/egress"
)

func (c *Compiler) addRemoteMCPServer(config *adk.AgentConfig, runtime *modelRuntime, server *v1alpha3.RemoteMCPServer, tool *v1alpha3.McpServerTool, headers map[string]string) error {
	targetURL := server.Spec.URL
	if c.mcpEgressPlaintext {
		targetURL = egress.RewriteURL(server)
	}

	switch server.Spec.Protocol {
	case v1alpha3.RemoteMCPServerProtocolSse:
		params := adk.SseConnectionParams{Url: targetURL, Headers: headers}
		if server.Spec.Timeout != nil {
			params.Timeout = new(server.Spec.Timeout.Seconds())
		}
		if server.Spec.SseReadTimeout != nil {
			params.SseReadTimeout = new(server.Spec.SseReadTimeout.Seconds())
		}
		params.TLSInsecureSkipVerify, params.TLSCACertPath, params.TLSDisableSystemCAs = deriveTLSFields(server.Spec.TLS)
		config.SseTools = append(config.SseTools, adk.SseMcpServerConfig{
			Params: params, Tools: tool.ToolNames, AllowedHeaders: tool.AllowedHeaders, RequireApproval: tool.RequireApproval,
		})
	default:
		params := adk.StreamableHTTPConnectionParams{Url: targetURL, Headers: headers}
		if server.Spec.Timeout != nil {
			params.Timeout = new(server.Spec.Timeout.Seconds())
		}
		if server.Spec.SseReadTimeout != nil {
			params.SseReadTimeout = new(server.Spec.SseReadTimeout.Seconds())
		}
		if server.Spec.TerminateOnClose != nil {
			params.TerminateOnClose = server.Spec.TerminateOnClose
		}
		params.TLSInsecureSkipVerify, params.TLSCACertPath, params.TLSDisableSystemCAs = deriveTLSFields(server.Spec.TLS)
		config.HttpTools = append(config.HttpTools, adk.HttpMcpServerConfig{
			Params: params, Tools: tool.ToolNames, AllowedHeaders: tool.AllowedHeaders, RequireApproval: tool.RequireApproval,
		})
	}

	addTLSConfiguration(runtime.data, server.Spec.TLS)
	runtime.HasUnsupportedVolumes = len(runtime.data.Volumes) > 0 || len(runtime.data.VolumeMounts) > 0
	return nil
}
