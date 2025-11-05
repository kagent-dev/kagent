// Package generator provides shared utilities for project code generation.
package generator

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

// ProjectConfig defines the interface that project configuration must implement.
// This allows both agent and MCP configs to use the same base generator.
type ProjectConfig interface {
	// GetDirectory returns the target directory for project generation
	GetDirectory() string

	// IsVerbose returns whether verbose logging is enabled
	IsVerbose() bool

	// ShouldInitGit returns whether to initialize a git repository
	ShouldInitGit() bool

	// ShouldSkipPath returns whether to skip a specific template path
	ShouldSkipPath(path string) bool
}

// BaseGenerator provides common functionality for all project generators.
// It handles template walking, rendering, and file creation with consistent
// error handling and logging across both agent and MCP generators.
type BaseGenerator struct {
	// TemplateFiles is the embedded filesystem containing template files
	TemplateFiles fs.FS

	// TemplateRoot is the root directory within TemplateFiles (usually "templates")
	TemplateRoot string
}

// NewBaseGenerator creates a new base generator with the specified configuration.
func NewBaseGenerator(templateFiles fs.FS, templateRoot string) *BaseGenerator {
	if templateRoot == "" {
		templateRoot = "templates"
	}
	return &BaseGenerator{
		TemplateFiles: templateFiles,
		TemplateRoot:  templateRoot,
	}
}

// GenerateProject generates a new project using the provided templates.
// It walks through all template files, renders them with the config data,
// and writes the results to the target directory.
func (g *BaseGenerator) GenerateProject(config ProjectConfig) error {
	// Get templates subdirectory
	templateRoot, err := fs.Sub(g.TemplateFiles, g.TemplateRoot)
	if err != nil {
		return fmt.Errorf("failed to get templates subdirectory: %w", err)
	}

	// Walk through all template files
	err = fs.WalkDir(templateRoot, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Allow config to skip specific paths (e.g., tool templates, mcp_server dirs)
		if config.ShouldSkipPath(path) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		// Determine destination path by removing .tmpl extension
		destPath := filepath.Join(config.GetDirectory(), strings.TrimSuffix(path, ".tmpl"))

		// Handle directories
		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		// Read template file
		templateContent, err := fs.ReadFile(templateRoot, path)
		if err != nil {
			return fmt.Errorf("failed to read template file %s: %w", path, err)
		}

		// Render template content
		renderedContent, err := g.RenderTemplate(string(templateContent), config)
		if err != nil {
			return fmt.Errorf("failed to render template for %s: %w", path, err)
		}

		// Create file
		if err := os.WriteFile(destPath, []byte(renderedContent), 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", destPath, err)
		}

		// Log if verbose
		if config.IsVerbose() {
			fmt.Printf("  Generated: %s\n", destPath)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk templates: %w", err)
	}

	// Initialize git repository if requested
	if config.ShouldInitGit() {
		if err := g.initGitRepo(config.GetDirectory(), config.IsVerbose()); err != nil {
			// Don't fail the whole operation if git init fails
			if config.IsVerbose() {
				fmt.Printf("Warning: failed to initialize git repository: %v\n", err)
			}
		}
	}

	return nil
}

// RenderTemplate renders a template string with the provided data.
// This is the core template rendering logic used by all generators.
func (g *BaseGenerator) RenderTemplate(tmplContent string, data interface{}) (string, error) {
	tmpl, err := template.New("template").Parse(tmplContent)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return result.String(), nil
}

// initGitRepo initializes a git repository in the specified directory.
func (g *BaseGenerator) initGitRepo(dir string, verbose bool) error {
	cmd := exec.Command("git", "init")
	cmd.Dir = dir

	if verbose {
		fmt.Printf("  Initializing git repository...\n")
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run git init: %w", err)
	}

	return nil
}

// ReadTemplateFile reads a template file from the embedded filesystem.
// This is a convenience method for reading individual template files.
func (g *BaseGenerator) ReadTemplateFile(templatePath string) ([]byte, error) {
	fullPath := filepath.Join(g.TemplateRoot, templatePath)
	return fs.ReadFile(g.TemplateFiles, fullPath)
}
