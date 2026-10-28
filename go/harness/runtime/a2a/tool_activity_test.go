package a2a

import (
	"testing"

	"github.com/stretchr/testify/require"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

func TestMCPActivityUsesDiscoveredNamesAndArguments(t *testing.T) {
	for _, name := range []string{"tools.get__data.v2", "mcp__tools__get__data.v2"} {
		t.Run(name, func(t *testing.T) {
			args := map[string]any{"arguments": map[string]any{"limit": float64(0)}}
			part, err := toolCallPart(runtime.ToolCall{
				ID: "call", Name: name, MCPServer: "tools", MCPTool: "get__data.v2", Arguments: args,
			})
			require.NoError(t, err)
			activity, ok := apia2a.ParseToolActivity(part)
			require.True(t, ok)
			require.Equal(t, "get__data.v2", activity.Name)
			require.Equal(t, args, activity.Args)

			part, err = toolResultPart(runtime.ToolResult{
				ID: "call", Name: name, MCPTool: "get__data.v2",
				Result: map[string]any{"structuredContent": map[string]any{"result": false}},
			})
			require.NoError(t, err)
			activity, ok = apia2a.ParseToolActivity(part)
			require.True(t, ok)
			require.Equal(t, "get__data.v2", activity.Name)
			require.Equal(t, map[string]any{"result": false}, activity.Response.GetOutput().AsInterface())
		})
	}
}
