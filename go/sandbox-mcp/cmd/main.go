package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/kagent-dev/kagent/go/sandbox-mcp/pkg/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type execInput struct {
	Command    string `json:"command"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
	WorkingDir string `json:"working_dir,omitempty"`
}

type readFileInput struct {
	Path string `json:"path"`
}

type writeFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type listDirInput struct {
	Path string `json:"path,omitempty"`
}

type getSkillInput struct {
	Name string `json:"name"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	skillsDir := os.Getenv("SKILLS_DIR")
	if skillsDir == "" {
		skillsDir = "/skills"
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	s := mcp.NewServer(&mcp.Implementation{
		Name:    "kagent-sandbox-mcp",
		Version: "0.1.0",
	}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "exec",
		Description: "Execute a shell command in the sandbox",
	}, handleExec)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "read_file",
		Description: "Read the content of a file",
	}, handleReadFile)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "write_file",
		Description: "Write content to a file (creates parent directories if needed)",
	}, handleWriteFile)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_dir",
		Description: "List entries in a directory",
	}, handleListDir)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_skill",
		Description: buildGetSkillDescription(skillsDir),
	}, makeHandleGetSkill(skillsDir))

	addr := fmt.Sprintf(":%s", port)
	slog.Info("Starting kagent-sandbox-mcp", "addr", addr, "skillsDir", skillsDir)

	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return s
	}, nil)

	if err := http.ListenAndServe(addr, handler); err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}

func handleExec(_ context.Context, _ *mcp.CallToolRequest, input execInput) (*mcp.CallToolResult, any, error) {
	slog.Info("exec", "command", input.Command, "working_dir", input.WorkingDir, "timeout_ms", input.TimeoutMs)

	result, err := tools.Exec(context.Background(), input.Command, input.TimeoutMs, input.WorkingDir)
	if err != nil {
		slog.Error("exec failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			IsError: true,
		}, nil, nil
	}

	slog.Info("exec completed", "exit_code", result.ExitCode, "stdout_len", len(result.Stdout), "stderr_len", len(result.Stderr))
	if result.Stderr != "" {
		slog.Warn("exec stderr", "stderr", result.Stderr)
	}

	data, _ := json.Marshal(result)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

func handleReadFile(_ context.Context, _ *mcp.CallToolRequest, input readFileInput) (*mcp.CallToolResult, any, error) {
	slog.Info("read_file", "path", input.Path)

	content, err := tools.ReadFile(input.Path)
	if err != nil {
		slog.Error("read_file failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			IsError: true,
		}, nil, nil
	}

	slog.Info("read_file completed", "path", input.Path, "content_len", len(content))
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: content}},
	}, nil, nil
}

func handleWriteFile(_ context.Context, _ *mcp.CallToolRequest, input writeFileInput) (*mcp.CallToolResult, any, error) {
	slog.Info("write_file", "path", input.Path, "content_len", len(input.Content))

	if err := tools.WriteFile(input.Path, input.Content); err != nil {
		slog.Error("write_file failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			IsError: true,
		}, nil, nil
	}

	slog.Info("write_file completed", "path", input.Path)
	data, _ := json.Marshal(map[string]bool{"ok": true})
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

func handleListDir(_ context.Context, _ *mcp.CallToolRequest, input listDirInput) (*mcp.CallToolResult, any, error) {
	slog.Info("list_dir", "path", input.Path)

	entries, err := tools.ListDir(input.Path)
	if err != nil {
		slog.Error("list_dir failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			IsError: true,
		}, nil, nil
	}

	slog.Info("list_dir completed", "path", input.Path, "entries", len(entries))
	data, _ := json.Marshal(map[string]any{"entries": entries})
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

// buildGetSkillDescription builds the get_skill tool description, embedding a
// brief listing of all available skills so the LLM knows which names to request.
func buildGetSkillDescription(skillsDir string) string {
	base := "Load the full content of a skill by name."
	skills, err := tools.ListSkills(skillsDir)
	if err != nil || len(skills) == 0 {
		return base
	}

	desc := base + "\n\nAvailable skills:"
	for _, s := range skills {
		if s.Description != "" {
			desc += fmt.Sprintf("\n- %s: %s", s.Name, s.Description)
		} else {
			desc += fmt.Sprintf("\n- %s", s.Name)
		}
	}
	return desc
}

// makeHandleGetSkill returns a handler that loads a skill by name.
func makeHandleGetSkill(skillsDir string) func(context.Context, *mcp.CallToolRequest, getSkillInput) (*mcp.CallToolResult, any, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, input getSkillInput) (*mcp.CallToolResult, any, error) {
		slog.Info("get_skill", "name", input.Name, "skillsDir", skillsDir)

		content, err := tools.LoadSkill(skillsDir, input.Name)
		if err != nil {
			slog.Error("get_skill failed", "error", err)
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
				IsError: true,
			}, nil, nil
		}

		slog.Info("get_skill completed", "name", input.Name, "content_len", len(content))
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: content}},
		}, nil, nil
	}
}
