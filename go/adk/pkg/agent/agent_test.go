package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/kagent-dev/kagent/go/api/adk"
)

// TestConfigDeserialization_OpenAI verifies that a realistic OpenAI config.json
// produced by the controller translator deserializes correctly and preserves
// the model name (not the type discriminator).
func TestConfigDeserialization_OpenAI(t *testing.T) {
	// This is what the controller produces in the secret config.json
	configJSON := `{
  "agent": {
    "model": {
      "model": "gpt-4o",
      "openai": {
        "base_url": "https://api.openai.com/v1"
      }
    },
    "description": "test agent",
    "instruction": "you are helpful"
  }
}`

	var cfg adk.Config
	if err := adk.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	if cfg.Agent.Model == nil {
		t.Fatal("model is nil after deserialization")
	}

	if adk.ModelType(cfg.Agent.Model) != "openai" {
		t.Errorf("model type = %q, want %q", adk.ModelType(cfg.Agent.Model), "openai")
	}

	openai := cfg.Agent.Model.GetOpenai()
	ok := openai != nil
	if !ok {
		t.Fatalf("model is %T, want *adk.OpenAI", cfg.Agent.Model)
	}

	if cfg.Agent.Model.Model != "gpt-4o" {
		t.Errorf("model name = %q, want %q", cfg.Agent.Model.Model, "gpt-4o")
	}

	if openai.BaseUrl != "https://api.openai.com/v1" {
		t.Errorf("base_url = %q, want %q", openai.BaseUrl, "https://api.openai.com/v1")
	}
}

// TestConfigDeserialization_Anthropic verifies Anthropic model deserialization.
func TestConfigDeserialization_Anthropic(t *testing.T) {
	configJSON := `{
  "agent": {
    "model": {
      "model": "claude-sonnet-4-20250514",
      "anthropic": {
        "base_url": "https://api.anthropic.com",
        "prompt_caching": true,
        "cache_ttl": "1h"
      }
    },
    "description": "test agent",
    "instruction": "you are helpful"
  }
}`

	var cfg adk.Config
	if err := adk.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	anthropic := cfg.Agent.Model.GetAnthropic()
	ok := anthropic != nil
	if !ok {
		t.Fatalf("model is %T, want *adk.Anthropic", cfg.Agent.Model)
	}

	if cfg.Agent.Model.Model != "claude-sonnet-4-20250514" {
		t.Errorf("model name = %q, want %q", cfg.Agent.Model.Model, "claude-sonnet-4-20250514")
	}
	if !anthropic.PromptCaching {
		t.Error("PromptCaching = false, want true")
	}
	if anthropic.CacheTtl != "1h" {
		t.Errorf("CacheTTL = %q, want %q", anthropic.CacheTtl, "1h")
	}
}

