package common

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// AgentConfig holds the configuration for agent project generation
type AgentConfig struct {
	Name          string
	Directory     string
	Verbose       bool
	Instruction   string
	ModelProvider string
	ModelName     string
	Framework     string
	Language      string
	KagentVersion string
}

// BaseGenerator provides common functionality for all project generators
type BaseGenerator struct {
	TemplateFiles fs.FS
}

// NewBaseGenerator creates a new base generator
func NewBaseGenerator(templateFiles fs.FS) *BaseGenerator {
	return &BaseGenerator{
		TemplateFiles: templateFiles,
	}
}

// GenerateProject generates a new project using the provided templates
func (g *BaseGenerator) GenerateProject(config AgentConfig) error {
	// Get templates subdirectory
	templateRoot, err := fs.Sub(g.TemplateFiles, "templates")
	if err != nil {
		return fmt.Errorf("failed to get templates subdirectory: %w", err)
	}

	// Walk through all template files
	err = fs.WalkDir(templateRoot, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories, we'll create them as needed
		if d.IsDir() {
			return nil
		}

		// Determine destination path by removing .tmpl extension
		destPath := filepath.Join(config.Directory, strings.TrimSuffix(path, ".tmpl"))

		// Create the directory if it doesn't exist
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", filepath.Dir(destPath), err)
		}

		// Read template file
		templateContent, err := fs.ReadFile(templateRoot, path)
		if err != nil {
			return fmt.Errorf("failed to read template file %s: %w", path, err)
		}

		// Render template content
		renderedContent, err := g.renderTemplate(string(templateContent), config)
		if err != nil {
			return fmt.Errorf("failed to render template for %s: %w", path, err)
		}

		// Create file
		if err := os.WriteFile(destPath, []byte(renderedContent), 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", destPath, err)
		}

		if config.Verbose {
			// print the generated files
			fmt.Printf("  Generated: %s\n", destPath)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk templates: %w", err)
	}

	return nil
}

// renderTemplate renders a template string with the provided data
func (g *BaseGenerator) renderTemplate(tmplContent string, data interface{}) (string, error) {
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
