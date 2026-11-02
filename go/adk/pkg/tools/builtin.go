package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

const (
	readFileDescription = `Reads a file from the filesystem with line numbers.

Usage:
- Provide a path to the file (absolute or relative to your working directory)
- Returns content with line numbers (format: LINE_NUMBER|CONTENT)
- Optional offset and limit parameters; reads default to 2000 lines
- Results are capped at 64 KiB; use offset to continue a truncated read
- Lines longer than 2000 characters are truncated
- Always read a file before editing it
- Paths are relative to /data/workspace unless absolute`

	writeFileDescription = `Writes content to a file on the filesystem.

Usage:
- Provide a path (absolute or relative to working directory) and content to write
- Overwrites existing files
- Creates parent directories if needed
- For existing files, read them first using read_file
- Prefer editing existing files over writing new ones
- You can write to any writable location in the sandbox`

	editFileDescription = `Performs exact string replacements in files.

Usage:
- You must read the file first using read_file
- Provide path (absolute or relative to working directory)
- When editing, preserve exact indentation from the file content
- Do NOT include line number prefixes in old_string or new_string
- old_string must be unique unless replace_all=true
- Use replace_all to rename variables/strings throughout the file
- old_string and new_string must be different
- Paths are relative to /data/workspace unless absolute`

	listFilesDescription = `Lists files and directories at a given path.

Usage:
- Provide a path (absolute or relative to your working directory); defaults to the working directory
- Results are capped at 64 KiB; narrow the path for large directories
- Directories are listed with a trailing "/"; files are followed by their size in bytes
- Paths are relative to /data/workspace unless absolute`

	grepFileDescription = `Searches for a regular expression pattern in a file or directory.

Usage:
- Provide a pattern and a path (absolute or relative to your working directory)
- Set recursive=true to search all files under a directory path
- Recursion does not follow symlinked subdirectories; target a symlink directly to search it
- Set ignore_case=true for case-insensitive matching
- Returns matching lines as path:line_number:content, capped at 64 KiB
- Paths are relative to /data/workspace unless absolute`

	bashDescription = `Execute a bash command inside the agent sandbox.

- Working directory: /data/workspace. Files there survive suspend/resume and checkpoints.
- Each call starts a new shell; use cd inside a command or absolute paths.
- Default timeout: 60 seconds. Set timeout_seconds up to 600 for longer commands.
- Returns combined stdout and stderr, capped at 64 KiB with a truncation marker.
- Redirect large output to a file if you need to inspect it in full.
- Child processes are stopped when the call finishes or is canceled.`

	// Mention search tools only when they are registered.
	fileSearchToolsBashHint = "\nAlso available: list_files and grep_file, for exploring the filesystem without a full shell command."
)

type bashInput struct {
	Command        string `json:"command"`
	Description    string `json:"description,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type readFileInput struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type writeFileInput struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

type editFileInput struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type listFilesInput struct {
	Path string `json:"path,omitempty"`
}

type grepFileInput struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path"`
	Recursive  bool   `json:"recursive,omitempty"`
	IgnoreCase bool   `json:"ignore_case,omitempty"`
}

// BuiltinToolsConfig configures tools in a Substrate Actor. Workspace defaults to
// the Actor's durable workspace; isolation is provided by the sandbox.
type BuiltinToolsConfig struct {
	Workspace         string
	SkillsDirectory   string
	BashVenvPath      string
	DisableFileSearch bool
	DisableBash       bool
}

