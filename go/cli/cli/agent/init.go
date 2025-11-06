package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/cli/agent/frameworks"
	"github.com/kagent-dev/kagent/go/cli/config"
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

func InitCmd(cfg *InitCfg, cmdName, kagentVersion string) error {
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

	if kagentVersion == "" {
		return fmt.Errorf("kagent version is required")
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

	// Generate the project
	if err := generator.Generate(projectDir, cfg.AgentName, instruction, cfg.ModelProvider, cfg.ModelName, cfg.Description, cfg.Config.Verbose, kagentVersion); err != nil {
		return fmt.Errorf("failed to generate project: %v", err)
	}

	fmt.Printf("   Note: MCP server directories are created when you run '%s add-mcp'\n", cmdName)
	fmt.Printf("\n🚀 Next steps:\n")
	fmt.Printf("   1. cd %s\n", cfg.AgentName)
	fmt.Printf("   2. Customize the agent in %s/agent.py\n", cfg.AgentName)
	fmt.Printf("   3. Build the agent and MCP servers and push it to the local registry\n")
	fmt.Printf("      %s build %s --push\n", cmdName, cfg.AgentName)
	fmt.Printf("   4. Run the agent locally\n")
	fmt.Printf("      %s run\n", cmdName)
	fmt.Printf("   5. Deploy the agent to your local cluster\n")
	fmt.Printf("      %s deploy %s --api-key-secret <secret-name>\n", cmdName, cfg.AgentName)
	fmt.Printf("      Or use --api-key for convenience: %s deploy %s --api-key <api-key>\n", cmdName, cfg.AgentName)
	fmt.Printf("      Support for using a credential file is coming soon\n")

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
