package telemetry

import (
	"context"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var (
	mcpTracer = otel.Tracer("kagent-mcp-server")
	mcpMeter  = otel.Meter("kagent-mcp-server")

	// MCP-specific metrics
	mcpToolCallsCounter    metric.Int64Counter
	mcpToolCallDuration    metric.Float64Histogram
	mcpToolCallErrors      metric.Int64Counter
	mcpServerRequestsTotal metric.Int64Counter
	mcpServerResponseTime  metric.Float64Histogram
)

func init() {
	// Initialize MCP metrics
	var err error

	mcpToolCallsCounter, err = mcpMeter.Int64Counter(
		"mcp_tool_calls_total",
		metric.WithDescription("Total number of MCP tool calls"),
	)
	if err != nil {
		// Log error but continue - metrics are optional
	}

	mcpToolCallDuration, err = mcpMeter.Float64Histogram(
		"mcp_tool_call_duration_seconds",
		metric.WithDescription("Duration of MCP tool calls in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		// Log error but continue - metrics are optional
	}

	mcpToolCallErrors, err = mcpMeter.Int64Counter(
		"mcp_tool_call_errors_total",
		metric.WithDescription("Total number of MCP tool call errors"),
	)
	if err != nil {
		// Log error but continue - metrics are optional
	}

	mcpServerRequestsTotal, err = mcpMeter.Int64Counter(
		"mcp_server_requests_total",
		metric.WithDescription("Total number of MCP server requests"),
	)
	if err != nil {
		// Log error but continue - metrics are optional
	}

	mcpServerResponseTime, err = mcpMeter.Float64Histogram(
		"mcp_server_response_time_seconds",
		metric.WithDescription("MCP server response time in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		// Log error but continue - metrics are optional
	}
}

// InstrumentedMCPServer wraps an MCP server with OpenTelemetry instrumentation
type InstrumentedMCPServer struct {
	*server.MCPServer
	originalAddTool func(tool mcp.Tool, handler server.ToolHandlerFunc)
}

// NewInstrumentedMCPServer creates a new instrumented MCP server
func NewInstrumentedMCPServer(name, version string) *InstrumentedMCPServer {
	mcpServer := server.NewMCPServer(name, version)

	instrumented := &InstrumentedMCPServer{
		MCPServer: mcpServer,
	}

	// Store the original AddTool method
	instrumented.originalAddTool = mcpServer.AddTool

	return instrumented
}

// AddTool wraps the original AddTool method with OpenTelemetry instrumentation
func (s *InstrumentedMCPServer) AddTool(tool mcp.Tool, handler server.ToolHandlerFunc) {
	// Wrap the handler with instrumentation
	instrumentedHandler := s.instrumentToolHandler(tool.Name, handler)

	// Call the original AddTool method with the instrumented handler
	s.originalAddTool(tool, instrumentedHandler)
}

// instrumentToolHandler wraps a tool handler with OpenTelemetry tracing and metrics
func (s *InstrumentedMCPServer) instrumentToolHandler(toolName string, handler server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Start tracing span
		ctx, span := mcpTracer.Start(ctx, "mcp.tool_call",
			trace.WithAttributes(
				attribute.String("tool.name", toolName),
				attribute.String("mcp.operation", "tool_call"),
			),
		)
		defer span.End()

		// Record metrics
		startTime := time.Now()

		// Increment tool calls counter
		if mcpToolCallsCounter != nil {
			mcpToolCallsCounter.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("tool_name", toolName),
				),
			)
		}

		// Add request parameters to span (safely)
		// Note: We don't access request.Arguments directly since it may not exist
		// Instead, we can count parameters by trying to parse some common ones
		paramCount := 0
		if mcp.ParseString(request, "test_param", "") != "" {
			paramCount++
		}
		span.SetAttributes(
			attribute.Int("request.param_check_count", paramCount),
		)

		// Call the original handler
		result, err := handler(ctx, request)

		// Record duration
		duration := time.Since(startTime)
		if mcpToolCallDuration != nil {
			mcpToolCallDuration.Record(ctx, duration.Seconds(),
				metric.WithAttributes(
					attribute.String("tool_name", toolName),
				),
			)
		}

		// Handle errors
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())

			// Increment error counter
			if mcpToolCallErrors != nil {
				mcpToolCallErrors.Add(ctx, 1,
					metric.WithAttributes(
						attribute.String("tool_name", toolName),
						attribute.String("error_type", "handler_error"),
					),
				)
			}

			span.SetAttributes(
				attribute.Bool("tool.success", false),
				attribute.String("tool.error", err.Error()),
			)

			return result, err
		}

		// Handle tool result errors (when err is nil but result contains an error)
		if result != nil && result.IsError {
			span.SetAttributes(
				attribute.Bool("tool.success", false),
				attribute.String("tool.result_type", "error"),
			)

			// Increment error counter for tool result errors
			if mcpToolCallErrors != nil {
				mcpToolCallErrors.Add(ctx, 1,
					metric.WithAttributes(
						attribute.String("tool_name", toolName),
						attribute.String("error_type", "tool_result_error"),
					),
				)
			}

			span.SetStatus(codes.Error, "Tool returned error result")
		} else {
			span.SetAttributes(
				attribute.Bool("tool.success", true),
				attribute.String("tool.result_type", "success"),
			)
			span.SetStatus(codes.Ok, "Tool call successful")
		}

		// Add result metadata to span
		if result != nil {
			span.SetAttributes(
				attribute.Bool("result.is_error", result.IsError),
			)

			// Add content information without logging the actual content
			if len(result.Content) > 0 {
				span.SetAttributes(
					attribute.Int("result.content_count", len(result.Content)),
				)
			}
		}

		return result, err
	}
}

// InstrumentMCPServer wraps an existing MCP server with OpenTelemetry instrumentation
// This is useful when you already have an MCP server instance
func InstrumentMCPServer(mcpServer *server.MCPServer) *InstrumentedMCPServer {
	return &InstrumentedMCPServer{
		MCPServer:       mcpServer,
		originalAddTool: mcpServer.AddTool,
	}
}

// RecordMCPServerRequest records metrics for MCP server requests
func RecordMCPServerRequest(ctx context.Context, method string, duration time.Duration, success bool) {
	if mcpServerRequestsTotal != nil {
		mcpServerRequestsTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("method", method),
				attribute.Bool("success", success),
			),
		)
	}

	if mcpServerResponseTime != nil {
		mcpServerResponseTime.Record(ctx, duration.Seconds(),
			metric.WithAttributes(
				attribute.String("method", method),
				attribute.Bool("success", success),
			),
		)
	}
}
