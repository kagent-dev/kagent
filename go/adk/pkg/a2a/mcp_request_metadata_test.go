package a2a

import (
	"context"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

const requestMetadataSessionKey = "com.example/session"

func mcpRequestMetadataCall(t *testing.T, requested bool) (context.Context, *a2asrv.CallContext) {
	t.Helper()
	params := map[string][]string{}
	if requested {
		params[a2atype.SvcParamExtensions] = []string{apia2a.MCPRequestMetadataExtensionURI}
	}
	return a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(params))
}

func sendWith(metadata map[string]any) *a2asrv.Request {
	return &a2asrv.Request{Payload: &a2atype.SendMessageRequest{
		Message:  a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
		Metadata: metadata,
	}}
}

func TestMCPRequestMetadataNeedActivation(t *testing.T) {
	metadata := map[string]any{apia2a.MCPRequestMetadataExtensionURI: map[string]any{requestMetadataSessionKey: "a1"}}

	ctx, callCtx := mcpRequestMetadataCall(t, true)
	_, _, err := MCPRequestMetadataActivationInterceptor().Before(ctx, callCtx, sendWith(metadata))
	require.NoError(t, err)
	require.Contains(t, callCtx.Extensions().ActivatedURIs(), apia2a.MCPRequestMetadataExtensionURI, "activated, so the transport echoes it")
	require.Equal(t, map[string]any{requestMetadataSessionKey: "a1"}, mcpRequestMetadata(ctx, metadata))

	ctx, callCtx = mcpRequestMetadataCall(t, false)
	_, _, err = MCPRequestMetadataActivationInterceptor().Before(ctx, callCtx, sendWith(metadata))
	require.NoError(t, err)
	require.Nil(t, mcpRequestMetadata(ctx, metadata), "values of an extension the client did not activate are ignored")
}

func TestMCPRequestMetadataRefusesMalformedValuesBeforeRunning(t *testing.T) {
	for name, values := range map[string]any{
		"string": "a1",
		"array":  []any{"a1"},
		"number": 42,
		"bool":   true,
	} {
		ctx, callCtx := mcpRequestMetadataCall(t, true)
		_, _, err := MCPRequestMetadataActivationInterceptor().Before(ctx, callCtx, sendWith(map[string]any{apia2a.MCPRequestMetadataExtensionURI: values}))
		require.ErrorIs(t, err, a2atype.ErrInvalidParams, name)
		require.Empty(t, callCtx.Extensions().ActivatedURIs(), name)
	}
}

func TestCardDeclaresMCPRequestMetadataExtension(t *testing.T) {
	card := &a2atype.AgentCard{}
	EnsureMCPRequestMetadataExtension(card)
	EnsureMCPRequestMetadataExtension(card)
	require.Equal(t, []a2atype.AgentExtension{apia2a.MCPRequestMetadataExtension()}, card.Capabilities.Extensions)
}
