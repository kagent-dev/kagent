package a2a

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const toolCallMetadataSessionKey = "com.example/session"

func TestMCPToolCallMetadata(t *testing.T) {
	values, err := MCPToolCallMetadata(map[string]any{
		MCPToolCallMetadataExtensionURI: map[string]any{toolCallMetadataSessionKey: "a1", "format": "csv", "io.modelcontextprotocol/clientInfo": map[string]any{"n": 1.0}},
		"other":                         "ignored",
	})
	require.NoError(t, err)
	require.Equal(t, map[string]any{toolCallMetadataSessionKey: "a1", "format": "csv", "io.modelcontextprotocol/clientInfo": map[string]any{"n": 1.0}}, values)

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
		"string": {MCPToolCallMetadataExtensionURI: "a1"},
		"array":  {MCPToolCallMetadataExtensionURI: []any{"a1"}},
		"number": {MCPToolCallMetadataExtensionURI: 42},
		"bool":   {MCPToolCallMetadataExtensionURI: true},
	} {
		_, err := MCPToolCallMetadata(metadata)
		require.Error(t, err, name)
	}
}
