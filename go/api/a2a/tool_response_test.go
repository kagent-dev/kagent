package a2a

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestMCPToolResponse(t *testing.T) {
	for _, test := range []struct {
		name   string
		input  any
		failed bool
		want   string
	}{
		{"structured result", &mcp.CallToolResult{StructuredContent: map[string]any{"ready": true}, Content: []mcp.Content{&mcp.TextContent{Text: "fallback"}}}, false, `{"output":{"ready":true}}`},
		{"null structured result", map[string]any{"structuredContent": nil, "content": []any{map[string]any{"type": "text", "text": "namespace active"}}}, false, `{"output":"namespace active"}`},
		{"text blocks", []any{map[string]any{"type": "text", "text": "one"}, map[string]any{"type": "text", "text": "two"}}, false, `{"output":"one\ntwo"}`},
		{"MCP failure", &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "not found"}}}, false, `{"error":"not found"}`},
		{"runtime failure", "connection refused", true, `{"error":"connection refused"}`},
		{"image", map[string]any{"content": []any{map[string]any{"type": "image", "data": "image", "mimeType": "image/png"}}}, false, `{"output":{"content":[{"type":"image","data":"image","mimeType":"image/png"}]}}`},
		{"empty content", map[string]any{"content": []any{}}, false, `{"output":{"content":[]}}`},
		{"false", false, false, `{"output":false}`},
		{"zero", 0, false, `{"output":0}`},
		{"null", nil, false, `{"output":null}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			response, err := NewMCPToolResponse(test.input, test.failed)
			require.NoError(t, err)
			encoded, err := protojson.Marshal(response)
			require.NoError(t, err)
			require.JSONEq(t, test.want, string(encoded))
		})
	}
}

func TestToolResponsePreservesApplicationFields(t *testing.T) {
	response, err := NewToolResponse(map[string]any{"output": false, "error": "an application field"}, false)
	require.NoError(t, err)
	encoded, err := protojson.Marshal(response)
	require.NoError(t, err)
	require.JSONEq(t, `{"output":{"output":false,"error":"an application field"}}`, string(encoded))
	_, err = NewToolResponse(make(chan string), false)
	require.Error(t, err)
}
