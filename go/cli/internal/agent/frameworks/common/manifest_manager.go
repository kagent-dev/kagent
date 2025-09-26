package common

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const ManifestFileName = "kagent.yaml"

// AgentManifest represents the agent project configuration and metadata
type AgentManifest struct {
	Name          string    `yaml:"agentName"`
	Language      string    `yaml:"language"`
	Framework     string    `yaml:"framework"`
	ModelProvider string    `yaml:"modelProvider"`
	ModelName     string    `yaml:"modelName"`
	Description   string    `yaml:"description"`
	UpdatedAt     time.Time `yaml:"updatedAt,omitempty"`
}

// Manager handles loading and saving of agent manifests
type Manager struct {
	projectRoot string
}

// NewManifestManager creates a new manifest manager for the given project root
func NewManifestManager(projectRoot string) *Manager {
	return &Manager{
		projectRoot: projectRoot,
	}
}

// Load reads and parses the kagent.yaml file
func (m *Manager) Load() (*AgentManifest, error) {
	manifestPath := filepath.Join(m.projectRoot, ManifestFileName)

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("kagent.yaml not found in %s", m.projectRoot)
		}
		return nil, fmt.Errorf("failed to read kagent.yaml: %w", err)
	}

	var manifest AgentManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse kagent.yaml: %w", err)
	}

	// Validate the manifest
	if err := m.Validate(&manifest); err != nil {
		return nil, fmt.Errorf("invalid kagent.yaml: %w", err)
	}

	return &manifest, nil
}

// Save writes the manifest to kagent.yaml
func (m *Manager) Save(manifest *AgentManifest) error {
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
		return fmt.Errorf("failed to write kagent.yaml: %w", err)
	}

	return nil
}

// Validate checks if the manifest is valid
func (m *Manager) Validate(manifest *AgentManifest) error {
	if manifest.Name == "" {
		return fmt.Errorf("agent name is required")
	}
	if manifest.Language == "" {
		return fmt.Errorf("language is required")
	}
	if manifest.Framework == "" {
		return fmt.Errorf("framework is required")
	}
	return nil
}

// NewProjectManifest creates a new AgentManifest with the given values
func NewProjectManifest(agentName, language, framework, modelProvider, modelName, description string) *AgentManifest {
	return &AgentManifest{
		Name:          agentName,
		Language:      language,
		Framework:     framework,
		ModelProvider: modelProvider,
		ModelName:     modelName,
		Description:   description,
		UpdatedAt:     time.Now(),
	}
}
