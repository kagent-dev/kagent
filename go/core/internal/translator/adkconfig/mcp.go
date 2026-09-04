package adkconfig

import (
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// addRemoteMCPServer translates the two remote protocols supported by the ADK.
// This path intentionally has no proxy URL or egress-gateway indirection.
func (c *Builder) addRemoteMCPServer(config *adk.AgentConfig, runtime *modelRuntime, server *v1alpha3.RemoteMCPServer, tools []string, headers map[string]string) error {
	targetURL := server.Spec.URL

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
			Params: params, Tools: tools,
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
			Params: params, Tools: tools,
		})
	}

	// Reuse the model deployment accumulator so TLS assets from every MCP server
	// are considered together when checking Substrate volume compatibility.
	addTLSConfiguration(runtime.data, server.Spec.TLS)
	runtime.HasUnsupportedVolumes = len(runtime.data.Volumes) > 0 || len(runtime.data.VolumeMounts) > 0
	return nil
}
