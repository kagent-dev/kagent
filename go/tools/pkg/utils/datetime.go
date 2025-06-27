package utils

import (
	"context"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// DateTime tools using direct Go time package
// This implementation matches the Python version exactly

func handleGetCurrentDateTimeTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Returns the current date and time in ISO 8601 format (RFC3339)
	// This matches the Python implementation: datetime.datetime.now().isoformat()
	now := time.Now()
	return mcp.NewToolResultText(now.Format(time.RFC3339)), nil
}

func RegisterDateTimeTools(s *server.MCPServer) {
	// Register the GetCurrentDateTime tool to match Python implementation exactly
	s.AddTool(mcp.NewTool("datetime_get_current_time",
		mcp.WithDescription("Returns the current date and time in ISO 8601 format."),
	), handleGetCurrentDateTimeTool)
}
