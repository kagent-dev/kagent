package a2a

import (
	"context"
	"fmt"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

var mcpExtensionsAgentExtension = apia2a.MCPExtensionsExtension()

// MCPExtensionsActivationInterceptor activates the MCP extensions extension
// when the client requested it. A message whose values are malformed is refused
// with invalid params before anything runs.
func MCPExtensionsActivationInterceptor() a2asrv.CallInterceptor {
	return &mcpExtensionsActivationInterceptor{}
}

type mcpExtensionsActivationInterceptor struct {
	a2asrv.PassthroughCallInterceptor
}

func (*mcpExtensionsActivationInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
	if !extensionRequested(callCtx, apia2a.MCPExtensionsExtensionURI) {
		return ctx, nil, nil
	}
	if send, ok := req.Payload.(*a2atype.SendMessageRequest); ok && send != nil {
		if _, err := apia2a.MCPExtensionValues(send.Metadata); err != nil {
			return ctx, nil, fmt.Errorf("%w: %w", a2atype.ErrInvalidParams, err)
		}
	}
	callCtx.Extensions().Activate(&mcpExtensionsAgentExtension)
	return ctx, nil, nil
}

// mcpExtensionValues returns the values of an activated MCP extensions
// extension, or nil when the client did not activate it.
func mcpExtensionValues(ctx context.Context, metadata map[string]any) map[string]any {
	extensions, ok := a2asrv.ExtensionsFrom(ctx)
	if !ok || !extensions.Active(&mcpExtensionsAgentExtension) {
		return nil
	}
	values, err := apia2a.MCPExtensionValues(metadata)
	if err != nil {
		return nil
	}
	return values
}

// EnsureMCPExtensionsExtension declares the optional MCP extensions extension
// on the card. Every Go ADK agent can call MCP servers, so the declaration is
// unconditional.
func EnsureMCPExtensionsExtension(card *a2atype.AgentCard) {
	ensureExtension(card, apia2a.MCPExtensionsExtension())
}
