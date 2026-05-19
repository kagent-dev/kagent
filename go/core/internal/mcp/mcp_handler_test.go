package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestListAgentsInputSchemaHasProperties asserts that the list_agents tool
// advertises an inputSchema containing an explicit "properties" key, even
// though it accepts no arguments. OpenAI strict mode requires this.
// Regression test for https://github.com/kagent-dev/kagent/issues/1889.
func TestListAgentsInputSchemaHasProperties(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	h, err := NewMCPHandler(kubeClient, "http://unused", nil, time.Minute)
	require.NoError(t, err)

	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	// Run the server in a goroutine; it returns when the transport closes.
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- h.server.Run(ctx, serverTransport)
	}()
	// Registered first so it runs last (LIFO): after session.Close below has
	// disconnected the client, cancel the context and drain the server's
	// return value so the goroutine cannot leak and unexpected errors surface.
	t.Cleanup(func() {
		cancel()
		if err := <-serverDone; err != nil && err != context.Canceled {
			t.Errorf("MCP server returned unexpected error: %v", err)
		}
	})

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { session.Close() })

	tools, err := session.ListTools(ctx, &mcpsdk.ListToolsParams{})
	require.NoError(t, err)

	var listAgents *mcpsdk.Tool
	for i := range tools.Tools {
		if tools.Tools[i].Name == "list_agents" {
			listAgents = tools.Tools[i]
			break
		}
	}
	require.NotNil(t, listAgents, "list_agents tool not registered")

	raw, err := json.Marshal(listAgents.InputSchema)
	require.NoError(t, err)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))

	require.Equal(t, "object", schema["type"], "inputSchema type must be object")
	props, ok := schema["properties"]
	require.True(t, ok, "inputSchema must include a properties key (got %s)", string(raw))
	require.IsType(t, map[string]any{}, props, "properties must be a JSON object")
	require.Empty(t, props, "list_agents takes no args, properties should be empty")
	require.Equal(t, false, schema["additionalProperties"], "additionalProperties must remain false")
}
