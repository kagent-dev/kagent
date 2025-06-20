package datetime

import (
	"context"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// DateTime tools using direct Go time package

func handleCurrentDateTimeTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	now := time.Now()
	return mcp.NewToolResultText(now.Format(time.RFC3339)), nil
}

func handleFormatTimeTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	timestamp := mcp.ParseString(request, "timestamp", "")
	format := mcp.ParseString(request, "format", time.RFC3339)
	timezone := mcp.ParseString(request, "timezone", "UTC")

	if timestamp == "" {
		return mcp.NewToolResultError("timestamp parameter is required"), nil
	}

	// Parse input timestamp
	var t time.Time
	var err error

	// Try parsing as RFC3339 first
	t, err = time.Parse(time.RFC3339, timestamp)
	if err != nil {
		// Try parsing as Unix timestamp
		t, err = time.Parse("1136239445", timestamp)
		if err != nil {
			return mcp.NewToolResultError("failed to parse timestamp: " + err.Error()), nil
		}
	}

	// Apply timezone
	if timezone != "UTC" {
		loc, err := time.LoadLocation(timezone)
		if err != nil {
			return mcp.NewToolResultError("invalid timezone: " + err.Error()), nil
		}
		t = t.In(loc)
	}

	return mcp.NewToolResultText(t.Format(format)), nil
}

func handleParseTimeTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	timeString := mcp.ParseString(request, "time_string", "")
	format := mcp.ParseString(request, "format", "")

	if timeString == "" {
		return mcp.NewToolResultError("time_string parameter is required"), nil
	}

	var t time.Time
	var err error

	if format != "" {
		t, err = time.Parse(format, timeString)
	} else {
		// Try common formats
		commonFormats := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
			"2006-01-02",
			"15:04:05",
		}

		for _, fmt := range commonFormats {
			t, err = time.Parse(fmt, timeString)
			if err == nil {
				break
			}
		}
	}

	if err != nil {
		return mcp.NewToolResultError("failed to parse time string: " + err.Error()), nil
	}

	return mcp.NewToolResultText(t.Format(time.RFC3339)), nil
}

func RegisterDateTimeTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("current_date_time",
		mcp.WithDescription("Get the current date and time in ISO 8601 format"),
	), handleCurrentDateTimeTool)

	s.AddTool(mcp.NewTool("format_time",
		mcp.WithDescription("Format a timestamp with optional timezone"),
		mcp.WithString("timestamp", mcp.Description("Unix timestamp or RFC3339 timestamp to format"), mcp.Required()),
		mcp.WithString("format", mcp.Description("Go time format string (default: RFC3339)")),
		mcp.WithString("timezone", mcp.Description("Timezone to format in (default: UTC)")),
	), handleFormatTimeTool)

	s.AddTool(mcp.NewTool("parse_time",
		mcp.WithDescription("Parse a time string into RFC3339 format"),
		mcp.WithString("time_string", mcp.Description("Time string to parse"), mcp.Required()),
		mcp.WithString("format", mcp.Description("Go time format for parsing (tries common formats if not specified)")),
	), handleParseTimeTool)
}
