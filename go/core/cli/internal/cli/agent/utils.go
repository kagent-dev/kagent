package cli

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	clia2a "github.com/kagent-dev/kagent/go/core/cli/internal/a2a"
	pygen "github.com/kagent-dev/kagent/go/core/cli/internal/agent/frameworks/adk/python"
	"github.com/kagent-dev/kagent/go/core/cli/internal/agent/frameworks/common"
	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
)

const (
	DockerComposeFilename = "docker-compose.yaml"
	DockerComposeTemplate = "templates/docker-compose.yaml.tmpl"
)

func StreamA2AEvents(ch <-chan clia2a.StreamResult, verbose bool) error {
	_ = verbose
	defer fmt.Fprintln(os.Stdout)

	for result := range ch {
		if result.Err != nil {
			return result.Err
		}

		json, err := json.Marshal(result.Event)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling A2A event: %v\n", err)
			continue
		}
		fmt.Fprintf(os.Stdout, "%+v\n", string(json))
	}
	return nil
}

// ResolveProjectDir resolves the project directory to an absolute path
func ResolveProjectDir(projectDir string) (string, error) {
	if projectDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current directory: %w", err)
		}
		return cwd, nil
	}

	if filepath.IsAbs(projectDir) {
		return projectDir, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}
	return filepath.Join(cwd, projectDir), nil
}

// ValidateProjectDir checks if a project directory exists
func ValidateProjectDir(projectDir string) error {
	if projectDir == "" {
		return fmt.Errorf("project directory is required")
	}

	if _, err := os.Stat(projectDir); os.IsNotExist(err) {
		return fmt.Errorf("project directory does not exist: %s", projectDir)
	}

	return nil
}

// LoadManifest loads the kagent.yaml file from the project directory
func LoadManifest(projectDir string) (*common.AgentManifest, error) {
	manager := common.NewManifestManager(projectDir)
	manifest, err := manager.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load kagent.yaml: %w", err)
	}
	return manifest, nil
}

// IsVerbose checks if verbose mode is enabled
func IsVerbose(cfg *config.Config) bool {
	return cfg != nil && cfg.Verbose
}

// Template utilities

// ReadTemplateFile reads a template file from the embedded filesystem
func ReadTemplateFile(templatePath string) ([]byte, error) {
	gen := pygen.NewPythonGenerator()
	return fs.ReadFile(gen.TemplateFiles, templatePath)
}

// RenderTemplate reads and renders a template file with the given data
func RenderTemplate(templatePath string, data any) (string, error) {
	gen := pygen.NewPythonGenerator()
	tmplBytes, err := fs.ReadFile(gen.TemplateFiles, templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to read template: %w", err)
	}

	return gen.RenderTemplate(string(tmplBytes), data)
}

// CopyTemplateIfNotExists copies a template file to the target directory if it doesn't exist
func CopyTemplateIfNotExists(targetDir, filename, templatePath string, verbose bool) error {
	targetPath := filepath.Join(targetDir, filename)
	if _, err := os.Stat(targetPath); err == nil {
		// File already exists
		return nil
	}

	templateBytes, err := ReadTemplateFile(templatePath)
	if err != nil {
		return fmt.Errorf("failed to read template %s: %w", templatePath, err)
	}

	if err := os.WriteFile(targetPath, templateBytes, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", filename, err)
	}

	if verbose {
		fmt.Printf("Created %s\n", targetPath)
	}

	return nil
}

// RegenerateDockerCompose regenerates the docker-compose.yaml file with updated MCP server configuration
func RegenerateDockerCompose(projectDir string, manifest *common.AgentManifest, verbose bool) error {
	// Extract environment variables referenced in MCP server headers
	envVars := extractEnvVarsFromHeaders(manifest.McpServers)

	// Template data for docker-compose.yaml
	templateData := struct {
		Name          string
		ModelProvider string
		ModelName     string
		EnvVars       []string
		McpServers    []common.McpServerType
	}{
		Name:          manifest.Name,
		ModelProvider: manifest.ModelProvider,
		ModelName:     manifest.ModelName,
		EnvVars:       envVars,
		McpServers:    manifest.McpServers,
	}

	// Render the docker-compose.yaml template
	renderedContent, err := RenderTemplate(DockerComposeTemplate, templateData)
	if err != nil {
		return fmt.Errorf("failed to render %s template: %w", DockerComposeFilename, err)
	}

	// Write the docker-compose.yaml file
	composePath := filepath.Join(projectDir, DockerComposeFilename)
	if err := os.WriteFile(composePath, []byte(renderedContent), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", DockerComposeFilename, err)
	}

	if verbose {
		fmt.Printf("Updated %s\n", composePath)
	}

	return nil
}

// extractEnvVarsFromHeaders extracts environment variable names from MCP server headers
// It looks for ${VAR_NAME} patterns in header values
func extractEnvVarsFromHeaders(mcpServers []common.McpServerType) []string {
	envVarSet := make(map[string]bool)

	for _, server := range mcpServers {
		if server.Type == "remote" && server.Headers != nil {
			for _, value := range server.Headers {
				// Find all ${VAR_NAME} patterns
				matches := regexp.MustCompile(`\$\{([^}]+)\}`).FindAllStringSubmatch(value, -1)
				for _, match := range matches {
					if len(match) > 1 {
						envVarSet[match[1]] = true
					}
				}
			}
		}
	}

	// Convert set to sorted slice for consistent output
	envVars := make([]string, 0, len(envVarSet))
	for varName := range envVarSet {
		envVars = append(envVars, varName)
	}
	slices.Sort(envVars)

	return envVars
}

// ValidateAPIKey checks if the required API key environment variable is set for the given model provider
func ValidateAPIKey(modelProvider string) error {
	// Get the environment variable name for the provider
	apiKeyEnvVar := GetProviderAPIKey(v1alpha3.ModelProvider(modelProvider))

	// If no API key is required for this provider (e.g., Ollama, local models), skip validation
	if apiKeyEnvVar == "" {
		return nil
	}

	// Check if the environment variable is set and non-empty
	apiKey := os.Getenv(apiKeyEnvVar)
	if apiKey == "" {
		return fmt.Errorf(`required API key not set

The model provider '%s' requires the %s environment variable to be set.

Please set it before running this command:
  export %s="your-api-key-here"`, modelProvider, apiKeyEnvVar, apiKeyEnvVar)
	}

	return nil
}

// regenerateMcpToolsFile regenerates mcp_tools.py with the current MCP servers from the manifest
func regenerateMcpToolsFile(projectDir string, manifest *common.AgentManifest, verbose bool) error {
	// Expected agent directory for ADK Python: <projectDir>/<agentName>
	agentDir := filepath.Join(projectDir, manifest.Name)
	if _, err := os.Stat(agentDir); err != nil {
		// If not present, nothing to do (not an ADK Python layout)
		return nil
	}

	// Prepare template data with MCP servers
	templateData := struct {
		McpServers []common.McpServerType
	}{
		McpServers: manifest.McpServers,
	}

	// Render the mcp_tools.py template
	renderedContent, err := RenderTemplate("templates/agent/mcp_tools.py.tmpl", templateData)
	if err != nil {
		return fmt.Errorf("failed to render mcp_tools.py template: %w", err)
	}

	// Write the mcp_tools.py file
	target := filepath.Join(agentDir, "mcp_tools.py")
	if err := os.WriteFile(target, []byte(renderedContent), 0o644); err != nil {
		return fmt.Errorf("failed to write mcp_tools.py: %w", err)
	}

	if verbose {
		fmt.Printf("Regenerated %s\n", target)
	}
	return nil
}
