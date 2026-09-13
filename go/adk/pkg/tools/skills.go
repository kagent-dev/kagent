package tools

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// enableFileSearchToolsEnv gates the list_files and grep_file tools, which
// are opt-in (disabled by default): they let an agent enumerate and search
// the filesystem under its session/skills roots without invoking a shell, so
// deployments that want to grant that visibility deliberately can, rather
// than having it enabled implicitly.
//
// Also registered (separately, for `kagent env` CLI discoverability only,
// not read here) as KagentEnableFileSearchTools in go/core/pkg/env/kagent.go.
// TestEnableFileSearchToolsEnvMatchesRegistry pins the two literals together
// so they cannot drift.
const enableFileSearchToolsEnv = "KAGENT_ENABLE_FILE_SEARCH_TOOLS"

// fileSearchToolsEnabled accepts the same case-insensitive true-values as
// Python's file_search_tools_enabled() (kagent-skills/shell.py), so the
// same literal env var value behaves identically in either runtime rather
// than relying on Go's strconv.ParseBool grammar, which Python doesn't
// replicate exactly (e.g. ParseBool requires the exact casing "True", not
// "tRue").
func fileSearchToolsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(enableFileSearchToolsEnv))) {
	case "1", "t", "true":
		return true
	default:
		return false
	}
}

const (
	readFileDescription = `Reads a file from the filesystem with line numbers.

Usage:
- Provide a path to the file (absolute or relative to your working directory)
- Returns content with line numbers (format: LINE_NUMBER|CONTENT)
- Optional offset and limit parameters for reading specific line ranges
- Lines longer than 2000 characters are truncated
- Always read a file before editing it
- You can read from skills/ directory, uploads/, outputs/, or any file in your session`

	writeFileDescription = `Writes content to a file on the filesystem.

Usage:
- Provide a path (absolute or relative to working directory) and content to write
- Overwrites existing files
- Creates parent directories if needed
- For existing files, read them first using read_file
- Prefer editing existing files over writing new ones
- You can write to your working directory, outputs/, or any writable location
- Note: skills/ directory is read-only`

	editFileDescription = `Performs exact string replacements in files.

Usage:
- You must read the file first using read_file
- Provide path (absolute or relative to working directory)
- When editing, preserve exact indentation from the file content
- Do NOT include line number prefixes in old_string or new_string
- old_string must be unique unless replace_all=true
- Use replace_all to rename variables/strings throughout the file
- old_string and new_string must be different
- Note: skills/ directory is read-only`

	listFilesDescription = `Lists files and directories at a given path.

Usage:
- Provide a path (absolute or relative to your working directory); defaults to the working directory
- Directories are listed with a trailing "/"; files are followed by their size in bytes
- You can list skills/ directory, uploads/, outputs/, or any directory in your session`

	grepFileDescription = `Searches for a regular expression pattern in a file or directory.

Usage:
- Provide a pattern and a path (absolute or relative to your working directory)
- Set recursive=true to search all files under a directory path
- Recursion does not follow symlinked subdirectories (e.g. skills/ is a symlink) -
  point path directly at skills/ to search inside it
- Set ignore_case=true for case-insensitive matching
- Returns matching lines as path:line_number:content
- You can search the skills/ directory, uploads/, outputs/, or any file/directory in your session`

	bashDescription = `Execute bash commands in the skills environment with sandbox protection.

Working Directory & Structure:
- Commands run in a temporary session directory: /tmp/kagent/{session_id}/
- /skills -> All skills are available here (read-only).
- Your current working directory and /skills are added to PYTHONPATH.

Python Imports (CRITICAL):
- To import from a skill, use the name of the skill.
  Example: from skills_name.module import function
- If the skills name contains a dash '-', you need to use importlib to import it.
  Example:
    import importlib
    skill_module = importlib.import_module('skill-name.module')

For file operations:
- Use read_file, write_file, and edit_file for interacting with the filesystem.

Timeouts:
- python scripts: 60s
- other commands: 30s`

	// fileSearchToolsBashHint is appended to bashDescription only when
	// list_files/grep_file are enabled, so bash's own description doesn't
	// point the model at tools that aren't registered. Appended as a
	// trailing paragraph rather than interpolated into bashDescription, so
	// the long, free-form prose above stays a plain string -- not a format
	// template where a stray '%' added later could silently corrupt output.
	fileSearchToolsBashHint = "\nAlso available: list_files and grep_file, for exploring the filesystem without a full shell command."
)