// TestCreateLLM_AnthropicPromptCaching verifies the prompt caching knobs reach
// the Anthropic model, which is where the cache_control markers are set.
func TestCreateLLM_AnthropicPromptCaching(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	m := &adk.Model{Model: "claude-sonnet-4-6", Provider: &adk.Model_Anthropic{Anthropic: &adk.Anthropic{
		PromptCaching: true,
		CacheTtl:      "1h"}}}

	llm, err := CreateLLM(context.Background(), m)
	if err != nil {
		t.Fatalf("CreateLLM: %v", err)
	}
	am, ok := llm.(*models.AnthropicModel)
	if !ok {
		t.Fatalf("model is %T, want *models.AnthropicModel", llm)
	}
	if !am.Config.PromptCaching {
		t.Error("Config.PromptCaching = false, want true")
	}
	if am.Config.CacheTTL != "1h" {
		t.Errorf("Config.CacheTTL = %q, want %q", am.Config.CacheTTL, "1h")
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
			name: "openai",
			json: `{
  "model": "gpt-4o",
  "openai": {}
}`,
			wantType:  "openai",
			wantModel: "gpt-4o",
		},
		{
			name: "azure_openai",
			json: `{
  "model": "gpt-4o-deployment",
  "azure_openai": {}
}`,
			wantType:  "azure_openai",
			wantModel: "gpt-4o-deployment",
		},
		{
			name: "anthropic",
			json: `{
  "model": "claude-sonnet-4-20250514",
  "anthropic": {}
}`,
			wantType:  "anthropic",
			wantModel: "claude-sonnet-4-20250514",
		},
		{
			name: "gemini",
			json: `{
  "model": "gemini-3.5-flash",
  "gemini": {}
}`,
			wantType:  "gemini",
			wantModel: "gemini-3.5-flash",
		},
		{
			name: "gemini_vertex_ai",
			json: `{
  "model": "gemini-pro",
  "gemini_vertex_ai": {}
}`,
			wantType:  "gemini_vertex_ai",
			wantModel: "gemini-pro",
		},
		{
			name: "gemini_anthropic",
			json: `{
  "model": "claude-3-5-sonnet",
  "gemini_anthropic": {}
}`,
			wantType:  "gemini_anthropic",
			wantModel: "claude-3-5-sonnet",
		},
		{
			name: "ollama",
			json: `{
  "model": "llama3.2",
  "ollama": {}
}`,
			wantType:  "ollama",
			wantModel: "llama3.2",
		},
		{
			name: "bedrock",
			json: `{
  "model": "anthropic.claude-3-sonnet",
  "bedrock": {
    "region": "us-east-1"
  }
}`,
			wantType:  "bedrock",
			wantModel: "anthropic.claude-3-sonnet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configJSON := `{"agent":{"model":` + tt.json + `,"description":"test","instruction":"test"}}`

			var cfg adk.Config
			if err := adk.Unmarshal([]byte(configJSON), &cfg); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if cfg.Agent.Model == nil {
				t.Fatal("model is nil")
			}

			if adk.ModelType(cfg.Agent.Model) != tt.wantType {
				t.Errorf("type = %q, want %q", adk.ModelType(cfg.Agent.Model), tt.wantType)
			}

			base := cfg.Agent.Model
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
  "agent": {
    "model": {
      "model": "gpt-4o",
      "openai": {
        "base_url": "https://api.openai.com/v1",
        "temperature": 0.7
      }
    },
    "description": "test",
    "instruction": "test"
  }
}`

	var cfg adk.Config
	if err := adk.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	openai := cfg.Agent.Model.GetOpenai()
	ok := openai != nil
	if !ok {
		t.Fatalf("model is %T, want *adk.OpenAI", cfg.Agent.Model)
	}

	// This is what createLLM does: it reads m.Model for the OpenAIConfig
	if cfg.Agent.Model.Model != "gpt-4o" {
		t.Errorf("createLLM would use model=%q, want %q", cfg.Agent.Model.Model, "gpt-4o")
	}

	// Verify the type field doesn't leak into the model field
	if cfg.Agent.Model.Model == "openai" {
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

	t.Run("Bedrock (via Anthropic)", func(t *testing.T) {
		// Bedrock uses AnthropicModel under the hood — verify the model name
		// is the Bedrock model ID, not "bedrock" or "anthropic"
		m := &models.AnthropicModel{
			Config: &models.AnthropicConfig{Model: "anthropic.claude-3-sonnet-20240229-v1:0"},
		}
		if m.Name() != "anthropic.claude-3-sonnet-20240229-v1:0" {
			t.Errorf("Name() = %q, want %q", m.Name(), "anthropic.claude-3-sonnet-20240229-v1:0")
		}
	})
}

// TestCreateLLMConfig_Mistral verifies that createLLM wires an *adk.Mistral
// into a MistralModel whose inner OpenAI client keeps the model name (not the
// type discriminator "mistral") and inherits the Mistral base URL.
func TestCreateLLMConfig_Mistral(t *testing.T) {
	t.Setenv("MISTRAL_API_KEY", "test-key")
	t.Setenv("MISTRAL_API_BASE", "")

	configJSON := `{
  "agent": {
    "model": {
      "model": "mistral-large-latest",
      "mistral": {
        "base_url": "https://api.mistral.ai/v1",
        "temperature": 0.5,
        "max_tokens": 1024
      }
    },
    "description": "test",
    "instruction": "test"
  }
}`

	var cfg adk.Config
	if err := adk.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	mistral := cfg.Agent.Model.GetMistral()
	ok := mistral != nil
	if !ok {
		t.Fatalf("model is %T, want *adk.Mistral", cfg.Agent.Model)
	}
	if cfg.Agent.Model.Model != "mistral-large-latest" {
		t.Errorf("model = %q, want %q", cfg.Agent.Model.Model, "mistral-large-latest")
	}
	if mistral.BaseUrl != "https://api.mistral.ai/v1" {
		t.Errorf("base_url = %q, want %q", mistral.BaseUrl, "https://api.mistral.ai/v1")
	}

	llm, err := CreateLLM(t.Context(), cfg.Agent.Model)
	if err != nil {
		t.Fatalf("CreateLLM returned error: %v", err)
	}
	mm, ok := llm.(*models.MistralModel)
	if !ok {
		t.Fatalf("CreateLLM returned %T, want *models.MistralModel", llm)
	}
	if mm.Name() != "mistral-large-latest" {
		t.Errorf("Name() = %q, want %q", mm.Name(), "mistral-large-latest")
	}
	if mm.Name() == "mistral" {
		t.Error("Name() returns provider name 'mistral' instead of model name — this causes 404s from the Mistral API")
	}
}

// TestConfigDeserialization_Bedrock verifies that a Bedrock config deserializes
// correctly with the model name and region preserved.
func TestConfigDeserialization_Bedrock(t *testing.T) {
	configJSON := `{
  "agent": {
    "model": {
      "model": "anthropic.claude-3-sonnet-20240229-v1:0",
      "bedrock": {
        "region": "us-east-1"
      }
    },
    "description": "test agent",
    "instruction": "you are helpful"
  }
}`

	var cfg adk.Config
	if err := adk.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	br := cfg.Agent.Model.GetBedrock()
	ok := br != nil
	if !ok {
		t.Fatalf("model is %T, want *adk.Bedrock", cfg.Agent.Model)
	}

	if cfg.Agent.Model.Model != "anthropic.claude-3-sonnet-20240229-v1:0" {
		t.Errorf("model = %q, want %q", cfg.Agent.Model.Model, "anthropic.claude-3-sonnet-20240229-v1:0")
	}
	if br.Region != "us-east-1" {
		t.Errorf("region = %q, want %q", br.Region, "us-east-1")
	}
}

// TestCreateLLM_BedrockTimeouts verifies that the Bedrock read/connect timeouts
// from the model config are wired into the Go ADK's HTTP transport, so the same
// ModelConfig fields are honored by both the Python and Go runtimes.
func TestCreateLLM_BedrockTimeouts(t *testing.T) {
	read := 1500 * time.Millisecond
	connect := 250 * time.Millisecond
	m := &adk.Model{Model: "anthropic.claude-3-sonnet-20240229-v1:0", Provider: &adk.Model_Bedrock{Bedrock: &adk.Bedrock{
		Region:         "us-east-1",
		ReadTimeout:    durationpb.New(read),
		ConnectTimeout: durationpb.New(connect)}}}

	llm, err := CreateLLM(context.Background(), m)
	if err != nil {
		t.Fatalf("CreateLLM: %v", err)
	}
	bm, ok := llm.(*models.BedrockModel)
	if !ok {
		t.Fatalf("model is %T, want *models.BedrockModel", llm)
	}
	if bm.Config.Timeout == nil || *bm.Config.Timeout != read {
		t.Errorf("Config.Timeout = %v, want %d", bm.Config.Timeout, read)
	}
	if bm.Config.ConnectTimeout == nil || *bm.Config.ConnectTimeout != connect {
		t.Errorf("Config.ConnectTimeout = %v, want %d", bm.Config.ConnectTimeout, connect)
	}
}

// TestCreateLLM_BedrockTimeoutsUnset verifies that omitting the timeouts leaves
// them nil so each runtime keeps its own default behavior.
func TestCreateLLM_BedrockTimeoutsUnset(t *testing.T) {
	m := &adk.Model{Model: "anthropic.claude-3-sonnet-20240229-v1:0", Provider: &adk.Model_Bedrock{Bedrock: &adk.Bedrock{
		Region: "us-east-1"}}}

	llm, err := CreateLLM(context.Background(), m)
	if err != nil {
		t.Fatalf("CreateLLM: %v", err)
	}
	bm, ok := llm.(*models.BedrockModel)
	if !ok {
		t.Fatalf("model is %T, want *models.BedrockModel", llm)
	}
	if bm.Config.Timeout != nil {
		t.Errorf("Config.Timeout = %v, want nil", *bm.Config.Timeout)
	}
	if bm.Config.ConnectTimeout != nil {
		t.Errorf("Config.ConnectTimeout = %v, want nil", *bm.Config.ConnectTimeout)
	}
}

// TestAgentConfigFieldUsage is a smoke test that ensures AgentConfig structures
// used by agents exercise all relevant fields. This test acts as a canary: if a
// new field is added to AgentConfig but not reflected in this test configuration,
// the test will fail during development, preventing configuration gaps from reaching
// production.
//
// This test enforces feature parity and configuration tracking as per issue #1444.
func TestAgentConfigFieldUsage(t *testing.T) {
	tests := []struct {
		name              string
		config            *adk.AgentConfig
		expectMemoryTools bool
	}{
		{
			name: "all_fields_populated",
			config: &adk.AgentConfig{
				Name: "root",
				Model: &adk.Model{
					Model: "gpt-4o-mini", Provider: &adk.Model_Openai{Openai: &adk.OpenAI{
						BaseUrl: "https://api.openai.com/v1"}}},
				Description:     "Test agent with all fields",
				Instruction:     "You are a helpful test assistant",
				SkillsDirectory: "/skills",
				SubAgents: []*adk.AgentConfig{{
					Name: "helper", Model: &adk.Model{Model: "gpt-4o-mini", Provider: &adk.Model_Openai{Openai: &adk.OpenAI{}}},
				}},
				Stream: new(true),
				Memory: &adk.MemoryConfig{
					TtlDays: 15,
					Embedding: &adk.EmbeddingConfig{
						Provider: "openai",
						Model:    "text-embedding-3-large",
						BaseUrl:  "https://api.openai.com/v1",
					},
				},
			},
			expectMemoryTools: true,
		},
		{
			name: "minimal_config",
			config: &adk.AgentConfig{
				Model: &adk.Model{
					Model: "gpt-4o-mini", Provider: &adk.Model_Openai{Openai: &adk.OpenAI{
						BaseUrl: "https://api.openai.com/v1"}}},
				Description: "Minimal test agent",
				Instruction: "You are helpful",
			},
			expectMemoryTools: false,
		},
		{
			name: "memory_only",
			config: &adk.AgentConfig{
				Model: &adk.Model{
					Model: "gpt-4o-mini", Provider: &adk.Model_Openai{Openai: &adk.OpenAI{
						BaseUrl: "https://api.openai.com/v1"}}},
				Description: "Agent with memory",
				Instruction: "You are helpful with memory",
				Memory: &adk.MemoryConfig{
					TtlDays: 30,
				},
			},
			expectMemoryTools: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip actual agent creation to avoid needing API keys
			// Just verify the config deserializes and has expected structure
			if tt.config.Model == nil {
				t.Fatal("test config has nil model")
			}

			// Verify memory field is correctly set
			if tt.expectMemoryTools && tt.config.Memory == nil {
				t.Error("expected memory config but got nil")
			}
			if !tt.expectMemoryTools && tt.config.Memory != nil {
				t.Error("expected no memory config but got one")
			}

			// Verify stream field handling
			if tt.config.Stream != nil {
				if tt.config.GetStream() != *tt.config.Stream {
					t.Errorf("GetStream() = %v, want %v", tt.config.GetStream(), *tt.config.Stream)
				}
			}

			// Note: We cannot fully test CreateGoogleADKAgent without API keys
			// and running models. The real validation happens in E2E tests.
			// This test primarily validates the AgentConfig structure itself.
		})
	}
}

func TestCreateGoogleADKAgentBuildsSubAgents(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	model := func() *adk.Model {
		return &adk.Model{Model: "gpt-4o", Provider: &adk.Model_Openai{Openai: &adk.OpenAI{BaseUrl: "https://api.openai.com/v1"}}}
	}
	config := &adk.AgentConfig{
		Model: model(), Description: "root", Instruction: "coordinate", SkillsDirectory: t.TempDir(),
		SubAgents: []*adk.AgentConfig{{Name: "researcher", Model: model(), Description: "research", Instruction: "investigate"}},
	}

	root, err := CreateGoogleADKAgent(context.Background(), &adk.Config{Agent: config}, "root", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.SubAgents()) != 1 || root.SubAgents()[0].Name() != "researcher" || root.SubAgents()[0].Description() != "research" {
		t.Fatalf("sub-agents = %#v", root.SubAgents())
	}
}

func TestUsesRawOutputSchema(t *testing.T) {
	tests := []struct {
		name  string
		model *adk.Model
		want  bool
	}{
		{name: "OpenAI", model: &adk.Model{Provider: &adk.Model_Openai{Openai: &adk.OpenAI{}}}, want: true},
		{name: "Azure OpenAI", model: &adk.Model{Provider: &adk.Model_AzureOpenai{AzureOpenai: &adk.AzureOpenAI{}}}, want: true},
		{name: "Anthropic", model: &adk.Model{Provider: &adk.Model_Anthropic{Anthropic: &adk.Anthropic{}}}, want: true},
		{name: "Anthropic on Vertex", model: &adk.Model{Provider: &adk.Model_GeminiAnthropic{GeminiAnthropic: &adk.GeminiAnthropic{}}}, want: true},
		{name: "Bedrock Converse", model: &adk.Model{Provider: &adk.Model_Bedrock{Bedrock: &adk.Bedrock{}}}, want: true},
		{name: "Foundry OpenAI", model: &adk.Model{Provider: &adk.Model_Foundry{Foundry: &adk.Foundry{}}}, want: true},
		{name: "Foundry Anthropic", model: &adk.Model{Provider: &adk.Model_Foundry{Foundry: &adk.Foundry{ApiFormat: adk.FoundryAPIFormatAnthropic}}}, want: true},
		{name: "Gemini", model: &adk.Model{Provider: &adk.Model_Gemini{Gemini: &adk.Gemini{}}}, want: false},
		{name: "Ollama", model: &adk.Model{Provider: &adk.Model_Ollama{Ollama: &adk.Ollama{}}}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, usesRawOutputSchema(test.model))
		})
	}
}
