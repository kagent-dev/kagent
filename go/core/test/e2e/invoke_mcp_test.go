package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// mcpEndpointURL returns the URL for the MCP endpoint
func mcpEndpointURL() string {
	kagentURL := os.Getenv("KAGENT_URL")
	if kagentURL == "" {
		// if running locally on kind, do "kubectl port-forward -n kagent deployments/kagent-controller 8083"
		kagentURL = "http://localhost:8083"
	}
	return kagentURL + "/mcp"
}

// setupMCPClient creates and initializes an MCP client for testing
func setupMCPClient(t *testing.T) *mcp.ClientSession {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	url := mcpEndpointURL()
	transport := &mcp.StreamableClientTransport{
		Endpoint: url,
	}

	impl := &mcp.Implementation{
		Name:    "e2e-test",
		Version: "0.0.0",
	}
	client := mcp.NewClient(impl, nil)

	session, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err, "Failed to connect MCP client")

	t.Cleanup(func() {
		session.Close()
	})

	return session
}

// TestE2EMCPEndpointListAgents tests the list_agents tool via the controller's MCP endpoint
// These tests use the kebab-agent deployed via push-test-agent in CI.
func TestE2EMCPEndpointListAgents(t *testing.T) {
	ctx := context.Background()
	session := setupMCPClient(t)

	// List tools
	toolsResult, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	require.NoError(t, err, "Should list tools")

	// Verify expected tools exist
	toolNames := make([]string, 0, len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		toolNames = append(toolNames, tool.Name)
	}
	require.Contains(t, toolNames, "list_agents", "Should have list_agents tool")
	require.Contains(t, toolNames, "invoke_agent", "Should have invoke_agent tool")

	// Call list_agents tool
	listAgentsResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "list_agents",
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
	session := setupMCPClient(t)

	// Invoke kebab-agent
	agentRef := "kagent/kebab-agent"
	invokeResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "invoke_agent",
		Arguments: map[string]any{
			"agent": agentRef,
			"task":  "What can you do?",
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

// TestE2EMCPAgentsResourceList verifies the kagent://agents resource is advertised by the server.
func TestE2EMCPAgentsResourceList(t *testing.T) {
	ctx := context.Background()
	session := setupMCPClient(t)

	result, err := session.ListResources(ctx, &mcp.ListResourcesParams{})
	require.NoError(t, err, "Should list resources")

	found := false
	for _, r := range result.Resources {
		if r.URI == "kagent://agents" {
			found = true
			require.Equal(t, "application/json", r.MIMEType, "kagent://agents should have JSON MIME type")
			break
		}
	}
	require.True(t, found, "kagent://agents resource not found in resources/list response")
}

// TestE2EMCPAgentsResourceRead verifies reading kagent://agents returns a JSON array containing the deployed kebab-agent.
func TestE2EMCPAgentsResourceRead(t *testing.T) {
	ctx := context.Background()
	session := setupMCPClient(t)

	result, err := session.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "kagent://agents",
	})
	require.NoError(t, err, "Should read kagent://agents resource")
	require.NotEmpty(t, result.Contents, "Resource contents should not be empty")
	require.Equal(t, "application/json", result.Contents[0].MIMEType, "Content should be JSON")

	var agents []struct {
		Ref         string `json:"ref"`
		Description string `json:"description,omitempty"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.Contents[0].Text), &agents), "Contents should be a valid JSON array")

	found := false
	for _, a := range agents {
		if a.Ref == "kagent/kebab-agent" {
			found = true
			break
		}
	}
	require.True(t, found, "kagent/kebab-agent should appear in kagent://agents resource")
}

// TestE2EMCPAgentsResourceNotification verifies that creating an agent triggers a resources/updated
// notification for kagent://agents on subscribed MCP clients.
func TestE2EMCPAgentsResourceNotification(t *testing.T) {
	notified := make(chan string, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	transport := &mcp.StreamableClientTransport{Endpoint: mcpEndpointURL()}
	impl := &mcp.Implementation{Name: "e2e-test-notifications", Version: "0.0.0"}
	mcpClient := mcp.NewClient(impl, &mcp.ClientOptions{
		ResourceUpdatedHandler: func(_ context.Context, req *mcp.ResourceUpdatedNotificationRequest) {
			select {
			case notified <- req.Params.URI:
			default:
			}
		},
	})

	session, err := mcpClient.Connect(ctx, transport, nil)
	require.NoError(t, err, "Should connect MCP client with notification handler")
	t.Cleanup(func() { session.Close() })

	// Subscribe before triggering the change so we don't miss the notification.
	err = session.Subscribe(ctx, &mcp.SubscribeParams{URI: "kagent://agents"})
	require.NoError(t, err, "Should subscribe to kagent://agents")

	// Create a new agent — the A2ARegistrar informer fires immediately on object
	// creation and calls NotifyAgentsChanged, which pushes the notification.
	k8sClient := setupK8sClient(t, false)
	mockURL, stopMock := setupMockServer(t, "mocks/invoke_inline_agent.json")
	defer stopMock()

	modelCfg := setupModelConfig(t, k8sClient, mockURL)
	agent := generateAgent(modelCfg.Name, nil, AgentOptions{Name: "mcp-notify-test"})
	require.NoError(t, k8sClient.Create(t.Context(), agent), "Should create agent")
	t.Cleanup(func() {
		_ = k8sClient.Delete(context.Background(), agent)
	})

	select {
	case uri := <-notified:
		require.Equal(t, "kagent://agents", uri, "Notification URI should match subscribed resource")
	case <-time.After(30 * time.Second):
		t.Fatal("Did not receive resources/updated notification for kagent://agents within 30s")
	}
}

// TestE2EMCPEndpointErrorHandling tests error handling in the MCP endpoint
func TestE2EMCPEndpointErrorHandling(t *testing.T) {
	ctx := context.Background()
	session := setupMCPClient(t)

	// Try to invoke a non-existent agent
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "invoke_agent",
		Arguments: map[string]any{
			"agent": "nonexistent/agent",
			"task":  "test",
		},
	})
	require.NoError(t, err, "CallTool should not return protocol error")
	require.True(t, result.IsError, "Should return error")
	// This content is the error text for the LLM to know what went wrong
	require.NotEmpty(t, result.Content, "Should have error content")

	// Try to call a non-existent tool
	_, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name: "nonexistent_tool",
	})
	// Should return an error
	require.Error(t, err, "Should return error for non-existent tool")
}
