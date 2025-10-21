package common

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const ManifestFileName = "kagent.yaml"

// AgentManifest represents the agent project configuration and metadata
type AgentManifest struct {
	Name          string          `yaml:"agentName"`
	Language      string          `yaml:"language"`
	Framework     string          `yaml:"framework"`
	ModelProvider string          `yaml:"modelProvider"`
	ModelName     string          `yaml:"modelName"`
	Description   string          `yaml:"description"`
	McpServers    []McpServerType `yaml:"mcpServers,omitempty" json:"mcpServers,omitempty"`
	UpdatedAt     time.Time       `yaml:"updatedAt,omitempty"`
}

// McpServerType represents a single MCP server configuration
type McpServerType struct {
	Type    string            `yaml:"type" json:"type"`
	Name    string            `yaml:"name" json:"name"`
	Image   string            `yaml:"image,omitempty" json:"image,omitempty"`
	Build   string            `yaml:"build,omitempty" json:"build,omitempty"`
	Command string            `yaml:"command,omitempty" json:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env     []string          `yaml:"env,omitempty" json:"env,omitempty"`
	URL     string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
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
	// McpServers are optional; validate only if provided
	for i, srv := range manifest.McpServers {
		if srv.Type == "" {
			return fmt.Errorf("mcpServers[%d]: type is required", i)
		}
		if srv.Name == "" {
			return fmt.Errorf("mcpServers[%d]: name is required", i)
		}
		// Enforce that only one of image or build is set
		if srv.Image != "" && srv.Build != "" {
			return fmt.Errorf("mcpServers[%d]: only one of image or build may be set", i)
		}
		switch srv.Type {
		case "remote":
			if srv.URL == "" {
				return fmt.Errorf("mcpServers[%d]: url is required for type 'remote'", i)
			}
			parsed, err := url.Parse(srv.URL)
			if err != nil {
				return fmt.Errorf("mcpServers[%d]: url is not a valid URL: %v", i, err)
			}
			if parsed.Scheme == "" {
				return fmt.Errorf("mcpServers[%d]: url is missing scheme (http or https required)", i)
			}
			if parsed.Scheme != "http" && parsed.Scheme != "https" {
				return fmt.Errorf("mcpServers[%d]: url scheme must be http or https (got '%s')", i, parsed.Scheme)
			}
			if parsed.Host == "" {
				return fmt.Errorf("mcpServers[%d]: url is missing host", i)
			}
		case "command":
			// Command is required only if neither image nor build is provided
			if srv.Command == "" && srv.Image == "" && srv.Build == "" {
				return fmt.Errorf("mcpServers[%d]: at least one of command, image, or build is required for type 'command'", i)
			}
		default:
			return fmt.Errorf("mcpServers[%d]: unsupported type '%s' (expected 'command' or 'remote')", i, srv.Type)
		}
	}
	return nil
}

// NewProjectManifest creates a new AgentManifest with the given values
func NewProjectManifest(agentName, language, framework, modelProvider, modelName, description string, mcpServers []McpServerType) *AgentManifest {
	return &AgentManifest{
		Name:          agentName,
		Language:      language,
		Framework:     framework,
		ModelProvider: modelProvider,
		ModelName:     modelName,
		Description:   description,
		UpdatedAt:     time.Now(),
		McpServers:    mcpServers,
	}
}
