package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/kagent-dev/kagent/go/sandbox-mcp/pkg/tools"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	s := server.NewMCPServer(
		"kagent-sandbox-mcp",
		"0.1.0",
	)

	// Register exec tool
	s.AddTool(mcp.NewTool(
		"exec",
		mcp.WithDescription("Execute a shell command in the sandbox"),
		mcp.WithString("command", mcp.Required(), mcp.Description("The shell command to execute")),
		mcp.WithNumber("timeout_ms", mcp.Description("Optional timeout in milliseconds")),
		mcp.WithString("working_dir", mcp.Description("Optional working directory")),
	), handleExec)

	// Register read_file tool
	s.AddTool(mcp.NewTool(
		"read_file",
		mcp.WithDescription("Read the content of a file"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path to the file")),
	), handleReadFile)

	// Register write_file tool
	s.AddTool(mcp.NewTool(
		"write_file",
		mcp.WithDescription("Write content to a file (creates parent directories if needed)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path to the file")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to write")),
	), handleWriteFile)

	// Register list_dir tool
	s.AddTool(mcp.NewTool(
		"list_dir",
		mcp.WithDescription("List entries in a directory"),
		mcp.WithString("path", mcp.Description("Directory path (defaults to current directory)")),
	), handleListDir)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("Starting kagent-sandbox-mcp on %s", addr)

	httpServer := server.NewStreamableHTTPServer(s)
	if err := httpServer.Start(addr); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func handleExec(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	command, _ := request.GetArguments()["command"].(string)
	timeoutMs := 0
	if v, ok := request.GetArguments()["timeout_ms"].(float64); ok {
		timeoutMs = int(v)
	}
	workingDir, _ := request.GetArguments()["working_dir"].(string)

	result, err := tools.Exec(ctx, command, timeoutMs, workingDir)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	data, _ := json.Marshal(result)
	return mcp.NewToolResultText(string(data)), nil
}

func handleReadFile(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, _ := request.GetArguments()["path"].(string)

	content, err := tools.ReadFile(path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(content), nil
}

func handleWriteFile(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, _ := request.GetArguments()["path"].(string)
	content, _ := request.GetArguments()["content"].(string)

	if err := tools.WriteFile(path, content); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	data, _ := json.Marshal(map[string]bool{"ok": true})
	return mcp.NewToolResultText(string(data)), nil
}

func handleListDir(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, _ := request.GetArguments()["path"].(string)

	entries, err := tools.ListDir(path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	data, _ := json.Marshal(map[string]any{"entries": entries})
	return mcp.NewToolResultText(string(data)), nil
}
