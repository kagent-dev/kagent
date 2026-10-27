package driver

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kagent-dev/kagent/go/harness/claude/config"
)

func TestSelectionAllowsReadsAndDeniesUnknownToolsWithoutApproval(t *testing.T) {
	broker, err := NewApprovalBroker(map[string]config.MCPServer{
		"catalog":         {Tools: []string{"lookup"}},
		"changes":         {Tools: []string{"apply"}, RequireApproval: true},
		"catalog__nested": {},
	}, t.TempDir(), 4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = broker.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "selection-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint: broker.URL(), HTTPClient: &http.Client{Transport: &headerTransport{headers: broker.Headers(), base: http.DefaultTransport}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	for _, test := range []struct{ name, behavior string }{
		{"mcp__catalog__lookup", "allow"},
		{"mcp__catalog__delete", "deny"},
		{"mcp__catalog__added_later", "deny"},
		{"mcp__changes__delete", "deny"},
		{"mcp__unknown__lookup", "deny"},
		{"mcp__catalog__nested__lookup", "allow"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
				Name: ApprovalToolName, Arguments: map[string]any{"tool_name": test.name, "input": map[string]any{"value": 7}, "tool_use_id": test.name},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError || len(result.Content) != 1 {
				t.Fatalf("permission result = %#v", result)
			}
			text, ok := result.Content[0].(*mcp.TextContent)
			if !ok {
				t.Fatalf("permission content = %#v", result.Content[0])
			}
			var output permissionPromptOutput
			if err := json.Unmarshal([]byte(text.Text), &output); err != nil {
				t.Fatal(err)
			}
			if output.Behavior != test.behavior {
				t.Fatalf("behavior = %q, want %q", output.Behavior, test.behavior)
			}
			if output.Behavior == "allow" && output.UpdatedInput["value"] != float64(7) {
				t.Fatalf("allowed input = %#v", output.UpdatedInput)
			}
		})
	}
}

func TestSelectionHookGuardsEveryDiscoveredTool(t *testing.T) {
	broker, err := NewApprovalBroker(map[string]config.MCPServer{
		"catalog": {Tools: []string{"lookup"}},
		"changes": {Tools: []string{"apply"}, RequireApproval: true},
	}, t.TempDir(), 4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = broker.Close() })
	for _, test := range []struct{ tool, decision string }{
		{"mcp__catalog__lookup", ""}, {"mcp__catalog__added_later", "deny"},
		{"mcp__changes__apply", "defer"}, {"mcp__changes__delete", "deny"},
	} {
		t.Run(test.tool, func(t *testing.T) {
			actual := postHook(t, broker, hookInput{SessionID: "session", Event: "PreToolUse", ToolID: test.tool, ToolName: test.tool}).Specific.Decision
			if actual != test.decision {
				t.Fatalf("decision = %q, want %q", actual, test.decision)
			}
		})
	}
}
