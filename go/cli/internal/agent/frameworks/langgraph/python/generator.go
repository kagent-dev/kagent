package python

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/common"
)

//go:embed templates/* templates/agent/*
var templatesFS embed.FS

// LangGraphGenerator generates Python LangGraph projects
type LangGraphGenerator struct {
	*common.BaseGenerator
	versions *common.FrameworkVersions
}

// NewLangGraphGenerator creates a new LangGraph Python generator
func NewLangGraphGenerator() *LangGraphGenerator {
	return &LangGraphGenerator{
		BaseGenerator: common.NewBaseGenerator(templatesFS),
		versions:      common.DefaultVersions(),
	}
}

// GetFrameworkName returns the framework name
func (g *LangGraphGenerator) GetFrameworkName() string {
	return "langgraph"
}

// GetLanguage returns the language
func (g *LangGraphGenerator) GetLanguage() string {
	return "python"
}

// Generate creates a new Python LangGraph project
func (g *LangGraphGenerator) Generate(projectDir, agentName, instruction, modelProvider, modelName, description string, verbose bool, kagentVersion string) error {
	// Create the main project directory structure
	subDir := filepath.Join(projectDir, agentName)
	if err := os.MkdirAll(subDir, 0755); err != nil {
		return fmt.Errorf("failed to create subdirectory: %v", err)
	}

	// Use default instruction if none provided
	if instruction == "" {
		instruction = "You are a helpful AI assistant built with LangGraph framework."
		if verbose {
			fmt.Println("ℹ  No instruction provided, using default LangGraph instructions")
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

func (g *LangGraphGenerator) printSuccessMessage(config common.AgentConfig) {
	fmt.Printf("   Successfully created %s project in %s\n", config.Framework, config.Directory)
	fmt.Printf("   Model configuration for project: %s (%s)\n", config.ModelProvider, config.ModelName)
	fmt.Printf("   Project structure:\n")
	fmt.Printf("   %s/\n", config.Name)
	fmt.Printf("   ├── %s/\n", config.Name)
	fmt.Printf("   │   ├── __init__.py\n")
	fmt.Printf("   │   ├── graph.py\n")
	fmt.Printf("   │   └── agent-card.json\n")
	fmt.Printf("   ├── %s\n", common.ManifestFileName)
	fmt.Printf("   ├── pyproject.toml\n")
	fmt.Printf("   ├── Dockerfile\n")
	fmt.Printf("   └── README.md\n")
	fmt.Printf("\n  Next steps:\n")
	fmt.Printf("   1. cd %s\n", config.Name)
	fmt.Printf("   2. Customize the graph in %s/graph.py\n", config.Name)
	fmt.Printf("   3. Build the agent image:\n")
	fmt.Printf("      kagent build %s --push\n", config.Name)
	fmt.Printf("   4. Deploy the agent:\n")
	fmt.Printf("      kagent deploy %s --api-key <your-api-key>\n", config.Name)
}
