package utils

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// Test the actual MCP tool handler functions
// These tests match the Python implementation exactly

func TestHandleGetCurrentDateTimeTool(t *testing.T) {
	ctx := context.Background()
	request := mcp.CallToolRequest{}

	result, err := handleGetCurrentDateTimeTool(ctx, request)
	if err != nil {
		t.Fatalf("handleGetCurrentDateTimeTool failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Content == nil || len(result.Content) == 0 {
		t.Fatal("Expected content in result")
	}

	// Verify the result is a valid RFC3339 timestamp (ISO 8601 format)
	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(mcp.TextContent); ok {
			_, err := time.Parse(time.RFC3339, textContent.Text)
			if err != nil {
				t.Errorf("Result is not valid RFC3339 timestamp: %v", err)
			}
			// Additional check: ensure it's a recent timestamp (within last minute)
			parsed, _ := time.Parse(time.RFC3339, textContent.Text)
			if time.Since(parsed) > time.Minute {
				t.Errorf("Timestamp seems too old: %s", textContent.Text)
			}
		} else {
			t.Error("Expected TextContent in result")
		}
	} else {
		t.Error("Expected content in result")
	}
}

func TestHandleGetCurrentDateTimeToolNoParameters(t *testing.T) {
	// Test that the tool works without any parameters (as per Python implementation)
	ctx := context.Background()
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]interface{}{} // Empty arguments

	result, err := handleGetCurrentDateTimeTool(ctx, request)
	if err != nil {
		t.Fatalf("handleGetCurrentDateTimeTool failed with empty args: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.IsError {
		t.Error("Expected successful result, got error")
	}

	// Verify we get a valid timestamp
	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(mcp.TextContent); ok {
			_, err := time.Parse(time.RFC3339, textContent.Text)
			if err != nil {
				t.Errorf("Result is not valid RFC3339 timestamp: %v", err)
			}
		} else {
			t.Error("Expected TextContent in result")
		}
	} else {
		t.Error("Expected content in result")
	}
}

func TestDateTimeFormatConsistency(t *testing.T) {
	// Test that our Go implementation produces ISO 8601 format consistent with Python
	ctx := context.Background()
	request := mcp.CallToolRequest{}

	result, err := handleGetCurrentDateTimeTool(ctx, request)
	if err != nil {
		t.Fatalf("handleGetCurrentDateTimeTool failed: %v", err)
	}

	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(mcp.TextContent); ok {
			timestamp := textContent.Text

			// Check that it follows RFC3339 format (which is ISO 8601 compliant)
			// Format should be: YYYY-MM-DDTHH:MM:SS.sssssssss+00:00 or YYYY-MM-DDTHH:MM:SSZ
			parsed, err := time.Parse(time.RFC3339, timestamp)
			if err != nil {
				// Try RFC3339Nano in case it includes nanoseconds
				parsed, err = time.Parse(time.RFC3339Nano, timestamp)
				if err != nil {
					t.Errorf("Timestamp format is not valid ISO 8601/RFC3339: %s, error: %v", timestamp, err)
				}
			}

			// Verify it can be formatted back to the same or similar format
			reformatted := parsed.Format(time.RFC3339)
			if reformatted == "" {
				t.Error("Failed to reformat timestamp")
			}
		}
	}
}
