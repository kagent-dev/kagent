package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	commonfs "github.com/kagent-dev/kagent/go/core/cli/internal/common/fs"
	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/kagent-dev/kagent/go/core/cli/internal/mcp"
	"github.com/kagent-dev/kagent/go/core/cli/internal/mcp/frameworks"
	"github.com/kagent-dev/kagent/go/core/cli/internal/mcp/manifests"
)

// AddToolCfg contains configuration for MCP add-tool command
type AddToolCfg struct {
	Description string
	Force       bool
	Interactive bool
	ProjectDir  string
}

func AddToolMcp(cfg *AddToolCfg, toolName string) error {
	appCfg, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	// Validate tool name
	if err := validateToolName(toolName); err != nil {
		return fmt.Errorf("invalid tool name: %w", err)
	}

	// Determine project directory
	projectDirectory := cfg.ProjectDir
	if projectDirectory == "" {
		var err error
		projectDirectory, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
	} else {
		// Convert relative path to absolute path
		if !filepath.IsAbs(projectDirectory) {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current directory: %w", err)
			}
			projectDirectory = filepath.Join(cwd, projectDirectory)
		}
	}

	manifestManager := manifests.NewManager(projectDirectory)
	projectManifest, err := manifestManager.Load()
	if err != nil {
		return fmt.Errorf("failed to load project manifest: %w", err)
	}
	framework := projectManifest.Framework

	// Check if tool already exists
	toolPath := filepath.Join("src", "tools", toolName+".py")
	toolExists := commonfs.FileExists(toolPath)

	if appCfg.Verbose {
		fmt.Printf("Tool file path: %s\n", toolPath)
		fmt.Printf("Tool exists: %v\n", toolExists)
	}

	if toolExists && !cfg.Force {
		return fmt.Errorf("tool '%s' already exists. Use --force to overwrite", toolName)
	}

	if cfg.Interactive {
		return createToolInteractive(cfg, toolName, projectDirectory, framework)
	}

	return createTool(cfg, toolName, projectDirectory, framework)
}

func validateToolName(name string) error {
	if name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}

	// Check for valid identifier (works for Python, Go, and TypeScript)
	if !isValidIdentifier(name) {
		return fmt.Errorf("tool name must be a valid identifier")
	}

	// Check for reserved names
	reservedNames := []string{"server", "main", "core", "utils", "init", "test"}
	if slices.Contains(reservedNames, strings.ToLower(name)) {
		return fmt.Errorf("'%s' is a reserved name", name)
	}

	return nil
}

func isValidIdentifier(name string) bool {
	if len(name) == 0 {
		return false
	}

	// First character must be letter or underscore
	firstChar := name[0]
	if firstChar < 'a' || firstChar > 'z' {
		if firstChar < 'A' || firstChar > 'Z' {
			if firstChar != '_' {
				return false
			}
		}
	}

	// Remaining characters must be letters, digits, or underscores
	for i := 1; i < len(name); i++ {
		c := name[i]
		if c < 'a' || c > 'z' {
			if c < 'A' || c > 'Z' {
				if c < '0' || c > '9' {
					if c != '_' {
						return false
					}
				}
			}
		}
	}

	return true
}

func createToolInteractive(cfg *AddToolCfg, toolName, projectRoot, framework string) error {
	fmt.Printf("Creating tool '%s' interactively...\n", toolName)

	// Get tool description
	if cfg.Description == "" {
		fmt.Printf("Enter tool description (optional): ")
		var desc string
		_, err := fmt.Scanln(&desc)
		if err != nil {
			return fmt.Errorf("failed to read description: %w", err)
		}
		cfg.Description = desc
	}

	return generateTool(cfg, toolName, projectRoot, framework)
}

func createTool(cfg *AddToolCfg, toolName, projectRoot, framework string) error {
	appCfg, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}
	if appCfg.Verbose {
		fmt.Printf("Creating tool: %s\n", toolName)
	}

	return generateTool(cfg, toolName, projectRoot, framework)
}

func generateTool(cfg *AddToolCfg, toolName, projectRoot, framework string) error {
	generator, err := frameworks.GetGenerator(framework)
	if err != nil {
		return err
	}

	config := mcp.ToolConfig{
		ToolName:    toolName,
		Description: cfg.Description,
	}

	if err := generator.GenerateTool(projectRoot, config); err != nil {
		return fmt.Errorf("failed to generate tool file: %w", err)
	}

	return nil
}
