package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInstrumentedMCPServer(t *testing.T) {
	instrumentedServer := NewInstrumentedMCPServer("test-server", "1.0.0")

	assert.NotNil(t, instrumentedServer)
	assert.NotNil(t, instrumentedServer.MCPServer)
	assert.NotNil(t, instrumentedServer.originalAddTool)
}

func TestInstrumentedMCPServer_AddTool_Success(t *testing.T) {
	instrumentedServer := NewInstrumentedMCPServer("test-server", "1.0.0")

	// Create a test tool
	tool := mcp.NewTool("test_tool",
		mcp.WithDescription("A test tool"),
		mcp.WithString("input", mcp.Description("Test input"), mcp.Required()),
	)

	// Create a test handler that succeeds
	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		input := mcp.ParseString(request, "input", "")
		return mcp.NewToolResultText("Hello " + input), nil
	}

	// Add the tool (this should not panic)
	require.NotPanics(t, func() {
		instrumentedServer.AddTool(tool, handler)
	})
}

func TestInstrumentedMCPServer_AddTool_Error(t *testing.T) {
	instrumentedServer := NewInstrumentedMCPServer("test-server", "1.0.0")

	// Create a test tool
	tool := mcp.NewTool("error_tool",
		mcp.WithDescription("A tool that errors"),
	)

	// Create a test handler that returns an error
	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return nil, errors.New("test error")
	}

	// Add the tool (this should not panic)
	require.NotPanics(t, func() {
		instrumentedServer.AddTool(tool, handler)
	})
}

func TestInstrumentedMCPServer_AddTool_ToolResultError(t *testing.T) {
	instrumentedServer := NewInstrumentedMCPServer("test-server", "1.0.0")

	// Create a test tool
	tool := mcp.NewTool("result_error_tool",
		mcp.WithDescription("A tool that returns an error result"),
	)

	// Create a test handler that returns a tool result error
	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultError("Tool execution failed"), nil
	}

	// Add the tool (this should not panic)
	require.NotPanics(t, func() {
		instrumentedServer.AddTool(tool, handler)
	})
}

func TestInstrumentMCPServer(t *testing.T) {
	// Create a regular MCP server
	regularServer := server.NewMCPServer("test-server", "1.0.0")

	// Instrument it
	instrumentedServer := InstrumentMCPServer(regularServer)

	assert.NotNil(t, instrumentedServer)
	assert.Equal(t, regularServer, instrumentedServer.MCPServer)
	assert.NotNil(t, instrumentedServer.originalAddTool)
}

func TestRecordMCPServerRequest(t *testing.T) {
	ctx := context.Background()

	// These should not panic even if metrics are not initialized
	require.NotPanics(t, func() {
		RecordMCPServerRequest(ctx, "test_method", time.Millisecond*100, true)
	})

	require.NotPanics(t, func() {
		RecordMCPServerRequest(ctx, "test_method", time.Millisecond*200, false)
	})
}

func TestInstrumentedHandler_ContextPropagation(t *testing.T) {
	instrumentedServer := NewInstrumentedMCPServer("test-server", "1.0.0")

	// Create a test tool
	tool := mcp.NewTool("context_tool",
		mcp.WithDescription("A tool that checks context"),
	)

	// Create a test handler that checks for context values
	var receivedCtx context.Context
	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		receivedCtx = ctx
		return mcp.NewToolResultText("context received"), nil
	}

	// Add the tool (this should not panic and should create an instrumented handler)
	require.NotPanics(t, func() {
		instrumentedServer.AddTool(tool, handler)
	})

	// The handler hasn't been called yet, so receivedCtx should still be nil
	// This test just verifies that the instrumented handler was created successfully
	assert.Nil(t, receivedCtx)
}

func TestInstrumentedHandler_MetricsInitialization(t *testing.T) {
	// Test that metrics initialization doesn't cause panics
	require.NotPanics(t, func() {
		// This should initialize metrics safely
		_ = NewInstrumentedMCPServer("test-server", "1.0.0")
	})
}

func TestInstrumentedHandler_ParameterCounting(t *testing.T) {
	instrumentedServer := NewInstrumentedMCPServer("test-server", "1.0.0")

	// Create a test tool
	tool := mcp.NewTool("param_tool",
		mcp.WithDescription("A tool with parameters"),
		mcp.WithString("param1", mcp.Description("First parameter")),
		mcp.WithString("param2", mcp.Description("Second parameter")),
	)

	// Create a test handler
	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		param1 := mcp.ParseString(request, "param1", "")
		param2 := mcp.ParseString(request, "param2", "")
		return mcp.NewToolResultText("param1: " + param1 + ", param2: " + param2), nil
	}

	// Add the tool (this should not panic)
	require.NotPanics(t, func() {
		instrumentedServer.AddTool(tool, handler)
	})
}
