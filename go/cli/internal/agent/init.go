package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/adk/python"
	"github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/common"
	crewai "github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/crewai/python"
	langgraph "github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/langgraph/python"
)

// Default model configuration
const (
	DefaultModelProvider = "Gemini"
	DefaultModelName     = "gemini-2.0-flash"
)

// InitConfig holds configuration for agent initialization
type InitConfig struct {
	AgentName     string
	Framework     string
	Language      string
	Instruction   string
	ModelProvider string
	ModelName     string
	Description   string
	OutputDir     string
	Verbose       bool
	KagentVersion string
}

// InitAgent initializes a new agent project
func InitAgent(config InitConfig) error {
	// Validate agent name
	if config.AgentName == "" {
		return fmt.Errorf("agent name is required")
	}

	// Create generator registry
	registry := common.NewGeneratorRegistry()

	// Register all available generators
	if err := registry.Register(python.NewADKGenerator()); err != nil {
		return fmt.Errorf("failed to register ADK generator: %w", err)
	}
	if err := registry.Register(crewai.NewCrewAIGenerator()); err != nil {
		return fmt.Errorf("failed to register CrewAI generator: %w", err)
	}
	if err := registry.Register(langgraph.NewLangGraphGenerator()); err != nil {
		return fmt.Errorf("failed to register LangGraph generator: %w", err)
	}

	// Validate and normalize framework
	framework := strings.ToLower(config.Framework)
	if framework == "" {
		framework = "adk" // Default to ADK
	}

	// Normalize and validate language (keep this per @petej's request)
	language := strings.ToLower(config.Language)
	if language == "" {
		language = "python" // Default to Python
	}
	if language != "python" {
		return fmt.Errorf("unsupported language: %s. Only 'python' is supported for now", language)
	}

	// Get the appropriate generator
	generator, err := registry.Get(framework)
	if err != nil {
		return fmt.Errorf("failed to get generator: %w", err)
	}

	// Determine output directory
	outputDir := config.OutputDir
	if outputDir == "" {
		outputDir = config.AgentName
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Get absolute path
	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Set default model provider and name if not specified
	if config.ModelProvider == "" {
		config.ModelProvider = DefaultModelProvider
	}
	if config.ModelName == "" {
		config.ModelName = DefaultModelName
	}

	if config.Verbose {
		fmt.Printf("Initializing %s agent with %s framework (language: %s)\n", config.AgentName, framework, language)
		fmt.Printf("Output directory: %s\n", absOutputDir)
		fmt.Printf("Model: %s/%s\n", config.ModelProvider, config.ModelName)
	}

	// Generate the project
	err = generator.Generate(
		absOutputDir,
		config.AgentName,
		config.Instruction,
		config.ModelProvider,
		config.ModelName,
		config.Description,
		config.Verbose,
		config.KagentVersion,
	)

	if err != nil {
		return fmt.Errorf("failed to generate project: %w", err)
	}

	return nil
}

// ListFrameworks returns all supported frameworks
func ListFrameworks() []string {
	return common.SupportedFrameworks
}
