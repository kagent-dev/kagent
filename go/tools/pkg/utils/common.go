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

// ShellExecutor defines the interface for executing shell commands
type ShellExecutor interface {
	Exec(ctx context.Context, command string, args ...string) (output []byte, err error)
}

// DefaultShellExecutor implements ShellExecutor using os/exec
type DefaultShellExecutor struct{}

// Exec executes a command using os/exec.CommandContext
func (e *DefaultShellExecutor) Exec(ctx context.Context, command string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	return cmd.CombinedOutput()
}

// MockShellExecutor implements ShellExecutor for testing
type MockShellExecutor struct {
	// Commands maps command+args to expected output and error
	Commands map[string]MockCommandResult
	// CallLog keeps track of all executed commands for verification
	CallLog []MockCommandCall
	// PartialMatchers allows partial matching for dynamic arguments
	PartialMatchers []PartialMatcher
}

// PartialMatcher represents a partial command matcher for dynamic arguments
type PartialMatcher struct {
	Command string
	Args    []string // Use "*" for wildcard matching
	Result  MockCommandResult
}

// MockCommandResult represents the expected result of a mocked command
type MockCommandResult struct {
	Output []byte
	Error  error
}

// MockCommandCall represents a logged command execution
type MockCommandCall struct {
	Command string
	Args    []string
}

// Exec executes a mocked command
func (m *MockShellExecutor) Exec(ctx context.Context, command string, args ...string) ([]byte, error) {
	// Log the call
	m.CallLog = append(m.CallLog, MockCommandCall{
		Command: command,
		Args:    args,
	})

	// Try exact match first
	key := m.commandKey(command, args...)
	if result, exists := m.Commands[key]; exists {
		return result.Output, result.Error
	}

	// Try partial matchers
	for _, matcher := range m.PartialMatchers {
		if m.matchesPartial(command, args, matcher) {
			return matcher.Result.Output, matcher.Result.Error
		}
	}

	// Default behavior for unmocked commands
	return []byte(""), fmt.Errorf("unmocked command: %s %v", command, args)
}

// matchesPartial checks if a command matches a partial matcher
func (m *MockShellExecutor) matchesPartial(command string, args []string, matcher PartialMatcher) bool {
	if command != matcher.Command {
		return false
	}

	if len(args) != len(matcher.Args) {
		return false
	}

	for i, expectedArg := range matcher.Args {
		if expectedArg == "*" {
			continue // Wildcard match
		}
		if args[i] != expectedArg {
			return false
		}
	}

	return true
}

// AddCommand adds a command mock
func (m *MockShellExecutor) AddCommand(command string, args []string, output []byte, err error) {
	if m.Commands == nil {
		m.Commands = make(map[string]MockCommandResult)
	}
	key := m.commandKey(command, args...)
	m.Commands[key] = MockCommandResult{
		Output: output,
		Error:  err,
	}
}

// AddCommandString is a convenience method for adding string output
func (m *MockShellExecutor) AddCommandString(command string, args []string, output string, err error) {
	m.AddCommand(command, args, []byte(output), err)
}

// AddPartialMatcher adds a partial matcher for dynamic arguments
func (m *MockShellExecutor) AddPartialMatcher(command string, args []string, output []byte, err error) {
	if m.PartialMatchers == nil {
		m.PartialMatchers = []PartialMatcher{}
	}
	m.PartialMatchers = append(m.PartialMatchers, PartialMatcher{
		Command: command,
		Args:    args,
		Result: MockCommandResult{
			Output: output,
			Error:  err,
		},
	})
}

// AddPartialMatcherString is a convenience method for adding string output with partial matching
func (m *MockShellExecutor) AddPartialMatcherString(command string, args []string, output string, err error) {
	m.AddPartialMatcher(command, args, []byte(output), err)
}

// GetCallLog returns the log of all command calls
func (m *MockShellExecutor) GetCallLog() []MockCommandCall {
	return m.CallLog
}

// Reset clears the mock state
func (m *MockShellExecutor) Reset() {
	m.Commands = make(map[string]MockCommandResult)
	m.CallLog = []MockCommandCall{}
	m.PartialMatchers = []PartialMatcher{}
}

// commandKey creates a unique key for command+args combination
func (m *MockShellExecutor) commandKey(command string, args ...string) string {
	return fmt.Sprintf("%s %s", command, strings.Join(args, " "))
}

// Context key for shell executor injection
type contextKey string

const shellExecutorKey contextKey = "shellExecutor"

// WithShellExecutor returns a context with the given shell executor
func WithShellExecutor(ctx context.Context, executor ShellExecutor) context.Context {
	return context.WithValue(ctx, shellExecutorKey, executor)
}

// GetShellExecutor retrieves the shell executor from context, or returns default
func GetShellExecutor(ctx context.Context) ShellExecutor {
	if executor, ok := ctx.Value(shellExecutorKey).(ShellExecutor); ok {
		return executor
	}
	return &DefaultShellExecutor{}
}

// NewMockShellExecutor creates a new mock shell executor for testing
func NewMockShellExecutor() *MockShellExecutor {
	return &MockShellExecutor{
		Commands:        make(map[string]MockCommandResult),
		CallLog:         []MockCommandCall{},
		PartialMatchers: []PartialMatcher{},
	}
}

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

	// Use the shell executor from context (or default)
	executor := GetShellExecutor(ctx)
	output, err := executor.Exec(ctx, command, args...)

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
