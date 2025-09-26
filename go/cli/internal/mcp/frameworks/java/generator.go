package java

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	texttemplate "text/template"

	"github.com/kagent-dev/kagent/go/cli/internal/mcp"
	"github.com/kagent-dev/kagent/go/cli/internal/mcp/frameworks/common"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

//go:embed all:templates
var templateFiles embed.FS

// Generator for Java projects
type Generator struct {
	common.BaseGenerator
}

// NewGenerator creates a new Java generator
func NewGenerator() *Generator {
	return &Generator{
		BaseGenerator: common.BaseGenerator{
			TemplateFiles:    templateFiles,
			ToolTemplateName: "src/main/java/com/example/tools/ToolTemplate.java.tmpl",
		},
	}
}

// GenerateProject generates a new Java project
func (g *Generator) GenerateProject(config mcp.ProjectConfig) error {
	if config.Verbose {
		fmt.Println("Generating Java MCP project...")
	}

	if err := g.BaseGenerator.GenerateProject(config); err != nil {
		return fmt.Errorf("failed to generate project: %w", err)
	}

	// Regenerate Tools.java to include initial tools
	toolsDir := filepath.Join(config.Directory, "src", "main", "java", "com", "example", "tools")
	if err := g.regenerateToolsClass(toolsDir); err != nil {
		return fmt.Errorf("failed to regenerate Tools.java: %w", err)
	}

	return nil
}

// GenerateTool generates a new tool for a Java project.
func (g *Generator) GenerateTool(projectRoot string, config mcp.ToolConfig) error {
	// Override the tool file generation to use PascalCase for Java
	toolNamePascalCase := cases.Title(language.English).String(config.ToolName)
	toolFileName := toolNamePascalCase + ".java"

	// Generate the tool file manually
	toolsDir := filepath.Join(projectRoot, "src", "main", "java", "com", "example", "tools")
	toolFilePath := filepath.Join(toolsDir, toolFileName)

	if err := g.GenerateToolFile(toolFilePath, config); err != nil {
		return fmt.Errorf("failed to generate tool file: %w", err)
	}

	// After generating the tool file, regenerate the Tools.java file
	if err := g.regenerateToolsClass(toolsDir); err != nil {
		return fmt.Errorf("failed to regenerate Tools.java: %w", err)
	}

	fmt.Printf("✅ Successfully created tool: %s\n", config.ToolName)
	fmt.Printf("📁 Generated file: src/main/java/com/example/tools/%s.java\n", toolNamePascalCase)
	fmt.Printf("🔄 Updated tools/Tools.java with new tool registration\n")

	fmt.Printf("\nNext steps:\n")
	fmt.Printf("1. Edit src/main/java/com/example/tools/%s.java to implement your tool logic\n", toolNamePascalCase)
	fmt.Printf("2. Configure any required environment variables in kmcp.yaml\n")
	fmt.Printf("3. Run 'mvn clean install' to build the project\n")
	fmt.Printf("4. Run 'mvn exec:java -Dexec.mainClass=\"com.example.Main\"' to start the server\n")

	return nil
}

// regenerateToolsClass regenerates the Tools.java file in the tools directory
func (g *Generator) regenerateToolsClass(toolsDir string) error {
	// Scan the tools directory for Java files
	tools, err := g.scanToolsDirectory(toolsDir)
	if err != nil {
		return fmt.Errorf("failed to scan tools directory: %w", err)
	}

	// Read the template
	templateContent, err := fs.ReadFile(g.TemplateFiles, "templates/src/main/java/com/example/tools/Tools.java.tmpl")
	if err != nil {
		return fmt.Errorf("failed to read Tools.java template: %w", err)
	}

	// Create template data
	templateData := map[string]interface{}{
		"Tools": tools,
	}

	// Render the template
	renderedContent, err := renderTemplate(string(templateContent), templateData)
	if err != nil {
		return fmt.Errorf("failed to render Tools.java template: %w", err)
	}

	// Write the Tools.java file
	toolsPath := filepath.Join(toolsDir, "Tools.java")
	return os.WriteFile(toolsPath, []byte(renderedContent), 0644)
}

// scanToolsDirectory scans the tools directory and returns a list of tool names
func (g *Generator) scanToolsDirectory(toolsDir string) ([]string, error) {
	var tools []string

	// Read the directory
	entries, err := os.ReadDir(toolsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read tools directory: %w", err)
	}

	// Find all Java files (excluding Tools.java, Tool.java, and ToolConfig.java)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasSuffix(name, ".java") && name != "Tools.java" && name != "Tool.java" && name != "ToolConfig.java" {
			// Extract tool name (filename without .java extension)
			toolName := strings.TrimSuffix(name, ".java")
			tools = append(tools, toolName)
		}
	}

	return tools, nil
}

// renderTemplate renders a template string with the provided data
func renderTemplate(tmplContent string, data interface{}) (string, error) {
	tmpl, err := texttemplate.New("template").Parse(tmplContent)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return result.String(), nil
}
