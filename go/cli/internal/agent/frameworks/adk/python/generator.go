package python

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/common"
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
func (g *PythonGenerator) Generate(projectDir, agentName, instruction, modelProvider, modelName, description string, verbose bool, kagentVersion string) error {
	// Create the main project directory structure
	subDir := filepath.Join(projectDir, agentName)
	if err := os.MkdirAll(subDir, 0755); err != nil {
		return fmt.Errorf("failed to create subdirectory: %v", err)
	}
	// Load default instructions if none provided
	if instruction == "" {
		if verbose {
			fmt.Println("🎲 No instruction provided, using default dice-roller instructions")
		}
		defaultInstructions, _ := templatesFS.ReadFile("dice-agent-instruction.md")
		instruction = string(defaultInstructions)
	}

	// agent project configuration
	agentConfig := common.AgentConfig{
		Name:          agentName,
		Directory:     projectDir,
		Framework:     "adk",
		Language:      "python",
		Verbose:       verbose,
		Instruction:   instruction,
		ModelProvider: modelProvider,
		ModelName:     modelName,
		KagentVersion: kagentVersion,
		// Empty MCP servers on init
		McpServers: nil,
	}

	// Use the base generator to create the project
	if err := g.GenerateProject(agentConfig); err != nil {
		return fmt.Errorf("failed to generate project: %v", err)
	}

	// Generate project manifest file
	projectManifest := common.NewProjectManifest(
		agentConfig.Name,
		agentConfig.Language,
		agentConfig.Framework,
		agentConfig.ModelProvider,
		agentConfig.ModelName,
		description,
		agentConfig.McpServers,
	)

	// Save the manifest using the Manager
	manager := common.NewManifestManager(projectDir)
	if err := manager.Save(projectManifest); err != nil {
		return fmt.Errorf("failed to write project manifest: %v", err)
	}

	// Move agent files from agent/ subdirectory to {agentName} subdirectory
	agentDir := filepath.Join(projectDir, "agent")
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

	fmt.Printf("✅ Successfully created %s project in %s\n", agentConfig.Framework, projectDir)
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
	fmt.Printf("   Note: MCP server directories are created when you run 'kagent add-mcp'\n")
	fmt.Printf("\n🚀 Next steps:\n")
	fmt.Printf("   1. cd %s\n", agentConfig.Name)
	fmt.Printf("   2. Customize the agent in %s/agent.py\n", agentConfig.Name)
	fmt.Printf("   3. Build the agent and MCP servers and push it to the local registry\n")
	fmt.Printf("      kagent build %s --push\n", agentConfig.Name)
	fmt.Printf("   4. Run the agent locally\n")
	fmt.Printf("      kagent run\n")
	fmt.Printf("   5. Deploy the agent to your local cluster\n")
	fmt.Printf("      kagent deploy %s --api-key-secret <secret-name>\n", agentConfig.Name)
	fmt.Printf("      Or use --api-key for convenience: kagent deploy %s --api-key <api-key>\n", agentConfig.Name)
	fmt.Printf("      Support for using a credential file is coming soon\n")

	return nil
}
