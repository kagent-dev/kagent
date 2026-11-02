package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/a2aproject/a2a-go/v2/a2a"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

// LoadAgentConfig loads agent configuration from config.json file
func LoadAgentConfig(configPath string) (*configv1.ADKConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	var config configv1.ADKConfig
	if err := Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// LoadAgentCard loads agent card from agent-card.json file
func LoadAgentCard(cardPath string) (*a2a.AgentCard, error) {
	data, err := os.ReadFile(cardPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent card file %s: %w", cardPath, err)
	}

	var card a2a.AgentCard
	if err := json.Unmarshal(data, &card); err != nil {
		return nil, fmt.Errorf("failed to parse agent card file: %w", err)
	}

	return &card, nil
}

// LoadAgentConfigs loads both config and agent card from the config directory
func LoadAgentConfigs(configDir string) (*configv1.ADKConfig, *a2a.AgentCard, error) {
	configPath := filepath.Join(configDir, "config.json")
	cardPath := filepath.Join(configDir, "agent-card.json")

	config, err := LoadAgentConfig(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load agent config: %w", err)
	}

	if err := ValidateAgentConfigUsage(config.Agent); err != nil {
		return nil, nil, fmt.Errorf("invalid agent config: %w", err)
	}

	card, err := LoadAgentCard(cardPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load agent card: %w", err)
	}

	return config, card, nil
}
