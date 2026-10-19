package config

import (
	"fmt"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

// AgentConfigUsage documents how Agent.yaml spec fields map to AgentConfig and are used
// This matches the Python implementation in kagent-adk

// AgentSpec to AgentConfig Mapping:
//
// Agent.Spec.Description -> AgentConfig.Description
//   - Used as agent description in agent card and metadata
//
// Agent.Spec.SystemMessage -> AgentConfig.Instruction
//   - Used as the system message/instruction for the LLM agent
//
// Agent.Spec.ModelConfig -> AgentConfig.Model
//   - Translated to model configuration (OpenAI, Anthropic, etc.)
//   - Includes TLS settings, headers, and model-specific parameters
//
// Agent.Spec.Stream -> AgentConfig.Stream
//   - Controls LLM response streaming (not A2A streaming)
//   - Used in A2aAgentExecutorConfig.stream
//
// Agent.Spec.Tools -> AgentConfig.HttpTools, SseTools, RemoteAgents
//   - Tools with McpServer -> HttpTools or SseTools (based on protocol)
//   - Tools with Agent -> RemoteAgents
//   - Used in create_agent() to add tools to the agent
//
// Agent.Spec.A2AConfig.Skills -> Not in config.json, handled separately
//   - Python adds skill discovery alongside the built-in tools
//   - In go-adk, skills are handled via AgentConfig.SkillsDirectory

// ValidateAgentConfigUsage validates that all AgentConfig fields are properly used
// This is a helper function to ensure we're using all fields correctly
func ValidateAgentConfigUsage(config *configv1.ADKAgentConfig) error {
	if config == nil {
		return fmt.Errorf("agent config is nil")
	}

	// Validate required fields
	if config.Model == nil {
		return fmt.Errorf("agent config model is required")
	}
	// Validate tools
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
func GetAgentConfigSummary(config *configv1.ADKAgentConfig) string {
	if config == nil {
		return "AgentConfig: nil"
	}

	summary := "AgentConfig:\n"
	if config.Model != nil {
		summary += fmt.Sprintf("  Model: %s (%s)\n", ModelType(config.Model), config.Model.GetModel())
	} else {
		summary += "  Model: (nil)\n"
	}
	summary += fmt.Sprintf("  Description: %s\n", config.Description)
	summary += fmt.Sprintf("  Instruction: %d chars\n", len(config.Instruction))
	summary += fmt.Sprintf("  Stream: %v\n", config.Stream)
	summary += fmt.Sprintf("  HasNetworkConfig: %v\n", config.Network != nil)
	summary += fmt.Sprintf("  HttpTools: %d\n", len(config.HttpTools))
	summary += fmt.Sprintf("  SseTools: %d\n", len(config.SseTools))
	summary += fmt.Sprintf("  RemoteAgents: %d\n", len(config.RemoteAgents))

	return summary
}
