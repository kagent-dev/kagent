package a2a

import (
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

const requestMetadataSessionKey = "com.example/session"

func TestMCPRequestMetadata(t *testing.T) {
	values, err := MCPRequestMetadata(map[string]any{
		MCPRequestMetadataExtensionURI: map[string]any{requestMetadataSessionKey: "a1", "format": "csv", "org.example/options": map[string]any{"arbitrary field": 1.0}},
		"other":                        "ignored",
	})
	require.NoError(t, err)
	require.Equal(t, map[string]any{requestMetadataSessionKey: "a1", "format": "csv", "org.example/options": map[string]any{"arbitrary field": 1.0}}, values)

	for name, metadata := range map[string]map[string]any{
		"absent": nil,
		"null":   {MCPRequestMetadataExtensionURI: nil},
		"empty":  {MCPRequestMetadataExtensionURI: map[string]any{}},
	} {
		values, err := MCPRequestMetadata(metadata)
		require.NoError(t, err, name)
		require.Nil(t, values, name)
	}

	for name, metadata := range map[string]map[string]any{
		"string": {MCPRequestMetadataExtensionURI: "a1"},
		"array":  {MCPRequestMetadataExtensionURI: []any{"a1"}},
		"number": {MCPRequestMetadataExtensionURI: 42},
		"bool":   {MCPRequestMetadataExtensionURI: true},
	} {
		_, err := MCPRequestMetadata(metadata)
		require.Error(t, err, name)
	}
}

func TestMCPRequestMetadataAcceptsCoreKeySyntax(t *testing.T) {
	for _, key := range []string{"", "session", "com.example/", "a/b", "com.ex-ample9/a-b_c.d", "io.modelcontextprotocol/ui"} {
		t.Run(key, func(t *testing.T) {
			values, err := MCPRequestMetadata(map[string]any{MCPRequestMetadataExtensionURI: map[string]any{key: map[string]any{"any nested key": true}}})
			require.NoError(t, err)
			require.Contains(t, values, key)
		})
	}
}

func TestMCPRequestMetadataRejectsMalformedAndClientOwnedKeys(t *testing.T) {
	for _, key := range []string{
		"/session", "com..example/session", "1example/session", "example-/session", "ex_ample/session", "com.example/-session", "com.example/session_", "com.example/a/b", "com.example/a b",
		mcpsdk.MetaKeyProtocolVersion, mcpsdk.MetaKeyClientInfo, mcpsdk.MetaKeyClientCapabilities, "progressToken",
		mcpLogLevelMetadataKey, mcpsdk.MetaKeyServerInfo, mcpsdk.MetaKeySubscriptionID, "traceparent", "tracestate", "baggage",
	} {
		t.Run(key, func(t *testing.T) {
			values, err := MCPRequestMetadata(map[string]any{MCPRequestMetadataExtensionURI: map[string]any{key: "private-value"}})
			require.Error(t, err)
			require.Nil(t, values)
			require.NotContains(t, err.Error(), "private-value")
		})
	}
}
