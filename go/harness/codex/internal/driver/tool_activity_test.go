package driver

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMCPActivityPreservesNativeValues(t *testing.T) {
	for _, test := range []struct {
		name, response string
		want           any
		failed         bool
	}{
		{"output", `"result":{"structuredContent":{"result":false}}`, map[string]any{"structuredContent": map[string]any{"result": false}}, false},
		{"error", `"error":{"message":"unavailable"}`, map[string]any{"message": "unavailable"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			translator := newEventTranslator("thread", "turn")
			sink := &recordingSink{}
			for _, raw := range []string{
				`{"method":"item/started","params":{"threadId":"thread","turnId":"turn","item":{"type":"mcpToolCall","id":"call","server":"tools","tool":"get__data.v2","arguments":{"arguments":{"limit":0}},"status":"inProgress"}}}`,
				`{"method":"item/completed","params":{"threadId":"thread","turnId":"turn","item":{"type":"mcpToolCall","id":"call","server":"tools","tool":"get__data.v2","status":"completed",` + test.response + `}}}`,
			} {
				var message rpcMessage
				require.NoError(t, json.Unmarshal([]byte(raw), &message))
				_, _, err := translator.translate(message, sink)
				require.NoError(t, err)
			}
			require.Len(t, sink.calls, 1)
			require.Equal(t, "get__data.v2", sink.calls[0].MCPTool)
			require.Equal(t, map[string]any{"arguments": map[string]any{"limit": float64(0)}}, sink.calls[0].Arguments)
			require.Len(t, sink.results, 1)
			require.Equal(t, "get__data.v2", sink.results[0].MCPTool)
			require.Equal(t, test.failed, sink.results[0].IsError)
			require.Equal(t, test.want, sink.results[0].Result)
		})
	}
}
