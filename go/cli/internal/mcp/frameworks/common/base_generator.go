package common

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/stoewer/go-strcase"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	commongenerator "github.com/kagent-dev/kagent/go/cli/internal/common/generator"
	"github.com/kagent-dev/kagent/go/cli/internal/mcp"
)

// mcpConfigAdapter adapts mcp.ProjectConfig to implement generator.ProjectConfig
type mcpConfigAdapter struct {
	mcp.ProjectConfig
	toolTemplateName string
}

func (a mcpConfigAdapter) GetDirectory() string {
	return a.Directory
}

func (a mcpConfigAdapter) IsVerbose() bool {
	return a.Verbose
}

func (a mcpConfigAdapter) ShouldInitGit() bool {
	return !a.NoGit
}

func (a mcpConfigAdapter) ShouldSkipPath(path string) bool {
	// Skip tool template during project generation
	return path == a.toolTemplateName
}

// BaseGenerator for MCP projects
type BaseGenerator struct {
	*commongenerator.BaseGenerator
	ToolTemplateName string
}

// NewBaseGenerator creates a new MCP base generator
func NewBaseGenerator(templateFiles fs.FS, toolTemplateName string) *BaseGenerator {
	return &BaseGenerator{
		BaseGenerator:    commongenerator.NewBaseGenerator(templateFiles, "templates"),
		ToolTemplateName: toolTemplateName,
	}
}

// GenerateProject generates a new project using the shared generator
func (g *BaseGenerator) GenerateProject(config mcp.ProjectConfig) error {
	adapter := mcpConfigAdapter{
		ProjectConfig:    config,
		toolTemplateName: g.ToolTemplateName,
	}
	return g.BaseGenerator.GenerateProject(adapter)
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

// GenerateToolFile generates a new tool file from the unified template.
// This uses the shared generator's RenderTemplate method.
func (g *BaseGenerator) GenerateToolFile(filePath string, config mcp.ToolConfig) error {
	// Prepare template data
	toolName := config.ToolName
	toolNamePascalCase := cases.Title(language.English).String(toolName)
	toolNameCamelCase := strcase.LowerCamelCase(toolName)
	data := map[string]any{
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

	// Read tool template
	templateContent, err := g.ReadTemplateFile(g.ToolTemplateName)
	if err != nil {
		return fmt.Errorf("failed to read tool template: %w", err)
	}

	// Render template using shared generator
	renderedContent, err := g.RenderTemplate(string(templateContent), data)
	if err != nil {
		return fmt.Errorf("failed to render tool template: %w", err)
	}

	// Write the rendered content
	if err := os.WriteFile(filePath, []byte(renderedContent), 0644); err != nil {
		return fmt.Errorf("failed to write tool file: %w", err)
	}

	return nil
}