type bashInput struct {
	Command     string `json:"command"`
	Description string `json:"description,omitempty"`
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

// NewSkillExecutionTools creates the filesystem and shell tools used to execute
// skills. Skill discovery and loading are provided by Go ADK's skilltoolset.
func NewSkillExecutionTools(skillsDirectory string) ([]tool.Tool, error) {
	skillsDirectory = strings.TrimSpace(skillsDirectory)
	if skillsDirectory == "" {
		return nil, nil
	}

	absSkillsDir, err := filepath.Abs(skillsDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve skills directory %q: %w", skillsDirectory, err)
	}
	if _, err := os.Stat(absSkillsDir); err != nil {
		return nil, fmt.Errorf("failed to access skills directory %q: %w", absSkillsDir, err)
	}

	commandExecutor := NewCommandExecutor()

	readFileTool, err := functiontool.New(functiontool.Config{
		Name:        "read_file",
		Description: readFileDescription,
	}, func(ctx adkagent.Context, in readFileInput) (string, error) {
		path, err := resolveReadPath(ctx.SessionID(), absSkillsDir, in.FilePath)
		if err != nil {
			return fmt.Sprintf("Error reading file %s: %v", strings.TrimSpace(in.FilePath), err), nil
		}

		content, err := ReadFileContent(path, in.Offset, in.Limit)
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
		path, err := resolveWritePath(ctx.SessionID(), absSkillsDir, in.FilePath)
		if err != nil {
			return fmt.Sprintf("Error writing file %s: %v", strings.TrimSpace(in.FilePath), err), nil
		}

		if err := WriteFileContent(path, in.Content); err != nil {
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
		path, err := resolveEditPath(ctx.SessionID(), absSkillsDir, in.FilePath)
		if err != nil {
			return fmt.Sprintf("Error editing file %s: %v", strings.TrimSpace(in.FilePath), err), nil
		}

		if err := EditFileContent(path, in.OldString, in.NewString, in.ReplaceAll); err != nil {
			return fmt.Sprintf("Error editing file %s: %v", strings.TrimSpace(in.FilePath), err), nil
		}
		return fmt.Sprintf("Successfully edited file: %s", path), nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create edit_file tool: %w", err)
	}

	tools := []tool.Tool{readFileTool, writeFileTool, editFileTool}

	// list_files/grep_file are opt-in: they give an agent broad filesystem
	// visibility, so deployments enable them deliberately. Note this gate is
	// theirs alone -- bash below is always registered.
	fileSearchEnabled := fileSearchToolsEnabled()
	if fileSearchEnabled {
		listFilesTool, err := functiontool.New(functiontool.Config{
			Name:        "list_files",
			Description: listFilesDescription,
		}, func(ctx adkagent.Context, in listFilesInput) (string, error) {
			requestedPath := in.Path
			if strings.TrimSpace(requestedPath) == "" {
				requestedPath = "."
			}

			path, err := resolveReadPath(ctx.SessionID(), absSkillsDir, requestedPath)
			if err != nil {
				return fmt.Sprintf("Error listing %s: %v", requestedPath, err), nil
			}

			content, err := ListDirContent(path)
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

			path, err := resolveReadPath(ctx.SessionID(), absSkillsDir, in.Path)
			if err != nil {
				return fmt.Sprintf("Error searching %s: %v", strings.TrimSpace(in.Path), err), nil
			}

			// ctx is the ADK tool context, which embeds context.Context, so a
			// recursive search is abortable by whatever deadline or
			// cancellation the caller already set -- and the walk, not the
			// match, is the part that can run long here.
			//
			// No fixed deadline is added on top of that. The Python runtime
			// caps grep at 30s, but that bound exists for a hazard Go doesn't
			// have: Python's `re` backtracks, so an adversarial pattern can
			// hang on a single line, while RE2 is linear-time. Since only the
			// walk is unbounded, and its cost scales with the tree the
			// deployment itself provisioned, a fixed cap here would break
			// large legitimate searches without closing a distinct risk.
			content, err := GrepContent(ctx, path, in.Pattern, in.Recursive, in.IgnoreCase)
			if err != nil {
				return fmt.Sprintf("Error searching %s: %v", strings.TrimSpace(in.Path), err), nil
			}
			return content, nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create grep_file tool: %w", err)
		}

		tools = append(tools, listFilesTool, grepFileTool)
	} else {
		slog.Debug("omitting list_files/grep_file tools: " + enableFileSearchToolsEnv + " not enabled")
	}

	desc := bashDescription
	if fileSearchEnabled {
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

		sessionPath, err := GetSessionPath(ctx.SessionID(), absSkillsDir)
		if err != nil {
			return fmt.Sprintf("Error executing command %q: %v", command, err), nil
		}

		result, err := commandExecutor.ExecuteCommand(ctx, command, sessionPath)
		if err != nil {
			return fmt.Sprintf("Error executing command %q: %v", command, err), nil
		}
		return result, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create bash tool: %w", err)
	}
	tools = append(tools, bashTool)

	return tools, nil
}

// pathPolicy is the sandbox contract for one class of filesystem access.
// The three resolvers below are the same code differing only in these
// fields, so they are expressed as data rather than as flags threaded
// through a shared function.
type pathPolicy struct {
	// resolve maps the requested leaf to a real path. EvalSymlinks requires
	// it to already exist; resolvePathWithExistingParents tolerates a
	// not-yet-created leaf, which only writes need.
	resolve func(string) (string, error)
	// allowSkillsRoot lets the read-only skills directory count as an allowed
	// root. Reads may reach it; edits and writes must not, or an agent could
	// modify the skills it was given.
	allowSkillsRoot bool
	// denied names the boundary in the error an out-of-bounds path gets. It
	// is its own field rather than being derived from allowSkillsRoot: the
	// two happen to correlate today, but a resolver that denied the skills
	// root for a reason other than writability would otherwise be described
	// wrongly.
	denied string
}

// TestResolvePathContainment pins this matrix for all three policies.
var (
	readPolicy  = pathPolicy{filepath.EvalSymlinks, true, "the allowed roots"}
	editPolicy  = pathPolicy{filepath.EvalSymlinks, false, "the writable session directory"}
	writePolicy = pathPolicy{resolvePathWithExistingParents, false, "the writable session directory"}
)

// resolveSandboxedPath maps a requested path onto the session directory,
// resolves it under policy, and then requires the result to land inside an
// allowed root -- the check that keeps an agent inside its sandbox.
func resolveSandboxedPath(sessionID, skillsDirectory, requestedPath string, policy pathPolicy) (string, error) {
	sessionPath, err := GetSessionPath(sessionID, skillsDirectory)
	if err != nil {
		return "", err
	}

	candidate, err := resolveRequestedPath(sessionPath, requestedPath)
	if err != nil {
		return "", err
	}

	resolvedCandidate, err := policy.resolve(candidate)
	if err != nil {
		return "", err
	}

	sessionRoot, err := filepath.EvalSymlinks(sessionPath)
	if err != nil {
		return "", err
	}
	roots := []string{sessionRoot}

	if policy.allowSkillsRoot {
		// Resolved eagerly rather than only when the session root misses, so
		// an unresolvable skills directory still surfaces as an error the way
		// it did before these three were merged into one function.
		skillsRoot, err := filepath.EvalSymlinks(skillsDirectory)
		if err != nil {
			return "", err
		}
		roots = append(roots, skillsRoot)
	}

	for _, root := range roots {
		if WithinRoot(resolvedCandidate, root) {
			return resolvedCandidate, nil
		}
	}

	return "", fmt.Errorf("path %q is outside %s", requestedPath, policy.denied)
}

func resolveReadPath(sessionID, skillsDirectory, requestedPath string) (string, error) {
	return resolveSandboxedPath(sessionID, skillsDirectory, requestedPath, readPolicy)
}

func resolveEditPath(sessionID, skillsDirectory, requestedPath string) (string, error) {
	return resolveSandboxedPath(sessionID, skillsDirectory, requestedPath, editPolicy)
}

func resolveWritePath(sessionID, skillsDirectory, requestedPath string) (string, error) {
	return resolveSandboxedPath(sessionID, skillsDirectory, requestedPath, writePolicy)
}

func resolveRequestedPath(basePath, requestedPath string) (string, error) {
	requestedPath = strings.TrimSpace(requestedPath)
	if requestedPath == "" {
		return "", fmt.Errorf("no file path provided")
	}

	candidate := requestedPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(basePath, candidate)
	}
	return filepath.Abs(candidate)
}

func resolvePathWithExistingParents(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	current := absPath
	for {
		if _, err := os.Lstat(current); err == nil {
			resolvedBase, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			if current == absPath {
				return filepath.Clean(resolvedBase), nil
			}

			relativeSuffix, err := filepath.Rel(current, absPath)
			if err != nil {
				return "", err
			}
			return filepath.Clean(filepath.Join(resolvedBase, relativeSuffix)), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("failed to resolve path %q", path)
		}
		current = parent
	}
}
