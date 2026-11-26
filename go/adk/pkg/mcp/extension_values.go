package mcp

import (
	"context"
	"maps"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type extensionValuesKey struct{}

// WithExtensionValues returns a context carrying the values, keyed by MCP
// extension id, that the turn's MCP tool calls send as `_meta`. It always
// replaces what ctx carried.
func WithExtensionValues(ctx context.Context, values map[string]any) context.Context {
	return context.WithValue(ctx, extensionValuesKey{}, maps.Clone(values))
}

func extensionValuesFrom(ctx context.Context) map[string]any {
	values, _ := ctx.Value(extensionValuesKey{}).(map[string]any)
	return values
}

// sendDeclaredExtensionValues adds the turn's extension values to the `_meta`
// of each tools/call, each only when the server declared that extension in its
// capabilities. A key the call already carries is left as it is.
func sendDeclaredExtensionValues(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
	return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
		values := extensionValuesFrom(ctx)
		if method != "tools/call" || len(values) == 0 {
			return next(ctx, method, req)
		}
		params, ok := req.GetParams().(*mcpsdk.CallToolParams)
		session, isClient := req.GetSession().(*mcpsdk.ClientSession)
		if !ok || params == nil || !isClient {
			return next(ctx, method, req)
		}
		declared := serverExtensions(session)
		for id, value := range values {
			if _, ok := declared[id]; !ok {
				continue
			}
			if params.Meta == nil {
				params.Meta = mcpsdk.Meta{}
			}
			if _, set := params.Meta[id]; !set {
				params.Meta[id] = value
			}
		}
		return next(ctx, method, req)
	}
}

func serverExtensions(session *mcpsdk.ClientSession) map[string]any {
	result := session.InitializeResult()
	if result == nil || result.Capabilities == nil {
		return nil
	}
	return result.Capabilities.Extensions
}
