package a2a

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const toolCallMetadataSessionKey = "com.example/session"

func TestIsMCPToolCallMetadataKey(t *testing.T) {
	for key, want := range map[string]bool{
		toolCallMetadataSessionKey:     true,
		"io.github.app/session-ref.v2": true,
		"example/x":                    true,
		"session":                      false,
		"/session":                     false,
		"com.example/":                 false,
		"com.example/-session":         false,
		"com..example/session":         false,
		"com.1example/session":         false,
		"io.modelcontextprotocol/ui":   false,
		"dev.mcp/session":              false,
		"com.example.mcp/session":      true,
		"progressToken":                false,
		"traceparent":                  false,
	} {
		require.Equal(t, want, IsMCPToolCallMetadataKey(key), key)
	}
}

func TestMCPToolCallMetadata(t *testing.T) {
	values, err := MCPToolCallMetadata(map[string]any{
		MCPToolCallMetadataExtensionURI: map[string]any{toolCallMetadataSessionKey: "a1", "com.example/config": map[string]any{"n": 1.0}},
		"other":                         "ignored",
	})
	require.NoError(t, err)
	require.Equal(t, map[string]any{toolCallMetadataSessionKey: "a1", "com.example/config": map[string]any{"n": 1.0}}, values)

	for name, metadata := range map[string]map[string]any{
		"absent": nil,
		"null":   {MCPToolCallMetadataExtensionURI: nil},
		"empty":  {MCPToolCallMetadataExtensionURI: map[string]any{}},
	} {
		values, err := MCPToolCallMetadata(metadata)
		require.NoError(t, err, name)
		require.Nil(t, values, name)
	}

	for name, metadata := range map[string]map[string]any{
		"not an object":      {MCPToolCallMetadataExtensionURI: "a1"},
		"key without prefix": {MCPToolCallMetadataExtensionURI: map[string]any{"session": "a1"}},
		"reserved prefix":    {MCPToolCallMetadataExtensionURI: map[string]any{"io.modelcontextprotocol/clientInfo": "x"}},
	} {
		_, err := MCPToolCallMetadata(metadata)
		require.Error(t, err, name)
	}
}
