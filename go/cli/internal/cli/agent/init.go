package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks"
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
	if err := validateAgentName(cfg.AgentName); err != nil {
		return err
	}

	// Validate framework and language
	if cfg.Framework != "adk" {
		return fmt.Errorf("unsupported framework: %s. Only 'adk' is supported", cfg.Framework)
	}

	if cfg.Language != "python" {
		return fmt.Errorf("unsupported language: %s. Only 'python' is supported for ADK", cfg.Language)
	}

	if cfg.ModelName != "" && cfg.ModelProvider == "" {
		return fmt.Errorf("model provider is required when model name is provided")
	}

	// Validate model provider if specified
	if cfg.ModelProvider != "" {
		if err := validateModelProvider(cfg.ModelProvider); err != nil {
			return err
		}
	}

	// use lower case for model provider since the templates expect the model provider in lower case
	cfg.ModelProvider = strings.ToLower(cfg.ModelProvider)

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

	// Initialize the framework generator
	generator, err := frameworks.NewGenerator(cfg.Framework, cfg.Language)
	if err != nil {
		return fmt.Errorf("failed to create generator: %v", err)
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
		v1alpha2.ModelProviderGemini:
		return nil
	default:
		return fmt.Errorf("unsupported model provider: %s. Supported providers: OpenAI, Anthropic, Gemini", provider)
	}
}

// validateAgentName checks if the agent name is a valid identifier.
// The name must start with a letter or underscore and contain only letters, digits,
// and underscores. This matches the Python identifier rules enforced by the ADK at runtime.
func validateAgentName(name string) error {
	if name == "" {
		return fmt.Errorf("agent name cannot be empty")
	}

	first, _ := utf8.DecodeRuneInString(name)
	if !unicode.IsLetter(first) && first != '_' {
		return fmt.Errorf("invalid agent name %q: must start with a letter or underscore", name)
	}

	for i, c := range name {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '_' {
			return fmt.Errorf("invalid agent name %q: character %q at position %d is not allowed. Agent names must only contain letters, digits, and underscores", name, c, i)
		}
	}

	return nil
}
