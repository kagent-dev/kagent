package common

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/tools/internal/logger"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RunCommand executes a command and returns output or error
func RunCommand(command string, args []string) (string, error) {
	// Get caller information for logging
	_, file, line, _ := runtime.Caller(1)
	caller := fmt.Sprintf("%s:%d", file, line)

	// Log command execution start
	logger.LogExecCommand(command, args, caller)

	// Start timing
	start := time.Now()

	cmd := exec.Command(command, args...)
	output, err := cmd.CombinedOutput()

	// Calculate duration
	duration := time.Since(start).Seconds()

	// Log command execution result
	if err != nil {
		logger.LogExecCommandResult(command, args, string(output), err, duration, caller)
		return "", fmt.Errorf("error running %s command: %s", command, string(output))
	}

	trimmedOutput := strings.TrimSpace(string(output))
	logger.LogExecCommandResult(command, args, trimmedOutput, nil, duration, caller)
	return trimmedOutput, nil
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

	return RunCommand(cmd, args)
}

func handleShellTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	command := mcp.ParseString(request, "command", "")
	if command == "" {
		return mcp.NewToolResultError("Command parameter is required"), nil
	}

	params := shellParams{Command: command}
	result, err := shellTool(ctx, params)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(result), nil
}

func RegisterCommonTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("Shell",
		mcp.WithDescription("Execute a shell command"),
		mcp.WithString("command",
			mcp.Description("The shell command to execute"),
			mcp.Required(),
		),
	), handleShellTool)
}
