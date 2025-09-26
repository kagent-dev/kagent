package common

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/stoewer/go-strcase"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/kagent-dev/kagent/go/cli/internal/mcp"
)

// Base Generator for MCP projects
type BaseGenerator struct {
	TemplateFiles    fs.FS
	ToolTemplateName string
}

// GenerateProject generates a new project
func (g *BaseGenerator) GenerateProject(config mcp.ProjectConfig) error {

	templateRoot, err := fs.Sub(g.TemplateFiles, "templates")
	if err != nil {
		return fmt.Errorf("failed to get templates subdirectory: %w", err)
	}

	err = fs.WalkDir(templateRoot, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip tool.*.tmpl during project generation - it's for individual tool generation
		if path == g.ToolTemplateName {
			return nil
		}

		destPath := filepath.Join(config.Directory, strings.TrimSuffix(path, ".tmpl"))

		if d.IsDir() {
			// Create the directory if it doesn't exist
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", destPath, err)
			}
			return nil
		}

		// Read template file
		templateContent, err := fs.ReadFile(templateRoot, path)
		if err != nil {
			return fmt.Errorf("failed to read template file %s: %w", path, err)
		}

		// Render template content
		renderedContent, err := renderTemplate(string(templateContent), config)
		if err != nil {
			return fmt.Errorf("failed to render template for %s: %w", path, err)
		}

		// Create file
		if err := os.WriteFile(destPath, []byte(renderedContent), 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", destPath, err)
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk templates: %w", err)
	}

	// Initialize git repository
	if !config.NoGit {
		if err := g.initGitRepo(config.Directory, config.Verbose); err != nil {
			// Don't fail the whole operation if git init fails
			if config.Verbose {
				fmt.Printf("Warning: failed to initialize git repository: %v\n", err)
			}
		}
	}

	return nil
}

// GenerateTool generates a new tool for a project.
func (g *BaseGenerator) GenerateTool(projectRoot string, config mcp.ToolConfig) error {

	templateRoot, err := fs.Sub(g.TemplateFiles, "templates")
	if err != nil {
		return fmt.Errorf("failed to get templates subdirectory: %w", err)
	}

	return fs.WalkDir(templateRoot, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Only generate tool.*.tmpl during tool generation
		if path != g.ToolTemplateName {
			return nil
		}

		toolNameSnakeCase := strcase.SnakeCase(config.ToolName)

		destPath := filepath.Join(
			projectRoot,
			filepath.Dir(path),
			toolNameSnakeCase+filepath.Ext(strings.TrimSuffix(path, ".tmpl")),
		)

		if d.IsDir() {
			// Create the directory if it doesn't exist
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", destPath, err)
			}
			return nil
		}

		return g.GenerateToolFile(destPath, config)
	})
}

// GenerateToolFile generates a new tool file from the unified template
func (g *BaseGenerator) GenerateToolFile(filePath string, config mcp.ToolConfig) error {
	// Prepare template data
	toolName := config.ToolName
	toolNamePascalCase := cases.Title(language.English).String(toolName)
	toolNameCamelCase := strcase.LowerCamelCase(toolName)
	data := map[string]interface{}{
		"ToolName":           toolName,
		"ToolNameCamelCase":  toolNameCamelCase,
		"ToolNameTitle":      cases.Title(language.English).String(toolName),
		"ToolNameUpper":      strings.ToUpper(toolName),
		"ToolNameLower":      strings.ToLower(toolName),
		"ToolNamePascalCase": toolNamePascalCase,
		"ClassName":          cases.Title(language.English).String(toolName) + "Tool",
		"Description":        config.Description,
	}

	// Create the directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Parse and execute the template
	templateContent, err := fs.ReadFile(g.TemplateFiles, filepath.Join("templates", g.ToolTemplateName))
	if err != nil {
		return fmt.Errorf("failed to read tool template: %w", err)
	}

	tmpl, err := template.New("tool").Parse(string(templateContent))
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	// Create the output file
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	// Execute the template
	err = tmpl.Execute(file, data)

	// Close the file and check for errors
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

// initGitRepo initializes a git repository in the specified directory
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

// renderTemplate renders a template string with the provided data
func renderTemplate(tmplContent string, data interface{}) (string, error) {
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
