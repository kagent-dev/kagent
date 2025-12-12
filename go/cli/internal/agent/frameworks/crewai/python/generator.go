package python

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/common"
)

//go:embed templates/* templates/agent/*
var templatesFS embed.FS

// CrewAIGenerator generates Python CrewAI projects
type CrewAIGenerator struct {
	*common.BaseGenerator
	versions *common.FrameworkVersions
}

// NewCrewAIGenerator creates a new CrewAI Python generator
func NewCrewAIGenerator() *CrewAIGenerator {
	return &CrewAIGenerator{
		BaseGenerator: common.NewBaseGenerator(templatesFS),
		versions:      common.DefaultVersions(),
	}
}

// GetFrameworkName returns the framework name
func (g *CrewAIGenerator) GetFrameworkName() string {
	return "crewai"
}

// GetLanguage returns the language
func (g *CrewAIGenerator) GetLanguage() string {
	return "python"
}

// Generate creates a new Python CrewAI project
func (g *CrewAIGenerator) Generate(projectDir, agentName, instruction, modelProvider, modelName, description string, verbose bool, kagentVersion string) error {
	// Create the main project directory structure
	// Convert agent name to valid Python module name (replace hyphens with underscores)
	moduleName := strings.ReplaceAll(strings.ReplaceAll(agentName, "-", "_"), " ", "_")
	subDir := filepath.Join(projectDir, moduleName)
	if err := os.MkdirAll(subDir, 0755); err != nil {
		return fmt.Errorf("failed to create subdirectory: %v", err)
	}

	// Use default instruction if none provided
	if instruction == "" {
		instruction = "You are a helpful AI assistant built with CrewAI framework."
		if verbose {
			fmt.Println("ℹ️  No instruction provided, using default CrewAI instructions")
		}
	}

	// Agent project configuration
	agentConfig := common.AgentConfig{
		Name:          agentName,
		Directory:     projectDir,
		Framework:     g.GetFrameworkName(),
		Language:      "python",
		Verbose:       verbose,
		Instruction:   instruction,
		ModelProvider: modelProvider,
		ModelName:     modelName,
		KagentVersion: kagentVersion,
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
		nil,
	)

	// Save the manifest using the Manager
	manager := common.NewManifestManager(projectDir)
	if err := manager.Save(projectManifest); err != nil {
		return fmt.Errorf("failed to write project manifest: %v", err)
	}

	// Move agent files from agent/ subdirectory to {agentName} subdirectory
	agentDir := filepath.Join(projectDir, "agent")
	if _, err := os.Stat(agentDir); err == nil {
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

	g.printSuccessMessage(agentConfig)
	return nil
}

func (g *CrewAIGenerator) printSuccessMessage(config common.AgentConfig) {
	fmt.Printf("   Successfully created %s project in %s\n", config.Framework, config.Directory)
	fmt.Printf("   Model configuration for project: %s (%s)\n", config.ModelProvider, config.ModelName)
	fmt.Printf("   Project structure:\n")
	fmt.Printf("   %s/\n", config.Name)
	fmt.Printf("   ├── %s/\n", config.Name)
	fmt.Printf("   │   ├── __init__.py\n")
	fmt.Printf("   │   ├── crew.py\n")
	fmt.Printf("   │   └── agent-card.json\n")
	fmt.Printf("   ├── %s\n", common.ManifestFileName)
	fmt.Printf("   ├── pyproject.toml\n")
	fmt.Printf("   ├── Dockerfile\n")
	fmt.Printf("   └── README.md\n")
	fmt.Printf("\n  Next steps:\n")
	fmt.Printf("   1. cd %s\n", config.Name)
	fmt.Printf("   2. Customize the crew in %s/crew.py\n", config.Name)
	fmt.Printf("   3. Build the agent image:\n")
	fmt.Printf("      kagent build %s --push\n", config.Name)
	fmt.Printf("   4. Deploy the agent:\n")
	fmt.Printf("      kagent deploy %s --api-key <your-api-key>\n", config.Name)
}
