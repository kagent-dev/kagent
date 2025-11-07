package python

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kagent-dev/kagent/go/cli/agent/frameworks/common"
)

//go:embed templates/* templates/agent/* templates/mcp_server/* dice-agent-instruction.md
var templatesFS embed.FS

// PythonGenerator generates Python ADK projects
type PythonGenerator struct {
	*common.BaseGenerator
}

// NewPythonGenerator creates a new ADK Python generator
func NewPythonGenerator() *PythonGenerator {
	return &PythonGenerator{
		BaseGenerator: common.NewBaseGenerator(templatesFS),
	}
}

// Generate creates a new Python ADK project
func (g *PythonGenerator) Generate(agentConfig *common.AgentConfig) error {
	// Create the main project directory structure
	subDir := filepath.Join(agentConfig.Directory, agentConfig.Name)
	if err := os.MkdirAll(subDir, 0755); err != nil {
		return fmt.Errorf("failed to create subdirectory: %v", err)
	}
	// Load default instructions if none provided
	if agentConfig.Instruction == "" {
		if agentConfig.Verbose {
			fmt.Println("🎲 No instruction provided, using default dice-roller instructions")
		}
		defaultInstructions, _ := templatesFS.ReadFile("dice-agent-instruction.md")
		agentConfig.Instruction = string(defaultInstructions)
	}

	// agent project configuration
	agentConfig.Framework = "adk"
	agentConfig.Language = "python"

	// Use the base generator to create the project
	if err := g.GenerateProject(*agentConfig); err != nil {
		return fmt.Errorf("failed to generate project: %v", err)
	}

	// Generate project manifest file
	projectManifest := common.NewProjectManifest(
		agentConfig.Name,
		agentConfig.Language,
		agentConfig.Framework,
		agentConfig.ModelProvider,
		agentConfig.ModelName,
		agentConfig.Description,
		agentConfig.McpServers,
	)

	// Save the manifest using the Manager
	manager := common.NewManifestManager(agentConfig.Directory)
	if err := manager.Save(projectManifest); err != nil {
		return fmt.Errorf("failed to write project manifest: %v", err)
	}

	// Move agent files from agent/ subdirectory to {agentName} subdirectory
	agentDir := filepath.Join(agentConfig.Directory, "agent")
	if _, err := os.Stat(agentDir); err == nil {
		// Move all files from agent/ to project subdirectory
		entries, err := os.ReadDir(agentDir)
		if err != nil {
			return fmt.Errorf("failed to read agent directory: %v", err)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				srcPath := filepath.Join(agentDir, entry.Name())
				dstPath := filepath.Join(subDir, entry.Name())

				if err := os.Rename(srcPath, dstPath); err != nil {
					return fmt.Errorf("failed to move %s to %s: %v", srcPath, dstPath, err)
				}
			}
		}

		// Remove the now-empty agent directory
		if err := os.Remove(agentDir); err != nil {
			return fmt.Errorf("failed to remove agent directory: %v", err)
		}
	}

	fmt.Printf("✅ Successfully created %s project in %s\n", agentConfig.Framework, agentConfig.Directory)
	fmt.Printf("🤖 Model configuration for project: %s (%s)\n", agentConfig.ModelProvider, agentConfig.ModelName)
	fmt.Printf("📁 Project structure:\n")
	fmt.Printf("   %s/\n", agentConfig.Name)
	fmt.Printf("   ├── %s/\n", agentConfig.Name)
	fmt.Printf("   │   ├── __init__.py\n")
	fmt.Printf("   │   ├── agent.py\n")
	fmt.Printf("   │   ├── mcp_tools.py\n")
	fmt.Printf("   │   └── agent-card.json\n")
	fmt.Printf("   ├── %s\n", common.ManifestFileName)
	fmt.Printf("   ├── pyproject.toml\n")
	fmt.Printf("   ├── Dockerfile\n")
	fmt.Printf("   ├── docker-compose.yaml\n")
	fmt.Printf("   └── README.md\n")

	return nil
}
