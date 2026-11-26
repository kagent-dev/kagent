package a2a

import (
	"context"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

const toolCallMetadataSessionKey = "com.example/session"

func mcpToolCallMetadataCall(t *testing.T, requested bool) (context.Context, *a2asrv.CallContext) {
	t.Helper()
	params := map[string][]string{}
	if requested {
		params[a2atype.SvcParamExtensions] = []string{apia2a.MCPToolCallMetadataExtensionURI}
	}
	return a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(params))
}

func sendWith(metadata map[string]any) *a2asrv.Request {
	return &a2asrv.Request{Payload: &a2atype.SendMessageRequest{
		Message:  a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
		Metadata: metadata,
	}}
}

func TestMCPToolCallMetadataNeedActivation(t *testing.T) {
	metadata := map[string]any{apia2a.MCPToolCallMetadataExtensionURI: map[string]any{toolCallMetadataSessionKey: "a1"}}

	ctx, callCtx := mcpToolCallMetadataCall(t, true)
	_, _, err := MCPToolCallMetadataActivationInterceptor().Before(ctx, callCtx, sendWith(metadata))
	require.NoError(t, err)
	require.Contains(t, callCtx.Extensions().ActivatedURIs(), apia2a.MCPToolCallMetadataExtensionURI, "activated, so the transport echoes it")
	require.Equal(t, map[string]any{toolCallMetadataSessionKey: "a1"}, mcpToolCallMetadata(ctx, metadata))

	ctx, callCtx = mcpToolCallMetadataCall(t, false)
	_, _, err = MCPToolCallMetadataActivationInterceptor().Before(ctx, callCtx, sendWith(metadata))
	require.NoError(t, err)
	require.Nil(t, mcpToolCallMetadata(ctx, metadata), "values of an extension the client did not activate are ignored")
}

func TestMCPToolCallMetadataRefusesMalformedValuesBeforeRunning(t *testing.T) {
	for name, values := range map[string]any{
		"not an object":      "a1",
		"key without prefix": map[string]any{"session": "a1"},
		"reserved prefix":    map[string]any{"io.modelcontextprotocol/clientInfo": "x"},
	} {
		ctx, callCtx := mcpToolCallMetadataCall(t, true)
		_, _, err := MCPToolCallMetadataActivationInterceptor().Before(ctx, callCtx, sendWith(map[string]any{apia2a.MCPToolCallMetadataExtensionURI: values}))
		require.ErrorIs(t, err, a2atype.ErrInvalidParams, name)
		require.Empty(t, callCtx.Extensions().ActivatedURIs(), name)
	}
}

func TestCardDeclaresMCPToolCallMetadataExtension(t *testing.T) {
	card := &a2atype.AgentCard{}
	EnsureMCPToolCallMetadataExtension(card)
	EnsureMCPToolCallMetadataExtension(card)
	require.Equal(t, []a2atype.AgentExtension{apia2a.MCPToolCallMetadataExtension()}, card.Capabilities.Extensions)
}
