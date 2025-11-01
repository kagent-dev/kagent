package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/cli/internal/agent"
	"github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/adk/python"
	"github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/common"
	crewai "github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/crewai/python"
	langgraph "github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/langgraph/python"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/internal/version"
)

type InitCfg struct {
	Framework       string
	Language        string
	AgentName       string
	InstructionFile string
	ModelProvider   string
	ModelName       string
	Description     string
	Config          *config.Config
}

func InitCmd(cfg *InitCfg) error {
	// Validate agent name
	if cfg.AgentName == "" {
		return fmt.Errorf("agent name is required")
	}

	// Normalize framework name
	framework := strings.ToLower(cfg.Framework)
	if framework == "" {
		framework = "adk" // Default to ADK
	}

	// Validate framework
	validator := common.NewFrameworkValidator()
	if err := validator.Validate(framework); err != nil {
		return fmt.Errorf("invalid framework: %w", err)
	}

	language := strings.ToLower(cfg.Language)
	if language == "" {
		language = "python" // Default to Python
	}
	if language != "python" {
		return fmt.Errorf("unsupported language: %s. Only 'python' is supported for now", language)
	}

	// Validate model provider if specified
	if cfg.ModelProvider != "" {
		if err := validateModelProvider(cfg.ModelProvider); err != nil {
			return err
		}
	}

	// use lower case for model provider since the templates expect the model provider in lower case
	cfg.ModelProvider = strings.ToLower(cfg.ModelProvider)

	// Set default model provider if not specified
	if cfg.ModelProvider == "" {
		cfg.ModelProvider = strings.ToLower(agent.DefaultModelProvider)
	}

	// Set default model name if not specified
	if cfg.ModelName == "" {
		cfg.ModelName = agent.DefaultModelName
	}

	// Get current working directory for project creation
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %v", err)
	}

	// Create project directory
	projectDir := filepath.Join(cwd, cfg.AgentName)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		return fmt.Errorf("failed to create project directory: %v", err)
	}

	// Create generator registry and register all generators
	registry := common.NewGeneratorRegistry()

	// Register ADK generator
	if err := registry.Register(python.NewADKGenerator()); err != nil {
		return fmt.Errorf("failed to register ADK generator: %w", err)
	}

	// Register CrewAI generator
	if err := registry.Register(crewai.NewCrewAIGenerator()); err != nil {
		return fmt.Errorf("failed to register CrewAI generator: %w", err)
	}

	// Register LangGraph generator
	if err := registry.Register(langgraph.NewLangGraphGenerator()); err != nil {
		return fmt.Errorf("failed to register LangGraph generator: %w", err)
	}

	// Get the appropriate generator
	generator, err := registry.Get(framework)
	if err != nil {
		return fmt.Errorf("failed to get generator for framework '%s': %w", framework, err)
	}

	// Load instruction from file if specified
	var instruction string
	if cfg.InstructionFile != "" {
		content, err := os.ReadFile(cfg.InstructionFile)
		if err != nil {
			return fmt.Errorf("failed to read instruction file '%s': %v", cfg.InstructionFile, err)
		}
		instruction = string(content)
	}

	// Get the kagent version
	kagentVersion := version.Version

	if cfg.Config.Verbose {
		fmt.Printf("🚀 Initializing %s agent with %s framework (language: %s)\n", cfg.AgentName, framework, language)
		fmt.Printf(" Output directory: %s\n", projectDir)
		fmt.Printf(" Model: %s/%s\n", cfg.ModelProvider, cfg.ModelName)
	}

	// Generate the project
	if err := generator.Generate(projectDir, cfg.AgentName, instruction, cfg.ModelProvider, cfg.ModelName, cfg.Description, cfg.Config.Verbose, kagentVersion); err != nil {
		return fmt.Errorf("failed to generate project: %v", err)
	}

	return nil
}

// validateModelProvider checks if the provided model provider is supported
func validateModelProvider(provider string) error {
	switch v1alpha2.ModelProvider(provider) {
	case v1alpha2.ModelProviderOpenAI,
		v1alpha2.ModelProviderAnthropic,
		v1alpha2.ModelProviderGemini,
		v1alpha2.ModelProviderAzureOpenAI:
		return nil
	default:
		return fmt.Errorf("unsupported model provider: %s. Supported providers: OpenAI, Anthropic, Gemini, azureopenai", provider)
	}
}
