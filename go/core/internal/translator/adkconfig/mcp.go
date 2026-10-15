package adkconfig

import (
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// addRemoteMCPServer translates the two remote protocols supported by the ADK.
// This path intentionally has no proxy URL or egress-gateway indirection.
func (c *Builder) addRemoteMCPServer(config *adk.AgentConfig, server *v1alpha3.RemoteMCPServer, tools []string, requireApproval bool, headers map[string]string) error {
	targetURL := server.Spec.URL

	switch server.Spec.Protocol {
	case v1alpha3.RemoteMCPServerProtocolSse:
		params := &adk.SseConnectionParams{Url: targetURL, Headers: headers}
		if server.Spec.Timeout != nil {
			params.Timeout = durationpb.New(server.Spec.Timeout.Duration)
		}
		if server.Spec.SseReadTimeout != nil {
			params.SseReadTimeout = durationpb.New(server.Spec.SseReadTimeout.Duration)
		}
		params.TlsInsecureSkipVerify = tlsInsecureSkipVerify(server.Spec.TLS)
		config.SseTools = append(config.SseTools, &adk.SseMcpServerConfig{
			Params: params, Tools: tools, RequireApproval: requireApproval,
		})
	default:
		params := &adk.StreamableHTTPConnectionParams{Url: targetURL, Headers: headers}
		if server.Spec.Timeout != nil {
			params.Timeout = durationpb.New(server.Spec.Timeout.Duration)
		}
		if server.Spec.SseReadTimeout != nil {
			params.SseReadTimeout = durationpb.New(server.Spec.SseReadTimeout.Duration)
		}
		if server.Spec.TerminateOnClose != nil {
			params.TerminateOnClose = server.Spec.TerminateOnClose
		}
		params.TlsInsecureSkipVerify = tlsInsecureSkipVerify(server.Spec.TLS)
		config.HttpTools = append(config.HttpTools, &adk.HttpMcpServerConfig{
			Params: params, Tools: tools, RequireApproval: requireApproval,
		})
	}

	return nil
}
