package mcp

import (
	"context"
	"maps"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

type requestMetadataKey struct{}

// WithRequestMetadata replaces the MCP tool-call metadata carried by ctx,
// including when values is nil. The map is shallow-copied.
func WithRequestMetadata(ctx context.Context, values map[string]any) context.Context {
	return context.WithValue(ctx, requestMetadataKey{}, maps.Clone(values))
}

func requestMetadataFrom(ctx context.Context) map[string]any {
	values, _ := ctx.Value(requestMetadataKey{}).(map[string]any)
	return values
}

func sendRequestMetadata(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
	return func(ctx context.Context, method string, request mcpsdk.Request) (mcpsdk.Result, error) {
		values := requestMetadataFrom(ctx)
		if method != "tools/call" || len(values) == 0 {
			return next(ctx, method, request)
		}
		params, ok := request.GetParams().(*mcpsdk.CallToolParams)
		if !ok || params == nil {
			return next(ctx, method, request)
		}
		if err := apia2a.ValidateMCPRequestMetadata(values); err != nil {
			return nil, err
		}
		metadata := maps.Clone(params.Meta)
		if metadata == nil {
			metadata = mcpsdk.Meta{}
		}
		for key, value := range values {
			if _, present := metadata[key]; !present {
				metadata[key] = value
			}
		}
		params.Meta = metadata
		return next(ctx, method, request)
	}
}