// NewBuiltinTools creates filesystem and shell tools independently of skills.
func NewBuiltinTools(cfg BuiltinToolsConfig) ([]tool.Tool, error) {
	workspace := cfg.Workspace
	if workspace == "" {
		workspace = "/data/workspace"
	}
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve workspace: %w", err)
	}
	if cfg.SkillsDirectory != "" {
		cfg.SkillsDirectory, err = filepath.Abs(cfg.SkillsDirectory)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve skills directory: %w", err)
		}
	}
	// Create lazily on invocation so tool discovery does not touch the filesystem.
	prepareWorkspace := func() error { return os.MkdirAll(workspace, 0755) }

	readFileTool, err := functiontool.New(functiontool.Config{
		Name:        "read_file",
		Description: readFileDescription,
	}, func(ctx adkagent.Context, in readFileInput) (string, error) {
		if err := prepareWorkspace(); err != nil {
			return "Error: " + err.Error(), nil
		}
		path, err := resolveToolPath(workspace, in.FilePath)
		if err != nil {
			return fmt.Sprintf("Error reading file %s: %v", strings.TrimSpace(in.FilePath), err), nil
		}

		content, err := readFileContent(ctx, path, in.Offset, in.Limit)
		if err != nil {
			return fmt.Sprintf("Error reading file %s: %v", strings.TrimSpace(in.FilePath), err), nil
		}
		return content, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create read_file tool: %w", err)
	}

	writeFileTool, err := functiontool.New(functiontool.Config{
		Name:        "write_file",
		Description: writeFileDescription,
	}, func(ctx adkagent.Context, in writeFileInput) (string, error) {
		if err := prepareWorkspace(); err != nil {
			return "Error: " + err.Error(), nil
		}
		path, err := resolveToolPath(workspace, in.FilePath)
		if err != nil {
			return fmt.Sprintf("Error writing file %s: %v", strings.TrimSpace(in.FilePath), err), nil
		}

		if err := writeFileContent(path, in.Content); err != nil {
			return fmt.Sprintf("Error writing file %s: %v", strings.TrimSpace(in.FilePath), err), nil
		}
		return fmt.Sprintf("Successfully wrote file: %s", path), nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create write_file tool: %w", err)
	}

	editFileTool, err := functiontool.New(functiontool.Config{
		Name:        "edit_file",
		Description: editFileDescription,
	}, func(ctx adkagent.Context, in editFileInput) (string, error) {
		if err := prepareWorkspace(); err != nil {
			return "Error: " + err.Error(), nil
		}
		path, err := resolveToolPath(workspace, in.FilePath)
		if err != nil {
			return fmt.Sprintf("Error editing file %s: %v", strings.TrimSpace(in.FilePath), err), nil
		}

		if err := editFileContent(path, in.OldString, in.NewString, in.ReplaceAll); err != nil {
			return fmt.Sprintf("Error editing file %s: %v", strings.TrimSpace(in.FilePath), err), nil
		}
		return fmt.Sprintf("Successfully edited file: %s", path), nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create edit_file tool: %w", err)
	}

	tools := []tool.Tool{readFileTool, writeFileTool, editFileTool}

	// File search tools are available unless explicitly disabled.
	if !cfg.DisableFileSearch {
		listFilesTool, err := functiontool.New(functiontool.Config{
			Name:        "list_files",
			Description: listFilesDescription,
		}, func(ctx adkagent.Context, in listFilesInput) (string, error) {
			requestedPath := in.Path
			if strings.TrimSpace(requestedPath) == "" {
				requestedPath = "."
			}

			if err := prepareWorkspace(); err != nil {
				return "Error: " + err.Error(), nil
			}
			path, err := resolveToolPath(workspace, requestedPath)
			if err != nil {
				return fmt.Sprintf("Error listing %s: %v", requestedPath, err), nil
			}

			content, err := listDirContent(ctx, path)
			if err != nil {
				return fmt.Sprintf("Error listing %s: %v", requestedPath, err), nil
			}
			return content, nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create list_files tool: %w", err)
		}

		grepFileTool, err := functiontool.New(functiontool.Config{
			Name:        "grep_file",
			Description: grepFileDescription,
		}, func(ctx adkagent.Context, in grepFileInput) (string, error) {
			if strings.TrimSpace(in.Pattern) == "" {
				return "Error: No pattern provided", nil
			}
			if strings.TrimSpace(in.Path) == "" {
				return "Error: No file path provided", nil
			}

			if err := prepareWorkspace(); err != nil {
				return "Error: " + err.Error(), nil
			}
			path, err := resolveToolPath(workspace, in.Path)
			if err != nil {
				return fmt.Sprintf("Error searching %s: %v", strings.TrimSpace(in.Path), err), nil
			}

			// Go regex matching is linear-time; use the invocation cancellation.
			content, err := grepContent(ctx, path, in.Pattern, in.Recursive, in.IgnoreCase)
			if err != nil {
				return fmt.Sprintf("Error searching %s: %v", strings.TrimSpace(in.Path), err), nil
			}
			return content, nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create grep_file tool: %w", err)
		}

		tools = append(tools, listFilesTool, grepFileTool)
	}

	if cfg.DisableBash {
		return tools, nil
	}

	desc := bashDescription
	if cfg.SkillsDirectory != "" {
		desc += "\nSkills directory: " + cfg.SkillsDirectory
	}
	if !cfg.DisableFileSearch {
		desc += fileSearchToolsBashHint
	}
	bashTool, err := functiontool.New(functiontool.Config{
		Name:        "bash",
		Description: desc,
	}, func(ctx adkagent.Context, in bashInput) (string, error) {
		command := strings.TrimSpace(in.Command)
		if command == "" {
			return "Error: No command provided", nil
		}

		timeout := defaultCommandTimeout
		if in.TimeoutSeconds < 0 || in.TimeoutSeconds > 600 {
			return "Error: timeout_seconds must be between 0 and 600 (0 uses the 60-second default)", nil
		}
		if in.TimeoutSeconds > 0 {
			timeout = time.Duration(in.TimeoutSeconds) * time.Second
		}
		if err := prepareWorkspace(); err != nil {
			return "Error: " + err.Error(), nil
		}
		result, err := executeCommand(ctx, command, commandConfig{
			directory: workspace, skillsDirectory: cfg.SkillsDirectory,
			bashVenvPath: cfg.BashVenvPath, timeout: timeout,
		})
		if err != nil {
			return fmt.Sprintf("Error: %v\n%s", err, result), nil
		}

		return result, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create bash tool: %w", err)
	}
	tools = append(tools, bashTool)

	return tools, nil
}

// resolveToolPath applies ordinary filesystem semantics inside the sandbox.
func resolveToolPath(workspace, requestedPath string) (string, error) {
	requestedPath = strings.TrimSpace(requestedPath)
	if requestedPath == "" {
		return "", fmt.Errorf("no file path provided")
	}
	if filepath.IsAbs(requestedPath) {
		return filepath.Clean(requestedPath), nil
	}
	return filepath.Join(workspace, requestedPath), nil
}
