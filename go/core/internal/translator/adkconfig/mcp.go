package adkconfig

import (
	"google.golang.org/protobuf/types/known/durationpb"
	"k8s.io/utils/ptr"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

// addRemoteMCPServer translates the two remote protocols supported by the ADK.
// This path intentionally has no proxy URL or egress-gateway indirection.
func (c *Builder) addRemoteMCPServer(config *configv1.ADKAgentConfig, server *v1alpha3.RemoteMCPServer, tools []string, requireApproval bool, headers map[string]string) error {
	targetURL := server.Spec.URL
	if server.Spec.ActorJWT != nil {
		headers = translator.MCPActorJWTHeaders(headers)
	}

	switch server.Spec.Protocol {
	case v1alpha3.RemoteMCPServerProtocolSse:
		params := &configv1.ADKSSEConnectionParams{Url: targetURL, Headers: headers}
		if server.Spec.Timeout != nil {
			params.Timeout = durationpb.New(server.Spec.Timeout.Duration)
		}
		if server.Spec.SseReadTimeout != nil {
			params.SseReadTimeout = durationpb.New(server.Spec.SseReadTimeout.Duration)
		}
		params.TlsInsecureSkipVerify = tlsInsecureSkipVerify(server.Spec.TLS)
		config.SseTools = append(config.SseTools, &configv1.ADKSSEMCPServerConfig{
			Params: params, Tools: tools, RequireApproval: requireApproval,
			TokenPassthrough: ptr.Deref(server.Spec.TokenPassthrough, false),
		})
	default:
		params := &configv1.ADKHTTPConnectionParams{Url: targetURL, Headers: headers}
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
		config.HttpTools = append(config.HttpTools, &configv1.ADKHTTPMCPServerConfig{
			Params: params, Tools: tools, RequireApproval: requireApproval,
			TokenPassthrough: ptr.Deref(server.Spec.TokenPassthrough, false),
		})
	}

	return nil
}
