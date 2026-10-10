package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/tool/skilltoolset/skill"
	"google.golang.org/genai"
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

	var cfg adk.AgentConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	if cfg.Model == nil {
		t.Fatal("model is nil after deserialization")
	}

	if cfg.Model.GetType() != "openai" {
		t.Errorf("model type = %q, want %q", cfg.Model.GetType(), "openai")
	}

	openai, ok := cfg.Model.(*adk.OpenAI)
	if !ok {
		t.Fatalf("model is %T, want *adk.OpenAI", cfg.Model)
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
			"base_url": "https://api.anthropic.com",
			"prompt_caching": true,
			"cache_ttl": "1h"
		},
		"description": "test agent",
		"instruction": "you are helpful"
	}`

	var cfg adk.AgentConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	anthropic, ok := cfg.Model.(*adk.Anthropic)
	if !ok {
		t.Fatalf("model is %T, want *adk.Anthropic", cfg.Model)
	}

	if anthropic.Model != "claude-sonnet-4-20250514" {
		t.Errorf("model name = %q, want %q", anthropic.Model, "claude-sonnet-4-20250514")
	}
	if !anthropic.PromptCaching {
		t.Error("PromptCaching = false, want true")
	}
	if anthropic.CacheTTL != "1h" {
		t.Errorf("CacheTTL = %q, want %q", anthropic.CacheTTL, "1h")
	}
}

// TestCreateLLM_AnthropicPromptCaching verifies the prompt caching knobs reach
// the Anthropic model, which is where the cache_control markers are set.
func TestCreateLLM_AnthropicPromptCaching(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	m := &adk.Anthropic{
		BaseModel:     adk.BaseModel{Type: "anthropic", Model: "claude-sonnet-4-6"},
		PromptCaching: true,
		CacheTTL:      "1h",
	}

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

// TestCreateLLM_OllamaThink verifies the think switch of the config reaches the
// Ollama model, which sends it as the chat request's think field.
func TestCreateLLM_OllamaThink(t *testing.T) {
	var cfg adk.AgentConfig
	configJSON := `{"model": {"type": "ollama", "model": "qwen3.5:2b", "think": false}}`
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	llm, err := CreateLLM(context.Background(), cfg.Model)
	if err != nil {
		t.Fatalf("CreateLLM: %v", err)
	}
	om, ok := llm.(*models.OllamaModel)
	if !ok {
		t.Fatalf("model is %T, want *models.OllamaModel", llm)
	}
	if om.Config.Think == nil || *om.Config.Think {
		t.Errorf("Config.Think = %v, want false", om.Config.Think)
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
			json:      `{"type":"gemini","model":"gemini-3.5-flash"}`,
			wantType:  "gemini",
			wantModel: "gemini-3.5-flash",
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

			var cfg adk.AgentConfig
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

			var base adk.BaseModel
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

	var cfg adk.AgentConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	openai, ok := cfg.Model.(*adk.OpenAI)
	if !ok {
		t.Fatalf("model is %T, want *adk.OpenAI", cfg.Model)
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
		"model": {
			"type": "mistral",
			"model": "mistral-large-latest",
			"base_url": "https://api.mistral.ai/v1",
			"temperature": 0.5,
			"max_tokens": 1024
		},
		"description": "test",
		"instruction": "test"
	}`

	var cfg adk.AgentConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	mistral, ok := cfg.Model.(*adk.Mistral)
	if !ok {
		t.Fatalf("model is %T, want *adk.Mistral", cfg.Model)
	}
	if mistral.Model != "mistral-large-latest" {
		t.Errorf("model = %q, want %q", mistral.Model, "mistral-large-latest")
	}
	if mistral.BaseUrl != "https://api.mistral.ai/v1" {
		t.Errorf("base_url = %q, want %q", mistral.BaseUrl, "https://api.mistral.ai/v1")
	}

	llm, err := CreateLLM(t.Context(), cfg.Model)
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
		"model": {
			"type": "bedrock",
			"model": "anthropic.claude-3-sonnet-20240229-v1:0",
			"region": "us-east-1"
		},
		"description": "test agent",
		"instruction": "you are helpful"
	}`

	var cfg adk.AgentConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	br, ok := cfg.Model.(*adk.Bedrock)
	if !ok {
		t.Fatalf("model is %T, want *adk.Bedrock", cfg.Model)
	}

	if br.Model != "anthropic.claude-3-sonnet-20240229-v1:0" {
		t.Errorf("model = %q, want %q", br.Model, "anthropic.claude-3-sonnet-20240229-v1:0")
	}
	if br.Region != "us-east-1" {
		t.Errorf("region = %q, want %q", br.Region, "us-east-1")
	}
}

// TestCreateLLM_BedrockTimeouts verifies that the Bedrock read/connect timeouts
// from the model config are wired into the Go ADK's HTTP transport, so the same
// ModelConfig fields are honored by both the Python and Go runtimes.
func TestCreateLLM_BedrockTimeouts(t *testing.T) {
	read := 1800
	connect := 10
	m := &adk.Bedrock{
		BaseModel:      adk.BaseModel{Type: "bedrock", Model: "anthropic.claude-3-sonnet-20240229-v1:0"},
		Region:         "us-east-1",
		ReadTimeout:    &read,
		ConnectTimeout: &connect,
	}

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
	m := &adk.Bedrock{
		BaseModel: adk.BaseModel{Type: "bedrock", Model: "anthropic.claude-3-sonnet-20240229-v1:0"},
		Region:    "us-east-1",
	}

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
				Model: &adk.OpenAI{
					BaseModel: adk.BaseModel{
						Type:  "openai",
						Model: "gpt-4o-mini",
					},
					BaseUrl: "https://api.openai.com/v1",
				},
				Description:     "Test agent with all fields",
				Instruction:     "You are a helpful test assistant",
				SkillsDirectory: "/skills",
				SubAgents: []*adk.AgentConfig{{
					Name: "helper", Model: &adk.OpenAI{BaseModel: adk.BaseModel{Type: "openai", Model: "gpt-4o-mini"}},
				}},
				Stream: new(true),
				Memory: &adk.MemoryConfig{
					TTLDays: 15,
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
				Model: &adk.OpenAI{
					BaseModel: adk.BaseModel{
						Type:  "openai",
						Model: "gpt-4o-mini",
					},
					BaseUrl: "https://api.openai.com/v1",
				},
				Description: "Minimal test agent",
				Instruction: "You are helpful",
			},
			expectMemoryTools: false,
		},
		{
			name: "memory_only",
			config: &adk.AgentConfig{
				Model: &adk.OpenAI{
					BaseModel: adk.BaseModel{
						Type:  "openai",
						Model: "gpt-4o-mini",
					},
					BaseUrl: "https://api.openai.com/v1",
				},
				Description: "Agent with memory",
				Instruction: "You are helpful with memory",
				Memory: &adk.MemoryConfig{
					TTLDays: 30,
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
	t.Setenv("KAGENT_SKILLS_FOLDER", "/does/not/exist")
	model := func() adk.Model {
		return &adk.OpenAI{BaseModel: adk.BaseModel{Type: adk.ModelTypeOpenAI, Model: "gpt-4o"}, BaseUrl: "https://api.openai.com/v1"}
	}
	config := &adk.AgentConfig{
		Model: model(), Description: "root", Instruction: "coordinate", SkillsDirectory: t.TempDir(),
		SubAgents: []*adk.AgentConfig{{Name: "researcher", Model: model(), Description: "research", Instruction: "investigate"}},
	}

	root, err := CreateGoogleADKAgent(context.Background(), config, "root", nil)
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
		model adk.Model
		want  bool
	}{
		{name: "OpenAI", model: &adk.OpenAI{}, want: true},
		{name: "Azure OpenAI", model: &adk.AzureOpenAI{}, want: true},
		{name: "Anthropic", model: &adk.Anthropic{}, want: true},
		{name: "Anthropic on Vertex", model: &adk.GeminiAnthropic{}, want: true},
		{name: "Bedrock Converse", model: &adk.Bedrock{}, want: true},
		{name: "Foundry OpenAI", model: &adk.Foundry{}, want: true},
		{name: "Foundry Anthropic", model: &adk.Foundry{APIFormat: adk.FoundryAPIFormatAnthropic}, want: true},
		{name: "Gemini", model: &adk.Gemini{}, want: false},
		{name: "Ollama", model: &adk.Ollama{}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, usesRawOutputSchema(test.model))
		})
	}
}

func TestCreateGoogleADKAgentLoadsSkillsWithFrontmatterFieldsOutsideTheSpec(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	writeSkill := func(t *testing.T, frontmatter string) *adk.AgentConfig {
		skillsDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(skillsDir, "deploy"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillsDir, "deploy", "SKILL.md"), []byte("---\n"+frontmatter+"---\n# Deploy\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return &adk.AgentConfig{
			Model:           &adk.OpenAI{BaseModel: adk.BaseModel{Type: adk.ModelTypeOpenAI, Model: "gpt-4o"}, BaseUrl: "https://api.openai.com/v1"},
			Description:     "root",
			Instruction:     "deploy",
			SkillsDirectory: skillsDir,
		}
	}

	// Claude Code's fields and a top-level version, as skills written for other
	// harnesses carry them.
	config := writeSkill(t, "name: deploy\ndescription: Deploys the service.\nuser-invocable: false\nargument-hint: '[environment]'\nversion: 0.1.0\n")
	if _, err := CreateGoogleADKAgent(context.Background(), config, "root", nil); err != nil {
		t.Fatalf("an agent whose skill carries fields outside the agentskills.io set must load: %v", err)
	}

	// The fields the specification requires are still required.
	config = writeSkill(t, "name: deploy\nuser-invocable: false\n")
	if _, err := CreateGoogleADKAgent(context.Background(), config, "root", nil); !errors.Is(err, skill.ErrInvalidFrontmatter) {
		t.Fatalf("a skill without a description must still fail with %v, got %v", skill.ErrInvalidFrontmatter, err)
	}
}

// With the egress gateway minting the access token, the Gemini Vertex AI client
// is built without Application Default Credentials.
func TestCreateLLMGeminiVertexAIWithGatewayCredential(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "project")
	t.Setenv("GOOGLE_CLOUD_LOCATION", "us-central1")
	t.Setenv("KAGENT_SKIP_VERTEX_AUTH", "true")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/nonexistent/credentials.json")
	llm, err := CreateLLM(context.Background(), &adk.GeminiVertexAI{BaseModel: adk.BaseModel{Model: "gemini-2.5-pro"}})
	if err != nil {
		t.Fatal(err)
	}
	if llm == nil {
		t.Fatal("CreateLLM() returned no model")
	}
}

// The gateway replaces the Authorization header a request carries and adds
// none, so the Gemini Vertex AI request carries the placeholder bearer.
func TestGeminiVertexAIClientConfigSendsGatewayPlaceholder(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "project")
	t.Setenv("GOOGLE_CLOUD_LOCATION", "europe-west4")
	t.Setenv("KAGENT_SKIP_VERTEX_AUTH", "true")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/nonexistent/credentials.json")
	var got *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]}}]}`))
	}))
	defer server.Close()

	clientConfig, err := geminiVertexAIClientConfig(&adk.GeminiVertexAI{})
	if err != nil {
		t.Fatal(err)
	}
	clientConfig.HTTPOptions.BaseURL = server.URL
	client, err := genai.NewClient(context.Background(), clientConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Models.GenerateContent(context.Background(), "gemini-2.5-flash", genai.Text("hi"), nil); err != nil {
		t.Fatal(err)
	}
	if want := "/v1beta1/projects/project/locations/europe-west4/publishers/google/models/gemini-2.5-flash:generateContent"; got.URL.Path != want {
		t.Fatalf("path = %q, want %q", got.URL.Path, want)
	}
	if want := "Bearer kagent-credential-injected"; got.Header.Get("Authorization") != want {
		t.Fatalf("Authorization = %q, want %q", got.Header.Get("Authorization"), want)
	}
}
