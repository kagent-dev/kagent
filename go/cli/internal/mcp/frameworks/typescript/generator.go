package typescript

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kagent-dev/kagent/go/cli/internal/mcp"
	"github.com/kagent-dev/kagent/go/cli/internal/mcp/frameworks/common"
	"github.com/stoewer/go-strcase"
)

//go:embed all:templates
var templateFiles embed.FS

// Generator for TypeScript projects
type Generator struct {
	common.BaseGenerator
}

// NewGenerator creates a new TypeScript generator
func NewGenerator() *Generator {
	return &Generator{
		BaseGenerator: common.BaseGenerator{
			TemplateFiles:    templateFiles,
			ToolTemplateName: "src/tools/tool.ts.tmpl",
		},
	}
}

// GenerateProject generates a new TypeScript project
func (g *Generator) GenerateProject(config mcp.ProjectConfig) error {
	if config.Verbose {
		fmt.Println("Generating TypeScript MCP project...")
	}

	if err := g.BaseGenerator.GenerateProject(config); err != nil {
		return fmt.Errorf("failed to generate project: %w", err)
	}

	// After generating the project, regenerate the tools.ts file to include the initial echo tool
	if err := g.regenerateToolsFile(config.Directory); err != nil {
		return fmt.Errorf("failed to regenerate tools.ts: %w", err)
	}

	return nil
}

// GenerateTool generates a new tool for a TypeScript project.
func (g *Generator) GenerateTool(projectroot string, config mcp.ToolConfig) error {
	// Override the base generator's tool generation to use camelCase file names
	if err := g.generateTypeScriptTool(projectroot, config); err != nil {
		return fmt.Errorf("failed to generate tool: %w", err)
	}

	// After generating the tool file, regenerate the tools.ts file
	if err := g.regenerateToolsFile(projectroot); err != nil {
		return fmt.Errorf("failed to regenerate tools.ts: %w", err)
	}

	toolNameCamelCase := strcase.LowerCamelCase(config.ToolName)

	fmt.Printf("✅ Successfully created tool: %s\n", config.ToolName)
	fmt.Printf("📁 Generated file: src/tools/%s.ts\n", toolNameCamelCase)
	fmt.Printf("🔄 Updated src/tools.ts with new tool import\n")

	fmt.Printf("\nNext steps:\n")
	fmt.Printf("1. Edit src/tools/%s.ts to implement your tool logic\n", toolNameCamelCase)
	fmt.Printf("2. Configure any required environment variables in manifest.yaml\n")
	fmt.Printf("3. Run 'npm run dev' to start the server in development mode\n")
	fmt.Printf("4. Run 'npm run build' to build the project\n")
	fmt.Printf("5. Run 'npm test' to test your tool\n")

	return nil
}

// generateTypeScriptTool generates a TypeScript tool with camelCase file naming
func (g *Generator) generateTypeScriptTool(projectRoot string, config mcp.ToolConfig) error {
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

		// Use camelCase for TypeScript file names
		toolNameCamelCase := strcase.LowerCamelCase(config.ToolName)

		destPath := filepath.Join(
			projectRoot,
			filepath.Dir(path),
			toolNameCamelCase+filepath.Ext(strings.TrimSuffix(path, ".tmpl")),
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

// regenerateToolsFile regenerates the tools.ts file in the src directory
func (g *Generator) regenerateToolsFile(projectRoot string) error {
	// Scan the tools directory for TypeScript files
	tools, err := g.scanToolsDirectory(filepath.Join(projectRoot, "src", "tools"))
	if err != nil {
		return fmt.Errorf("failed to scan tools directory: %w", err)
	}

	// Generate the tools.ts content
	content := g.generateToolsContent(tools)

	// Write the tools.ts file
	toolsPath := filepath.Join(projectRoot, "src", "tools.ts")
	return os.WriteFile(toolsPath, []byte(content), 0644)
}

// generateToolsContent generates the content for the tools.ts file
func (g *Generator) generateToolsContent(tools []string) string {
	var content strings.Builder

	// Add import statements (named exports)
	for _, tool := range tools {
		content.WriteString(fmt.Sprintf("import { %s } from './tools/%s.js';\n", tool, tool))
	}

	// Add empty line
	content.WriteString("\n")

	// Add export statement (flat array of tool objects)
	content.WriteString("export const allTools = [\n")
	for i, tool := range tools {
		if i > 0 {
			content.WriteString(",\n")
		}
		content.WriteString(fmt.Sprintf("  %s", tool))
	}
	content.WriteString("\n];\n\n")

	// Add getTools function
	content.WriteString("export function getTools() {\n")
	content.WriteString("  return allTools;\n")
	content.WriteString("}\n")

	return content.String()
}

// scanToolsDirectory scans the tools directory and returns a list of tool names
func (g *Generator) scanToolsDirectory(toolsDir string) ([]string, error) {
	var tools []string

	// Read the directory
	entries, err := os.ReadDir(toolsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read tools directory: %w", err)
	}

	// Find all TypeScript files (excluding index.ts)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasSuffix(name, ".ts") && name != "index.ts" {
			// Extract tool name (filename without .ts extension)
			toolName := strings.TrimSuffix(name, ".ts")
			tools = append(tools, toolName)
		}
	}

	return tools, nil
}
