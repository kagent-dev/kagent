package config

import (
	"strings"
	"testing"
)

func TestValidateAgentConfigUsage_NilConfig(t *testing.T) {
	err := ValidateAgentConfigUsage(nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("error should mention nil: %v", err)
	}
}

func TestValidateAgentConfigUsage_MissingModel(t *testing.T) {
	config := &AgentConfig{
		Instruction: "test",
	}
	err := ValidateAgentConfigUsage(config)
	if err == nil {
		t.Fatal("expected error for missing model")
	}
	if !strings.Contains(err.Error(), "model") {
		t.Errorf("error should mention model: %v", err)
	}
}

func TestValidateAgentConfigUsage_ValidMinimal(t *testing.T) {
	config := &AgentConfig{
		Model:       &OpenAI{BaseModel: BaseModel{Type: ModelTypeOpenAI, Model: "gpt-4"}},
		Instruction: "You are helpful.",
	}
	err := ValidateAgentConfigUsage(config)
	if err != nil {
		t.Errorf("expected no error for valid minimal config: %v", err)
	}
}

func TestValidateAgentConfigUsage_HttpToolMissingURL(t *testing.T) {
	config := &AgentConfig{
		Model:       &OpenAI{BaseModel: BaseModel{Type: ModelTypeOpenAI, Model: "gpt-4"}},
		Instruction: "test",
		HttpTools: []HttpMcpServerConfig{
			{Params: StreamableHTTPConnectionParams{Url: ""}},
		},
	}
	err := ValidateAgentConfigUsage(config)
	if err == nil {
		t.Fatal("expected error for http_tool with empty url")
	}
	if !strings.Contains(err.Error(), "http_tools") {
		t.Errorf("error should mention http_tools: %v", err)
	}
}

func TestValidateAgentConfigUsage_SseToolMissingURL(t *testing.T) {
	config := &AgentConfig{
		Model:       &OpenAI{BaseModel: BaseModel{Type: ModelTypeOpenAI, Model: "gpt-4"}},
		Instruction: "test",
		SseTools: []SseMcpServerConfig{
			{Params: SseConnectionParams{Url: ""}},
		},
	}
	err := ValidateAgentConfigUsage(config)
	if err == nil {
		t.Fatal("expected error for sse_tool with empty url")
	}
	if !strings.Contains(err.Error(), "sse_tools") {
		t.Errorf("error should mention sse_tools: %v", err)
	}
}

func TestValidateAgentConfigUsage_RemoteAgentMissingURL(t *testing.T) {
	config := &AgentConfig{
		Model:       &OpenAI{BaseModel: BaseModel{Type: ModelTypeOpenAI, Model: "gpt-4"}},
		Instruction: "test",
		RemoteAgents: []RemoteAgentConfig{
			{Name: "agent1", Url: ""},
		},
	}
	err := ValidateAgentConfigUsage(config)
	if err == nil {
		t.Fatal("expected error for remote_agent with empty url")
	}
	if !strings.Contains(err.Error(), "remote_agents") {
		t.Errorf("error should mention remote_agents: %v", err)
	}
}

func TestValidateAgentConfigUsage_RemoteAgentMissingName(t *testing.T) {
	config := &AgentConfig{
		Model:       &OpenAI{BaseModel: BaseModel{Type: ModelTypeOpenAI, Model: "gpt-4"}},
		Instruction: "test",
		RemoteAgents: []RemoteAgentConfig{
			{Name: "", Url: "http://example.com"},
		},
	}
	err := ValidateAgentConfigUsage(config)
	if err == nil {
		t.Fatal("expected error for remote_agent with empty name")
	}
	if !strings.Contains(err.Error(), "remote_agents") {
		t.Errorf("error should mention remote_agents: %v", err)
	}
}

func TestGetAgentConfigSummary_Nil(t *testing.T) {
	s := GetAgentConfigSummary(nil)
	if s != "AgentConfig: nil" {
		t.Errorf("GetAgentConfigSummary(nil) = %q, want %q", s, "AgentConfig: nil")
	}
}

func TestGetAgentConfigSummary_WithModel(t *testing.T) {
	config := &AgentConfig{
		Model:        &OpenAI{BaseModel: BaseModel{Type: ModelTypeOpenAI, Model: "gpt-4"}},
		Description:  "Test agent",
		Instruction:  "Be helpful",
		HttpTools:    []HttpMcpServerConfig{},
		SseTools:     []SseMcpServerConfig{},
		RemoteAgents: []RemoteAgentConfig{},
	}
	s := GetAgentConfigSummary(config)
	if !strings.Contains(s, "openai") {
		t.Errorf("summary should contain model type: %s", s)
	}
	if !strings.Contains(s, "gpt-4") {
		t.Errorf("summary should contain model name: %s", s)
	}
	if !strings.Contains(s, "Test agent") {
		t.Errorf("summary should contain description: %s", s)
	}
	if !strings.Contains(s, "Instruction: 10 chars") {
		t.Errorf("summary should contain instruction length: %s", s)
	}
}
