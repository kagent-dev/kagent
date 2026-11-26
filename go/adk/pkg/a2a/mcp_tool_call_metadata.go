package a2a

import (
	"context"
	"fmt"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

var mcpToolCallMetadataAgentExtension = apia2a.MCPToolCallMetadataExtension()

// MCPToolCallMetadataActivationInterceptor validates and activates requested
// tool-call metadata before dispatch.
func MCPToolCallMetadataActivationInterceptor() a2asrv.CallInterceptor {
	return &mcpToolCallMetadataActivationInterceptor{}
}

type mcpToolCallMetadataActivationInterceptor struct {
	a2asrv.PassthroughCallInterceptor
}

var _ a2asrv.CallInterceptor = (*mcpToolCallMetadataActivationInterceptor)(nil)

func (*mcpToolCallMetadataActivationInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, request *a2asrv.Request) (context.Context, any, error) {
	if !extensionRequested(callCtx, apia2a.MCPToolCallMetadataExtensionURI) {
		return ctx, nil, nil
	}
	if send, ok := request.Payload.(*a2atype.SendMessageRequest); ok && send != nil {
		if _, err := apia2a.MCPToolCallMetadata(send.Metadata); err != nil {
			return ctx, nil, fmt.Errorf("%w: %w", a2atype.ErrInvalidParams, err)
		}
	}
	callCtx.Extensions().Activate(&mcpToolCallMetadataAgentExtension)
	return ctx, nil, nil
}

// mcpToolCallMetadata reads only activated tool-call metadata.
func mcpToolCallMetadata(ctx context.Context, metadata map[string]any) map[string]any {
	extensions, ok := a2asrv.ExtensionsFrom(ctx)
	if !ok || !extensions.Active(&mcpToolCallMetadataAgentExtension) {
		return nil
	}
	values, err := apia2a.MCPToolCallMetadata(metadata)
	if err != nil {
		return nil
	}
	return values
}

// EnsureMCPToolCallMetadataExtension declares the Go ADK's optional tool-call
// metadata capability.
func EnsureMCPToolCallMetadataExtension(card *a2atype.AgentCard) {
	ensureExtension(card, apia2a.MCPToolCallMetadataExtension())
}
