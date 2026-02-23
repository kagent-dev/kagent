package config

import (
	"fmt"

	"github.com/go-logr/logr"
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
//   - Used in AgentConfig.to_agent() to add tools to the agent
//
// Agent.Spec.ExecuteCodeBlocks -> AgentConfig.ExecuteCode
//   - Currently disabled in Go controller (see adk_api_translator.go:533)
//   - Would enable SandboxedLocalCodeExecutor if true
//
// Agent.Spec.A2AConfig.Skills -> Not in config.json, handled separately
//   - Skills are added via SkillsPlugin in Python
//   - In go-adk, skills are handled via KAGENT_SKILLS_FOLDER env var

// ValidateAgentConfigUsage validates that all AgentConfig fields are properly used
// This is a helper function to ensure we're using all fields correctly
func ValidateAgentConfigUsage(config *AgentConfig) error {
	var logger logr.Logger
	return ValidateAgentConfigUsageWithLogger(config, logger)
}

// ValidateAgentConfigUsageWithLogger validates that all AgentConfig fields are properly used
// This is a helper function to ensure we're using all fields correctly
// If logger is the zero value (no sink), validation will proceed without logging
func ValidateAgentConfigUsageWithLogger(config *AgentConfig, logger logr.Logger) error {
	if config == nil {
		return fmt.Errorf("agent config is nil")
	}

	// Validate required fields
	if config.Model == nil {
		return fmt.Errorf("agent config model is required")
	}
	if config.Instruction == "" {
		if logger.GetSink() != nil {
			logger.Info("Warning: agent config instruction is empty")
		}
	}

	// Log field usage (for debugging)
	if logger.GetSink() != nil {
		logger.Info("AgentConfig fields",
			"description", config.Description,
			"instructionLength", len(config.Instruction),
			"modelType", config.Model.GetType(),
			"stream", config.Stream,
			"executeCode", config.ExecuteCode,
			"httpToolsCount", len(config.HttpTools),
			"sseToolsCount", len(config.SseTools),
			"remoteAgentsCount", len(config.RemoteAgents))
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
	summary += fmt.Sprintf("  Stream: %v\n", config.Stream)
	summary += fmt.Sprintf("  ExecuteCode: %v\n", config.ExecuteCode)
	summary += fmt.Sprintf("  HttpTools: %d\n", len(config.HttpTools))
	summary += fmt.Sprintf("  SseTools: %d\n", len(config.SseTools))
	summary += fmt.Sprintf("  RemoteAgents: %d\n", len(config.RemoteAgents))

	return summary
}

func getModelName(m Model) string {
	switch m := m.(type) {
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
