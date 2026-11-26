package a2a

import (
	"context"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

func mcpExtensionsCall(t *testing.T, requested bool) (context.Context, *a2asrv.CallContext) {
	t.Helper()
	params := map[string][]string{}
	if requested {
		params[a2atype.SvcParamExtensions] = []string{apia2a.MCPExtensionsExtensionURI}
	}
	return a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(params))
}

func sendWith(metadata map[string]any) *a2asrv.Request {
	return &a2asrv.Request{Payload: &a2atype.SendMessageRequest{
		Message:  a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
		Metadata: metadata,
	}}
}

func TestMCPExtensionValuesNeedActivation(t *testing.T) {
	metadata := map[string]any{apia2a.MCPExtensionsExtensionURI: map[string]any{"com.example/session": "a1"}}

	ctx, callCtx := mcpExtensionsCall(t, true)
	_, _, err := MCPExtensionsActivationInterceptor().Before(ctx, callCtx, sendWith(metadata))
	require.NoError(t, err)
	require.Contains(t, callCtx.Extensions().ActivatedURIs(), apia2a.MCPExtensionsExtensionURI, "activated, so the transport echoes it")
	require.Equal(t, map[string]any{"com.example/session": "a1"}, mcpExtensionValues(ctx, metadata))

	ctx, callCtx = mcpExtensionsCall(t, false)
	_, _, err = MCPExtensionsActivationInterceptor().Before(ctx, callCtx, sendWith(metadata))
	require.NoError(t, err)
	require.Nil(t, mcpExtensionValues(ctx, metadata), "values of an extension the client did not activate are ignored")
}

func TestMCPExtensionsRefusesMalformedValuesBeforeRunning(t *testing.T) {
	for name, values := range map[string]any{
		"not an object":      "a1",
		"key without prefix": map[string]any{"session": "a1"},
		"reserved prefix":    map[string]any{"io.modelcontextprotocol/clientInfo": "x"},
	} {
		ctx, callCtx := mcpExtensionsCall(t, true)
		_, _, err := MCPExtensionsActivationInterceptor().Before(ctx, callCtx, sendWith(map[string]any{apia2a.MCPExtensionsExtensionURI: values}))
		require.ErrorIs(t, err, a2atype.ErrInvalidParams, name)
		require.Empty(t, callCtx.Extensions().ActivatedURIs(), name)
	}
}

func TestCardDeclaresMCPExtensionsExtension(t *testing.T) {
	card := &a2atype.AgentCard{}
	EnsureMCPExtensionsExtension(card)
	EnsureMCPExtensionsExtension(card)
	require.Equal(t, []a2atype.AgentExtension{apia2a.MCPExtensionsExtension()}, card.Capabilities.Extensions)
}
