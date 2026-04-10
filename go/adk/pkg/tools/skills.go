package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	skillruntime "github.com/kagent-dev/kagent/go/adk/pkg/skills"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

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
)

type skillsInput struct {
	Command string `json:"command"`
}

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

func NewSkillsTools(skillsDirectory string) ([]tool.Tool, error) {
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

	discoveredSkills, err := skillruntime.DiscoverSkills(absSkillsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to discover skills: %w", err)
	}
	commandExecutor, err := skillruntime.NewCommandExecutorFromEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to configure bash sandbox: %w", err)
	}

	skillsTool, err := functiontool.New(functiontool.Config{
		Name:        "skills",
		Description: skillruntime.GenerateSkillsToolDescription(discoveredSkills),
	}, func(ctx tool.Context, in skillsInput) (string, error) {
		skillName := strings.TrimSpace(in.Command)
		if skillName == "" {
			return "Error: No skill name provided", nil
		}

		content, err := skillruntime.LoadSkillContent(absSkillsDir, skillName)
		if err != nil {
			return fmt.Sprintf("Error loading skill '%s': %v", skillName, err), nil
		}

		return fmt.Sprintf(
			"<command-message>The %q skill is loading</command-message>\n\nBase directory for this skill: %s\n\n%s\n\n---\nThe skill has been loaded. Follow the instructions above and use the bash tool to execute commands.",
			skillName,
			filepath.Join(absSkillsDir, skillName),
			content,
		), nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create skills tool: %w", err)
	}

	readFileTool, err := functiontool.New(functiontool.Config{
		Name:        "read_file",
		Description: readFileDescription,
	}, func(ctx tool.Context, in readFileInput) (string, error) {
		path, err := resolveReadPath(ctx.SessionID(), absSkillsDir, in.FilePath)
		if err != nil {
			return fmt.Sprintf("Error reading file %s: %v", strings.TrimSpace(in.FilePath), err), nil
		}

		content, err := skillruntime.ReadFileContent(path, in.Offset, in.Limit)
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
	}, func(ctx tool.Context, in writeFileInput) (string, error) {
		path, err := resolveWritePath(ctx.SessionID(), absSkillsDir, in.FilePath)
		if err != nil {
			return fmt.Sprintf("Error writing file %s: %v", strings.TrimSpace(in.FilePath), err), nil
		}

		if err := skillruntime.WriteFileContent(path, in.Content); err != nil {
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
	}, func(ctx tool.Context, in editFileInput) (string, error) {
		path, err := resolveEditPath(ctx.SessionID(), absSkillsDir, in.FilePath)
		if err != nil {
			return fmt.Sprintf("Error editing file %s: %v", strings.TrimSpace(in.FilePath), err), nil
		}

		if err := skillruntime.EditFileContent(path, in.OldString, in.NewString, in.ReplaceAll); err != nil {
			return fmt.Sprintf("Error editing file %s: %v", strings.TrimSpace(in.FilePath), err), nil
		}
		return fmt.Sprintf("Successfully edited file: %s", path), nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create edit_file tool: %w", err)
	}

	bashTool, err := functiontool.New(functiontool.Config{
		Name:        "bash",
		Description: bashDescription,
	}, func(ctx tool.Context, in bashInput) (string, error) {
		command := strings.TrimSpace(in.Command)
		if command == "" {
			return "Error: No command provided", nil
		}

		sessionPath, err := skillruntime.GetSessionPath(ctx.SessionID(), absSkillsDir)
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

	return []tool.Tool{skillsTool, readFileTool, writeFileTool, editFileTool, bashTool}, nil
}

func resolveReadPath(sessionID, skillsDirectory, requestedPath string) (string, error) {
	sessionPath, err := skillruntime.GetSessionPath(sessionID, skillsDirectory)
	if err != nil {
		return "", err
	}

	candidate, err := resolveRequestedPath(sessionPath, requestedPath)
	if err != nil {
		return "", err
	}

	resolvedCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}

	sessionRoot, err := filepath.Abs(sessionPath)
	if err != nil {
		return "", err
	}
	skillsRoot, err := filepath.EvalSymlinks(skillsDirectory)
	if err != nil {
		return "", err
	}

	if !isWithinRoot(resolvedCandidate, sessionRoot) && !isWithinRoot(resolvedCandidate, skillsRoot) {
		return "", fmt.Errorf("path %q is outside the allowed roots", requestedPath)
	}

	return resolvedCandidate, nil
}

func resolveEditPath(sessionID, skillsDirectory, requestedPath string) (string, error) {
	sessionPath, err := skillruntime.GetSessionPath(sessionID, skillsDirectory)
	if err != nil {
		return "", err
	}

	candidate, err := resolveRequestedPath(sessionPath, requestedPath)
	if err != nil {
		return "", err
	}

	resolvedCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}

	sessionRoot, err := filepath.Abs(sessionPath)
	if err != nil {
		return "", err
	}
	if !isWithinRoot(resolvedCandidate, sessionRoot) {
		return "", fmt.Errorf("path %q is outside the writable session directory", requestedPath)
	}

	return resolvedCandidate, nil
}

func resolveWritePath(sessionID, skillsDirectory, requestedPath string) (string, error) {
	sessionPath, err := skillruntime.GetSessionPath(sessionID, skillsDirectory)
	if err != nil {
		return "", err
	}

	candidate, err := resolveRequestedPath(sessionPath, requestedPath)
	if err != nil {
		return "", err
	}

	resolvedCandidate, err := resolvePathWithExistingParents(candidate)
	if err != nil {
		return "", err
	}

	sessionRoot, err := filepath.Abs(sessionPath)
	if err != nil {
		return "", err
	}
	if !isWithinRoot(resolvedCandidate, sessionRoot) {
		return "", fmt.Errorf("path %q is outside the writable session directory", requestedPath)
	}

	return resolvedCandidate, nil
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

func isWithinRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}
