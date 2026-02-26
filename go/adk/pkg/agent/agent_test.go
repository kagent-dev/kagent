package agent

import (
	"encoding/json"
	"testing"

	"github.com/kagent-dev/kagent/go/adk/pkg/config"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
)

// TestConfigDeserialization_OpenAI verifies that a realistic OpenAI config.json
// produced by the controller translator deserializes correctly and preserves
// the model name (not the type discriminator).
func TestConfigDeserialization_OpenAI(t *testing.T) {
	// This is what the controller produces in the secret config.json
	configJSON := `{
		"model": {
			"type": "openai",
			"model": "gpt-4o",
			"base_url": "https://api.openai.com/v1"
		},
		"description": "test agent",
		"instruction": "you are helpful"
	}`

	var cfg config.AgentConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	if cfg.Model == nil {
		t.Fatal("model is nil after deserialization")
	}

	if cfg.Model.GetType() != "openai" {
		t.Errorf("model type = %q, want %q", cfg.Model.GetType(), "openai")
	}

	openai, ok := cfg.Model.(*config.OpenAI)
	if !ok {
		t.Fatalf("model is %T, want *config.OpenAI", cfg.Model)
	}

	if openai.Model != "gpt-4o" {
		t.Errorf("model name = %q, want %q", openai.Model, "gpt-4o")
	}

	if openai.BaseUrl != "https://api.openai.com/v1" {
		t.Errorf("base_url = %q, want %q", openai.BaseUrl, "https://api.openai.com/v1")
	}
}

// TestConfigDeserialization_Anthropic verifies Anthropic model deserialization.
func TestConfigDeserialization_Anthropic(t *testing.T) {
	configJSON := `{
		"model": {
			"type": "anthropic",
			"model": "claude-sonnet-4-20250514",
			"base_url": "https://api.anthropic.com"
		},
		"description": "test agent",
		"instruction": "you are helpful"
	}`

	var cfg config.AgentConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	anthropic, ok := cfg.Model.(*config.Anthropic)
	if !ok {
		t.Fatalf("model is %T, want *config.Anthropic", cfg.Model)
	}

	if anthropic.Model != "claude-sonnet-4-20250514" {
		t.Errorf("model name = %q, want %q", anthropic.Model, "claude-sonnet-4-20250514")
	}
}

// TestConfigDeserialization_AllTypes verifies every model type deserializes with
// the correct model name preserved.
func TestConfigDeserialization_AllTypes(t *testing.T) {
	tests := []struct {
		name      string
		json      string
		wantType  string
		wantModel string
	}{
		{
			name:      "openai",
			json:      `{"type":"openai","model":"gpt-4o"}`,
			wantType:  "openai",
			wantModel: "gpt-4o",
		},
		{
			name:      "azure_openai",
			json:      `{"type":"azure_openai","model":"gpt-4o-deployment"}`,
			wantType:  "azure_openai",
			wantModel: "gpt-4o-deployment",
		},
		{
			name:      "anthropic",
			json:      `{"type":"anthropic","model":"claude-sonnet-4-20250514"}`,
			wantType:  "anthropic",
			wantModel: "claude-sonnet-4-20250514",
		},
		{
			name:      "gemini",
			json:      `{"type":"gemini","model":"gemini-2.0-flash"}`,
			wantType:  "gemini",
			wantModel: "gemini-2.0-flash",
		},
		{
			name:      "gemini_vertex_ai",
			json:      `{"type":"gemini_vertex_ai","model":"gemini-pro"}`,
			wantType:  "gemini_vertex_ai",
			wantModel: "gemini-pro",
		},
		{
			name:      "gemini_anthropic",
			json:      `{"type":"gemini_anthropic","model":"claude-3-5-sonnet"}`,
			wantType:  "gemini_anthropic",
			wantModel: "claude-3-5-sonnet",
		},
		{
			name:      "ollama",
			json:      `{"type":"ollama","model":"llama3.2"}`,
			wantType:  "ollama",
			wantModel: "llama3.2",
		},
		{
			name:      "bedrock",
			json:      `{"type":"bedrock","model":"anthropic.claude-3-sonnet","region":"us-east-1"}`,
			wantType:  "bedrock",
			wantModel: "anthropic.claude-3-sonnet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configJSON := `{"model":` + tt.json + `,"description":"test","instruction":"test"}`

			var cfg config.AgentConfig
			if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if cfg.Model == nil {
				t.Fatal("model is nil")
			}

			if cfg.Model.GetType() != tt.wantType {
				t.Errorf("type = %q, want %q", cfg.Model.GetType(), tt.wantType)
			}

			// Use BaseModel to check the model name generically
			modelJSON, err := json.Marshal(cfg.Model)
			if err != nil {
				t.Fatalf("failed to marshal model: %v", err)
			}

			var base config.BaseModel
			if err := json.Unmarshal(modelJSON, &base); err != nil {
				t.Fatalf("failed to unmarshal base: %v", err)
			}

			if base.Model != tt.wantModel {
				t.Errorf("model name = %q, want %q", base.Model, tt.wantModel)
			}
		})
	}
}

// TestCreateLLMConfig_OpenAI verifies that createLLM passes the correct model
// name (not the type discriminator) to the OpenAI config.
func TestCreateLLMConfig_OpenAI(t *testing.T) {
	configJSON := `{
		"model": {
			"type": "openai",
			"model": "gpt-4o",
			"base_url": "https://api.openai.com/v1",
			"temperature": 0.7
		},
		"description": "test",
		"instruction": "test"
	}`

	var cfg config.AgentConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	openai, ok := cfg.Model.(*config.OpenAI)
	if !ok {
		t.Fatalf("model is %T, want *config.OpenAI", cfg.Model)
	}

	// This is what createLLM does: it reads m.Model for the OpenAIConfig
	if openai.Model != "gpt-4o" {
		t.Errorf("createLLM would use model=%q, want %q", openai.Model, "gpt-4o")
	}

	// Verify the type field doesn't leak into the model field
	if openai.Model == "openai" {
		t.Error("model name is the type discriminator 'openai' — this is the bug that causes 404s from the OpenAI API")
	}

	// Verify temperature is preserved
	if openai.Temperature == nil || *openai.Temperature != 0.7 {
		t.Errorf("temperature = %v, want 0.7", openai.Temperature)
	}
}

// TestModelName_ReturnsModelNotProvider verifies that the LLM Name() method
// returns the actual model name (e.g. "gpt-4o") rather than the provider name
// (e.g. "openai"). The Google ADK framework uses Name() to set req.Model in
// API calls, so returning the provider name causes 404 errors.
func TestModelName_ReturnsModelNotProvider(t *testing.T) {
	t.Run("OpenAI", func(t *testing.T) {
		m := &models.OpenAIModel{
			Config: &models.OpenAIConfig{Model: "gpt-4o"},
		}
		if m.Name() != "gpt-4o" {
			t.Errorf("Name() = %q, want %q", m.Name(), "gpt-4o")
		}
		if m.Name() == "openai" {
			t.Error("Name() returns provider name 'openai' instead of model name — this causes 404 from OpenAI API")
		}
	})

	t.Run("Anthropic", func(t *testing.T) {
		m := &models.AnthropicModel{
			Config: &models.AnthropicConfig{Model: "claude-sonnet-4-20250514"},
		}
		if m.Name() != "claude-sonnet-4-20250514" {
			t.Errorf("Name() = %q, want %q", m.Name(), "claude-sonnet-4-20250514")
		}
		if m.Name() == "anthropic" {
			t.Error("Name() returns provider name 'anthropic' instead of model name — this causes 404 from Anthropic API")
		}
	})
}
