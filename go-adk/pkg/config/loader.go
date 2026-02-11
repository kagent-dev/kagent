package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"trpc.group/trpc-go/trpc-a2a-go/server"
)

// LoadAgentConfig loads agent configuration from config.json file
func LoadAgentConfig(configPath string) (*AgentConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	var config AgentConfig
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
func LoadAgentConfigs(configDir string) (*AgentConfig, *server.AgentCard, error) {
	configPath := filepath.Join(configDir, "config.json")
	cardPath := filepath.Join(configDir, "agent-card.json")

	config, err := LoadAgentConfig(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load agent config: %w", err)
	}

	if err := ValidateAgentConfigUsage(config); err != nil {
		return nil, nil, fmt.Errorf("invalid agent config: %w", err)
	}

	card, err := LoadAgentCard(cardPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load agent card: %w", err)
	}

	return config, card, nil
}

// ValidateAgentConfigUsage validates that all AgentConfig fields are properly used.
func ValidateAgentConfigUsage(config *AgentConfig) error {
	if config == nil {
		return fmt.Errorf("agent config is nil")
	}

	if config.Model == nil {
		return fmt.Errorf("agent config model is required")
	}

	for i, tool := range config.HttpTools {
		if tool.Params.Url == "" {
			return fmt.Errorf("http_tools[%d].params.url is required", i)
		}
	}
	for i, tool := range config.SseTools {
		if tool.Params.Url == "" {
			return fmt.Errorf("sse_tools[%d].params.url is required", i)
		}
	}
	for i, agent := range config.RemoteAgents {
		if agent.Url == "" {
			return fmt.Errorf("remote_agents[%d].url is required", i)
		}
		if agent.Name == "" {
			return fmt.Errorf("remote_agents[%d].name is required", i)
		}
	}

	return nil
}

// GetAgentConfigSummary returns a summary of the agent configuration
func GetAgentConfigSummary(config *AgentConfig) string {
	if config == nil {
		return "AgentConfig: nil"
	}

	summary := "AgentConfig:\n"
	if config.Model != nil {
		summary += fmt.Sprintf("  Model: %s (%s)\n", config.Model.GetType(), getModelName(config.Model))
	} else {
		summary += "  Model: (nil)\n"
	}
	summary += fmt.Sprintf("  Description: %s\n", config.Description)
	summary += fmt.Sprintf("  Instruction: %d chars\n", len(config.Instruction))
	summary += fmt.Sprintf("  Stream: %v\n", config.GetStream())
	summary += fmt.Sprintf("  ExecuteCode: %v\n", config.GetExecuteCode())
	summary += fmt.Sprintf("  HttpTools: %d\n", len(config.HttpTools))
	summary += fmt.Sprintf("  SseTools: %d\n", len(config.SseTools))
	summary += fmt.Sprintf("  RemoteAgents: %d\n", len(config.RemoteAgents))

	return summary
}

func getModelName(model Model) string {
	switch m := model.(type) {
	case *OpenAI:
		return m.Model
	case *AzureOpenAI:
		return m.Model
	case *Anthropic:
		return m.Model
	case *GeminiVertexAI:
		return m.Model
	case *GeminiAnthropic:
		return m.Model
	case *Ollama:
		return m.Model
	case *Gemini:
		return m.Model
	default:
		return "unknown"
	}
}
