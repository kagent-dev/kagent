package utils

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/tools/pkg/logger"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
)

var (
	tracer = otel.Tracer("kagent-tools")
	meter  = otel.Meter("kagent-tools")

	// Metrics
	commandExecutionCounter  metric.Int64Counter
	commandExecutionDuration metric.Float64Histogram
	commandExecutionErrors   metric.Int64Counter
)

func init() {
	// Initialize metrics (these are safe to call even if OTEL is not configured)
	var err error

	commandExecutionCounter, err = meter.Int64Counter(
		"command_executions_total",
		metric.WithDescription("Total number of command executions"),
	)
	if err != nil {
		logger.Get().Error(err, "Failed to create command execution counter")
	}

	commandExecutionDuration, err = meter.Float64Histogram(
		"command_execution_duration_seconds",
		metric.WithDescription("Duration of command executions in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		logger.Get().Error(err, "Failed to create command execution duration histogram")
	}

	commandExecutionErrors, err = meter.Int64Counter(
		"command_execution_errors_total",
		metric.WithDescription("Total number of command execution errors"),
	)
	if err != nil {
		logger.Get().Error(err, "Failed to create command execution errors counter")
	}
}

// RunCommand executes a command and returns output or error with OTEL tracing
func RunCommand(command string, args []string) (string, error) {
	return RunCommandWithContext(context.Background(), command, args)
}

// RunCommandWithContext executes a command with context and returns output or error with OTEL tracing
func RunCommandWithContext(ctx context.Context, command string, args []string) (string, error) {
	// Get caller information for tracing
	_, file, line, _ := runtime.Caller(1)
	caller := fmt.Sprintf("%s:%d", file, line)

	// Start OpenTelemetry span
	spanName := fmt.Sprintf("exec.%s", command)
	ctx, span := tracer.Start(ctx, spanName)
	defer span.End()

	// Set span attributes
	span.SetAttributes(
		attribute.String("command", command),
		attribute.StringSlice("args", args),
		attribute.String("caller", caller),
	)

	// Record metrics
	startTime := time.Now()

	cmd := exec.CommandContext(ctx, command, args...)
	output, err := cmd.CombinedOutput()

	duration := time.Since(startTime)

	// Set additional span attributes with results
	span.SetAttributes(
		attribute.Float64("duration_seconds", duration.Seconds()),
		attribute.Int("output_size", len(output)),
	)

	// Record metrics
	attributes := []attribute.KeyValue{
		attribute.String("command", command),
		attribute.Bool("success", err == nil),
	}

	if commandExecutionCounter != nil {
		commandExecutionCounter.Add(ctx, 1, metric.WithAttributes(attributes...))
	}

	if commandExecutionDuration != nil {
		commandExecutionDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attributes...))
	}

	if err != nil {
		// Set span status and record error
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(attribute.String("error", err.Error()))

		if commandExecutionErrors != nil {
			commandExecutionErrors.Add(ctx, 1, metric.WithAttributes(attributes...))
		}

		logger.Get().Error(err, "CommandExec failed",
			"command", command,
			"args", args,
			"duration", duration,
			"caller", caller,
		)
		return "", fmt.Errorf("command %s failed: %v", command, err)
	}

	// Set successful span status
	span.SetStatus(codes.Ok, "CommandExec")

	logger.Get().Info("CommandExec",
		"command", command,
		"args", args,
		"duration", duration,
		"outputSize", len(output),
		"caller", caller,
	)

	return strings.TrimSpace(string(output)), nil
}

// shellTool provides shell command execution functionality
type shellParams struct {
	Command string `json:"command" description:"The shell command to execute"`
}

func shellTool(ctx context.Context, params shellParams) (string, error) {
	// Split command into parts (basic implementation)
	parts := strings.Fields(params.Command)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty command")
	}

	cmd := parts[0]
	args := parts[1:]

	return RunCommandWithContext(ctx, cmd, args)
}

func RegisterCommonTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("shell",
		mcp.WithDescription("Execute shell commands"),
		mcp.WithString("command", mcp.Description("The shell command to execute"), mcp.Required()),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		command := mcp.ParseString(request, "command", "")
		if command == "" {
			return mcp.NewToolResultError("command parameter is required"), nil
		}

		params := shellParams{Command: command}
		result, err := shellTool(ctx, params)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return mcp.NewToolResultText(result), nil
	})

	// Note: LLM Tool implementation would go here if needed
}
