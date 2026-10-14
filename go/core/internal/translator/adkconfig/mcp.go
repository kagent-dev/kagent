package adkconfig

import (
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

// ApplyCredentialHeaders adds gateway placeholders to the root and shared MCP clients.
func ApplyCredentialHeaders(config *configv1.ADKAgentConfig, additions []egress.Credential) error {
	for i := range config.HttpTools {
		params := config.HttpTools[i].Params
		var err error
		params.Headers, err = translator.CredentialHeaders(params.Url, params.Headers, additions)
		if err != nil {
			return err
		}
	}
	for i := range config.SseTools {
		params := config.SseTools[i].Params
		var err error
		params.Headers, err = translator.CredentialHeaders(params.Url, params.Headers, additions)
		if err != nil {
			return err
		}
	}
	for _, child := range config.SubAgents {
		if err := ApplyCredentialHeaders(child, additions); err != nil {
			return err
		}
	}
	return nil
}

// addRemoteMCPServer translates the two remote protocols supported by the ADK.
// This path intentionally has no proxy URL or egress-gateway indirection.
func (c *Builder) addRemoteMCPServer(config *configv1.ADKAgentConfig, server *v1alpha3.RemoteMCPServer, tools []string, requireApproval bool, headers map[string]string) error {
	targetURL := server.Spec.URL

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
		})
	}

	return nil
}
