package common

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RunCommand executes a command and returns output or error
func RunCommand(command string, args []string) (string, error) {
	cmd := exec.Command(command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("error running %s command: %s", command, string(output))
	}
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
	s.AddTool(mcp.NewTool("shell",
		mcp.WithDescription("Execute a shell command"),
		mcp.WithString("command",
			mcp.Description("The shell command to execute"),
			mcp.Required(),
		),
	), handleShellTool)
}
