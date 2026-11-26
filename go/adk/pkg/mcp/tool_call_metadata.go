package mcp

import (
	"context"
	"maps"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

type toolCallMetadataKey struct{}

// WithToolCallMetadata replaces the request-scoped tool-call metadata carried
// by ctx, including when values is nil.
func WithToolCallMetadata(ctx context.Context, values map[string]any) context.Context {
	return context.WithValue(ctx, toolCallMetadataKey{}, maps.Clone(values))
}

func toolCallMetadataFrom(ctx context.Context) map[string]any {
	values, _ := ctx.Value(toolCallMetadataKey{}).(map[string]any)
	return values
}

// sendToolCallMetadata sends the request's payload to servers that declare
// the extension. Existing extension metadata takes precedence.
func sendToolCallMetadata(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
	return func(ctx context.Context, method string, request mcpsdk.Request) (mcpsdk.Result, error) {
		values := toolCallMetadataFrom(ctx)
		if method != "tools/call" || len(values) == 0 {
			return next(ctx, method, request)
		}
		params, ok := request.GetParams().(*mcpsdk.CallToolParams)
		session, isClient := request.GetSession().(*mcpsdk.ClientSession)
		if !ok || params == nil || !isClient {
			return next(ctx, method, request)
		}
		if _, set := params.Meta[apia2a.MCPToolCallMetadataExtensionID]; set {
			return next(ctx, method, request)
		}
		if !supportsToolCallMetadata(session) {
			return next(ctx, method, request)
		}
		if params.Meta == nil {
			params.Meta = mcpsdk.Meta{}
		}
		params.Meta[apia2a.MCPToolCallMetadataExtensionID] = maps.Clone(values)
		return next(ctx, method, request)
	}
}

func supportsToolCallMetadata(session *mcpsdk.ClientSession) bool {
	result := session.InitializeResult()
	if result == nil || result.Capabilities == nil {
		return false
	}
	settings, ok := result.Capabilities.Extensions[apia2a.MCPToolCallMetadataExtensionID].(map[string]any)
	return ok && settings != nil
}
