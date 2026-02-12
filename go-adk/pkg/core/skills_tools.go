package core

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go-adk/pkg/skills"
)

// SkillsTool provides skill discovery and loading functionality
type SkillsTool struct {
	SkillsDirectory string
}

// NewSkillsTool creates a new SkillsTool
func NewSkillsTool(skillsDirectory string) *SkillsTool {
	return &SkillsTool{SkillsDirectory: skillsDirectory}
}

// Execute executes the skills tool command
func (t *SkillsTool) Execute(ctx context.Context, command string) (string, error) {
	if command == "" {
		// Return list of available skills
		discoveredSkills, err := skills.DiscoverSkills(t.SkillsDirectory)
		if err != nil {
			return "", fmt.Errorf("failed to discover skills: %w", err)
		}
		return skills.GenerateSkillsToolDescription(discoveredSkills), nil
	}

	// Load specific skill content
	content, err := skills.LoadSkillContent(t.SkillsDirectory, command)
	if err != nil {
		return "", err
	}
	return content, nil
}

// BashTool provides shell command execution in skills context
type BashTool struct {
	SkillsDirectory string
}

// NewBashTool creates a new BashTool
func NewBashTool(skillsDirectory string) *BashTool {
	return &BashTool{SkillsDirectory: skillsDirectory}
}

// Execute executes a bash command in the skills context
func (t *BashTool) Execute(ctx context.Context, command string, sessionID string) (string, error) {
	// Get session path for working directory
	sessionPath, err := skills.GetSessionPath(sessionID, t.SkillsDirectory)
	if err != nil {
		return "", fmt.Errorf("failed to get session path: %w", err)
	}

	return skills.ExecuteCommand(ctx, command, sessionPath)
}

// FileTools provides file operation tools
type FileTools struct{}

// ReadFile reads a file with line numbers
func (ft *FileTools) ReadFile(path string, offset, limit int) (string, error) {
	return skills.ReadFileContent(path, offset, limit)
}

// WriteFile writes content to a file
func (ft *FileTools) WriteFile(path string, content string) error {
	return skills.WriteFileContent(path, content)
}

// EditFile performs an exact string replacement in a file
func (ft *FileTools) EditFile(path string, oldString, newString string, replaceAll bool) error {
	return skills.EditFileContent(path, oldString, newString, replaceAll)
}

// InitializeSessionPath initializes a session's working directory with skills symlink
func InitializeSessionPath(sessionID, skillsDirectory string) (string, error) {
	return skills.GetSessionPath(sessionID, skillsDirectory)
}
