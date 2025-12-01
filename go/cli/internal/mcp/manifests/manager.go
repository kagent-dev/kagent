package manifests

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const ManifestFileName = "manifest.yaml"

// Manager handles project manifest operations
type Manager struct {
	projectRoot string
}

// NewManager creates a new manifest manager
func NewManager(projectRoot string) *Manager {
	return &Manager{
		projectRoot: projectRoot,
	}
}

// Load reads and parses the manifest.yaml file
func (m *Manager) Load() (*ProjectManifest, error) {
	manifestPath := filepath.Join(m.projectRoot, ManifestFileName)

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("manifest.yaml not found in %s", m.projectRoot)
		}
		return nil, fmt.Errorf("failed to read manifest.yaml: %w", err)
	}

	var manifest ProjectManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest.yaml: %w", err)
	}

	// Validate the manifest
	if err := m.Validate(&manifest); err != nil {
		return nil, fmt.Errorf("invalid manifest.yaml: %w", err)
	}

	return &manifest, nil
}

// Save writes the manifest to manifest.yaml
func (m *Manager) Save(manifest *ProjectManifest) error {
	// Update timestamp
	manifest.UpdatedAt = time.Now()

	// Validate before saving
	if err := m.Validate(manifest); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}

	data, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	manifestPath := filepath.Join(m.projectRoot, ManifestFileName)
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write manifest.yaml: %w", err)
	}

	return nil
}

// Exists checks if a manifest.yaml file exists in the project root
func (m *Manager) Exists() bool {
	manifestPath := filepath.Join(m.projectRoot, ManifestFileName)
	_, err := os.Stat(manifestPath)
	return err == nil
}

// GetDefault returns a new ProjectManifest with default values
func GetDefault(name, framework, description, author, email, namespace string) *ProjectManifest {
	if description == "" {
		description = fmt.Sprintf("MCP server built with %s", framework)
	}
	return &ProjectManifest{
		Name:        name,
		Framework:   framework,
		Version:     "0.1.0",
		Description: description,
		Author:      author,
		Email:       email,
		Tools:       make(map[string]ToolConfig),
		Secrets: SecretsConfig{
			"local": {
				Enabled:  false,
				Provider: SecretProviderEnv,
				File:     ".env.local",
			},
			"staging": {
				Enabled:    false,
				Provider:   SecretProviderKubernetes,
				Namespace:  namespace,
				SecretName: fmt.Sprintf("%s-secrets-staging", strings.ReplaceAll(name, "_", "-")),
			},
			"production": {
				Enabled:    false,
				Provider:   SecretProviderKubernetes,
				Namespace:  namespace,
				SecretName: fmt.Sprintf("%s-secrets-production", strings.ReplaceAll(name, "_", "-")),
			},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// Validate checks if the manifest is valid
func (m *Manager) Validate(manifest *ProjectManifest) error {
	// Basic validation
	if manifest.Name == "" {
		return fmt.Errorf("project name is required")
	}

	if manifest.Framework == "" {
		return fmt.Errorf("framework is required")
	}

	if !isValidFramework(manifest.Framework) {
		return fmt.Errorf("unsupported framework: %s", manifest.Framework)
	}

	// Validate tools
	for toolName, tool := range manifest.Tools {
		if err := m.validateTool(toolName, tool); err != nil {
			return fmt.Errorf("invalid tool %s: %w", toolName, err)
		}
	}

	// Validate secrets
	if err := m.validateSecrets(manifest.Secrets); err != nil {
		return fmt.Errorf("invalid secrets config: %w", err)
	}

	return nil
}

// AddTool adds a new tool to the manifest
func (m *Manager) AddTool(manifest *ProjectManifest, name string, config ToolConfig) error {
	if name == "" {
		return fmt.Errorf("tool name is required")
	}

	if err := m.validateTool(name, config); err != nil {
		return err
	}

	if manifest.Tools == nil {
		manifest.Tools = make(map[string]ToolConfig)
	}

	manifest.Tools[name] = config
	return nil
}

// RemoveTool removes a tool from the manifest
func (m *Manager) RemoveTool(manifest *ProjectManifest, name string) error {
	if manifest.Tools == nil {
		return fmt.Errorf("tool %s not found", name)
	}

	if _, exists := manifest.Tools[name]; !exists {
		return fmt.Errorf("tool %s not found", name)
	}

	delete(manifest.Tools, name)
	return nil
}

// Private validation methods

func (m *Manager) validateTool(_ string, tool ToolConfig) error {
	if tool.Name == "" {
		return fmt.Errorf("tool name is required")
	}
	return nil
}

func (m *Manager) validateSecrets(secrets SecretsConfig) error {
	// Validate each secret provider configuration
	for env, config := range secrets {
		if config.Provider != "" && !isValidSecretProvider(config.Provider) {
			return fmt.Errorf("invalid secret provider for environment %s: %s", env, config.Provider)
		}
	}

	return nil
}

// Helper functions

func isValidFramework(framework string) bool {
	validFrameworks := []string{
		FrameworkFastMCPPython,
		FrameworkMCPGo,
		FrameworkTypeScript,
		FrameworkJava,
	}

	for _, valid := range validFrameworks {
		if framework == valid {
			return true
		}
	}
	return false
}

func isValidSecretProvider(provider string) bool {
	validProviders := []string{
		SecretProviderEnv,
		SecretProviderKubernetes,
	}

	for _, valid := range validProviders {
		if provider == valid {
			return true
		}
	}
	return false
}
