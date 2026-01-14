package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	mcp_client "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// mcpEndpointURL returns the URL for the MCP endpoint
func mcpEndpointURL() string {
	kagentURL := os.Getenv("KAGENT_URL")
	if kagentURL == "" {
		// if running locally on kind, do "kubectl port-forward -n kagent deployments/kagent-controller 8083"
		kagentURL = "http://localhost:8083"
	}
	return kagentURL + "/api/mcp"
}

// setupMCPClient creates and initializes an MCP client for testing
func setupMCPClient(t *testing.T) *mcp_client.Client {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	url := mcpEndpointURL()
	tsp, err := transport.NewStreamableHTTP(url)
	require.NoError(t, err, "Failed to create transport")
	client := mcp_client.NewClient(tsp)

	err = client.Start(ctx)
	require.NoError(t, err, "Failed to start MCP client")

	t.Cleanup(func() {
		client.Close()
	})

	_, err = client.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			Capabilities:    mcp.ClientCapabilities{},
			ClientInfo: mcp.Implementation{
				Name:    "e2e-test",
				Version: "0.0.0",
			},
		},
	})
	require.NoError(t, err, "Failed to initialize MCP client")

	return client
}

// TestE2EMCPEndpointListAgents tests the list_agents tool via the controller's MCP endpoint
// These tests use the kebab-agent deployed via push-test-agent in CI.
func TestE2EMCPEndpointListAgents(t *testing.T) {
	ctx := context.Background()
	client := setupMCPClient(t)

	// List tools
	toolsResult, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	require.NoError(t, err, "Should list tools")

	// Verify expected tools exist
	toolNames := make([]string, 0, len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		toolNames = append(toolNames, tool.Name)
	}
	require.Contains(t, toolNames, "list_agents", "Should have list_agents tool")
	require.Contains(t, toolNames, "invoke_agent", "Should have invoke_agent tool")

	// Call list_agents tool
	listAgentsResult, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "list_agents",
		},
	})
	require.NoError(t, err, "Should call list_agents tool")
	require.NotEmpty(t, listAgentsResult.Content, "Should have content in response")
	require.False(t, listAgentsResult.IsError, "Should not be an error")

	agentRef := "kagent/kebab-agent"
	found := false

	// First check StructuredContent (preferred)
	if listAgentsResult.StructuredContent != nil {
		structuredBytes, err := json.Marshal(listAgentsResult.StructuredContent)
		require.NoError(t, err, "Should marshal structured content")
		var structuredData struct {
			Agents []struct {
				Ref         string `json:"ref"`
				Description string `json:"description,omitempty"`
			} `json:"agents"`
		}
		if err := json.Unmarshal(structuredBytes, &structuredData); err == nil {
			for _, a := range structuredData.Agents {
				if a.Ref == agentRef {
					found = true
					break
				}
			}
		}
	}

	// Check text format for fallback
	if !found {
		for _, content := range listAgentsResult.Content {
			if textContent, ok := content.(*mcp.TextContent); ok {
				if strings.Contains(textContent.Text, agentRef) {
					found = true
					break
				}
			}
		}
	}

	require.True(t, found, "Should find agent %s in list", agentRef)
}

// TestE2EMCPEndpointInvokeAgent tests the invoke_agent tool via the controller's MCP endpoint
func TestE2EMCPEndpointInvokeAgent(t *testing.T) {
	ctx := context.Background()
	client := setupMCPClient(t)

	// Invoke kebab-agent
	agentRef := "kagent/kebab-agent"
	invokeResult, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "invoke_agent",
			Arguments: map[string]interface{}{
				"agent": agentRef,
				"task":  "What can you do?",
			},
		},
	})
	require.NoError(t, err, "Should call invoke_agent tool")
	require.NotEmpty(t, invokeResult.Content, "Should have content in response")
	require.False(t, invokeResult.IsError, "Should not be an error")

	foundText := false

	if invokeResult.StructuredContent != nil {
		structuredBytes, err := json.Marshal(invokeResult.StructuredContent)
		require.NoError(t, err, "Should marshal structured content")
		var structuredData struct {
			Agent string `json:"agent"`
			Text  string `json:"text"`
		}
		if err := json.Unmarshal(structuredBytes, &structuredData); err == nil {
			if strings.Contains(strings.ToLower(structuredData.Text), "kebab") {
				foundText = true
			}
		}
	}

	if !foundText {
		for _, content := range invokeResult.Content {
			if textContent, ok := content.(*mcp.TextContent); ok && textContent.Text != "" {
				if strings.Contains(strings.ToLower(textContent.Text), "kebab") {
					foundText = true
					break
				}
			}
		}
	}

	require.True(t, foundText, "Should have text content containing 'kebab' in response")
}

// TestE2EMCPEndpointErrorHandling tests error handling in the MCP endpoint
func TestE2EMCPEndpointErrorHandling(t *testing.T) {
	ctx := context.Background()
	client := setupMCPClient(t)

	// Try to invoke a non-existent agent
	result, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "invoke_agent",
			Arguments: map[string]interface{}{
				"agent": "nonexistent/agent",
				"task":  "test",
			},
		},
	})
	require.True(t, result.IsError, "Should return error")
	// This content is the error text for the LLM to know what went wrong
	require.NotEmpty(t, result.Content, "Should have error content")

	// Try to call a non-existent tool
	_, err = client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "nonexistent_tool",
		},
	})
	// Should return an error
	require.Error(t, err, "Should return error for non-existent tool")
}
