package a2a

import (
	"context"
	"fmt"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

var mcpRequestMetadataAgentExtension = apia2a.MCPRequestMetadataExtension()

// MCPRequestMetadataActivationInterceptor validates and activates requested
// MCP request metadata before dispatch.
func MCPRequestMetadataActivationInterceptor() a2asrv.CallInterceptor {
	return &mcpRequestMetadataActivationInterceptor{}
}

type mcpRequestMetadataActivationInterceptor struct {
	a2asrv.PassthroughCallInterceptor
}

var _ a2asrv.CallInterceptor = (*mcpRequestMetadataActivationInterceptor)(nil)

func (*mcpRequestMetadataActivationInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, request *a2asrv.Request) (context.Context, any, error) {
	if !extensionRequested(callCtx, apia2a.MCPRequestMetadataExtensionURI) {
		return ctx, nil, nil
	}
	if send, ok := request.Payload.(*a2atype.SendMessageRequest); ok && send != nil {
		if _, err := apia2a.MCPRequestMetadata(send.Metadata); err != nil {
			return ctx, nil, fmt.Errorf("%w: %w", a2atype.ErrInvalidParams, err)
		}
	}
	callCtx.Extensions().Activate(&mcpRequestMetadataAgentExtension)
	return ctx, nil, nil
}

// mcpRequestMetadata reads only activated MCP request metadata.
func mcpRequestMetadata(ctx context.Context, metadata map[string]any) map[string]any {
	extensions, ok := a2asrv.ExtensionsFrom(ctx)
	if !ok || !extensions.Active(&mcpRequestMetadataAgentExtension) {
		return nil
	}
	values, err := apia2a.MCPRequestMetadata(metadata)
	if err != nil {
		return nil
	}
	return values
}

// EnsureMCPRequestMetadataExtension declares the Go ADK's optional A2A
// request metadata bridge.
func EnsureMCPRequestMetadataExtension(card *a2atype.AgentCard) {
	ensureExtension(card, apia2a.MCPRequestMetadataExtension())
}
