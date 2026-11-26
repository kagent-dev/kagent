package a2a

import (
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"
)

const requestMetadataSessionKey = "com.example/session"

func TestRequestMetadataNeedsNoActivation(t *testing.T) {
	ctx, callContext := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(nil))
	metadata := map[string]any{requestMetadataSessionKey: "request-only", "org.example/options": map[string]any{"any nested key": true}}
	_, _, err := MCPRequestMetadataInterceptor().Before(ctx, callContext, &a2asrv.Request{Payload: &a2atype.SendMessageRequest{Metadata: metadata}})
	require.NoError(t, err)
	require.Empty(t, callContext.Extensions().ActivatedURIs())
	require.Len(t, metadata, 2)
}

func TestRequestMetadataRefusesInvalidNamesBeforeExecution(t *testing.T) {
	for _, key := range []string{"not valid/key", "progressToken", "io.modelcontextprotocol/clientCapabilities"} {
		ctx, callContext := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(nil))
		_, _, err := MCPRequestMetadataInterceptor().Before(ctx, callContext, &a2asrv.Request{Payload: &a2atype.SendMessageRequest{Metadata: map[string]any{key: "private-value"}}})
		require.ErrorIs(t, err, a2atype.ErrInvalidParams)
		require.Empty(t, callContext.Extensions().ActivatedURIs())
	}
}
