package a2a

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsMCPExtensionID(t *testing.T) {
	for id, want := range map[string]bool{
		"com.example/session":          true,
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
		require.Equal(t, want, IsMCPExtensionID(id), id)
	}
}

func TestMCPExtensionValues(t *testing.T) {
	values, err := MCPExtensionValues(map[string]any{
		MCPExtensionsExtensionURI: map[string]any{"com.example/session": "a1", "com.example/config": map[string]any{"n": 1.0}},
		"other":                   "ignored",
	})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"com.example/session": "a1", "com.example/config": map[string]any{"n": 1.0}}, values)

	for name, metadata := range map[string]map[string]any{
		"absent": nil,
		"null":   {MCPExtensionsExtensionURI: nil},
		"empty":  {MCPExtensionsExtensionURI: map[string]any{}},
	} {
		values, err := MCPExtensionValues(metadata)
		require.NoError(t, err, name)
		require.Nil(t, values, name)
	}

	for name, metadata := range map[string]map[string]any{
		"not an object":      {MCPExtensionsExtensionURI: "a1"},
		"key without prefix": {MCPExtensionsExtensionURI: map[string]any{"session": "a1"}},
		"reserved prefix":    {MCPExtensionsExtensionURI: map[string]any{"io.modelcontextprotocol/clientInfo": "x"}},
	} {
		_, err := MCPExtensionValues(metadata)
		require.Error(t, err, name)
	}
}
