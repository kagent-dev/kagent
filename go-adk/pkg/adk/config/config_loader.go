package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kagent-dev/kagent/go-adk/pkg/core/types"
	"trpc.group/trpc-go/trpc-a2a-go/server"
)

// LoadAgentConfig loads agent configuration from config.json file
func LoadAgentConfig(configPath string) (*types.AgentConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	var config types.AgentConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// LoadAgentCard loads agent card from agent-card.json file
func LoadAgentCard(cardPath string) (*server.AgentCard, error) {
	data, err := os.ReadFile(cardPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent card file %s: %w", cardPath, err)
	}

	var card server.AgentCard
	if err := json.Unmarshal(data, &card); err != nil {
		return nil, fmt.Errorf("failed to parse agent card file: %w", err)
	}

	return &card, nil
}

// LoadAgentConfigs loads both config and agent card from the config directory
// This matches the Python implementation which reads from /config directory
func LoadAgentConfigs(configDir string) (*types.AgentConfig, *server.AgentCard, error) {
	configPath := filepath.Join(configDir, "config.json")
	cardPath := filepath.Join(configDir, "agent-card.json")

	config, err := LoadAgentConfig(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load agent config: %w", err)
	}

	// Validate that all fields are properly loaded
	// Note: No logger available at this point, validation will proceed without logging
	if err := ValidateAgentConfigUsage(config); err != nil {
		return nil, nil, fmt.Errorf("invalid agent config: %w", err)
	}

	card, err := LoadAgentCard(cardPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load agent card: %w", err)
	}

	return config, card, nil
}
