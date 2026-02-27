package adk

import (
	"encoding/json"
	"testing"
)

func boolPtr(b bool) *bool          { return &b }
func stringPtr(s string) *string    { return &s }
func float64Ptr(f float64) *float64 { return &f }
func intPtr(i int) *int             { return &i }

func TestMarshalJSON_TypeDiscriminator(t *testing.T) {
	tests := []struct {
		name     string
		model    Model
		wantType string
	}{
		{name: "OpenAI", model: &OpenAI{BaseModel: BaseModel{Model: "gpt-4o"}}, wantType: ModelTypeOpenAI},
		{name: "AzureOpenAI", model: &AzureOpenAI{BaseModel: BaseModel{Model: "gpt-4o"}}, wantType: ModelTypeAzureOpenAI},
		{name: "Anthropic", model: &Anthropic{BaseModel: BaseModel{Model: "claude-3"}}, wantType: ModelTypeAnthropic},
		{name: "GeminiVertexAI", model: &GeminiVertexAI{BaseModel: BaseModel{Model: "gemini-pro"}}, wantType: ModelTypeGeminiVertexAI},
		{name: "GeminiAnthropic", model: &GeminiAnthropic{BaseModel: BaseModel{Model: "gemini-pro"}}, wantType: ModelTypeGeminiAnthropic},
		{name: "Ollama", model: &Ollama{BaseModel: BaseModel{Model: "llama3"}}, wantType: ModelTypeOllama},
		{name: "Gemini", model: &Gemini{BaseModel: BaseModel{Model: "gemini-pro"}}, wantType: ModelTypeGemini},
		{name: "Bedrock", model: &Bedrock{BaseModel: BaseModel{Model: "claude-v2"}}, wantType: ModelTypeBedrock},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.model)
			if err != nil {
				t.Fatalf("MarshalJSON() error = %v", err)
			}

			var raw map[string]json.RawMessage
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}

			var gotType string
			if err := json.Unmarshal(raw["type"], &gotType); err != nil {
				t.Fatalf("failed to unmarshal type field: %v", err)
			}
			if gotType != tt.wantType {
				t.Errorf("type = %q, want %q", gotType, tt.wantType)
			}
		})
	}
}

func TestMarshalJSON_OmitemptyFields(t *testing.T) {
	tests := []struct {
		name       string
		model      Model
		wantAbsent []string
	}{
		{
			name:       "OpenAI zero-valued omitempty fields omitted",
			model:      &OpenAI{BaseModel: BaseModel{Model: "gpt-4o"}},
			wantAbsent: []string{"headers", "tls_disable_verify", "tls_ca_cert_path", "tls_disable_system_cas", "api_key_passthrough", "frequency_penalty", "max_tokens", "temperature"},
		},
		{
			name:       "Anthropic zero-valued omitempty fields omitted",
			model:      &Anthropic{BaseModel: BaseModel{Model: "claude-3"}},
			wantAbsent: []string{"headers", "tls_disable_verify", "tls_ca_cert_path", "tls_disable_system_cas", "api_key_passthrough"},
		},
		{
			name:       "Bedrock zero-valued omitempty fields omitted",
			model:      &Bedrock{BaseModel: BaseModel{Model: "claude-v2"}},
			wantAbsent: []string{"headers", "region", "api_key_passthrough"},
		},
		{
			name:       "Ollama zero-valued omitempty fields omitted",
			model:      &Ollama{BaseModel: BaseModel{Model: "llama3"}},
			wantAbsent: []string{"headers", "options", "api_key_passthrough"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.model)
			if err != nil {
				t.Fatalf("MarshalJSON() error = %v", err)
			}

			var raw map[string]json.RawMessage
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}

			for _, field := range tt.wantAbsent {
				if _, ok := raw[field]; ok {
					t.Errorf("field %q should be omitted when zero-valued, but was present", field)
				}
			}
		})
	}
}

func TestMarshalJSON_BaseModelFields(t *testing.T) {
	base := BaseModel{
		Model:               "test-model",
		Headers:             map[string]string{"X-Custom": "value"},
		TLSDisableVerify:    boolPtr(true),
		TLSCACertPath:       stringPtr("/etc/ssl/ca.crt"),
		TLSDisableSystemCAs: boolPtr(false),
		APIKeyPassthrough:   true,
	}

	tests := []struct {
		name  string
		model Model
	}{
		{name: "OpenAI", model: &OpenAI{BaseModel: base}},
		{name: "AzureOpenAI", model: &AzureOpenAI{BaseModel: base}},
		{name: "Anthropic", model: &Anthropic{BaseModel: base}},
		{name: "GeminiVertexAI", model: &GeminiVertexAI{BaseModel: base}},
		{name: "GeminiAnthropic", model: &GeminiAnthropic{BaseModel: base}},
		{name: "Ollama", model: &Ollama{BaseModel: base}},
		{name: "Gemini", model: &Gemini{BaseModel: base}},
		{name: "Bedrock", model: &Bedrock{BaseModel: base}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.model)
			if err != nil {
				t.Fatalf("MarshalJSON() error = %v", err)
			}

			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}

			if raw["model"] != "test-model" {
				t.Errorf("model = %v, want %q", raw["model"], "test-model")
			}

			headers, ok := raw["headers"].(map[string]any)
			if !ok {
				t.Fatal("headers field missing or wrong type")
			}
			if headers["X-Custom"] != "value" {
				t.Errorf("headers[X-Custom] = %v, want %q", headers["X-Custom"], "value")
			}

			if raw["tls_disable_verify"] != true {
				t.Errorf("tls_disable_verify = %v, want true", raw["tls_disable_verify"])
			}
			if raw["tls_ca_cert_path"] != "/etc/ssl/ca.crt" {
				t.Errorf("tls_ca_cert_path = %v, want %q", raw["tls_ca_cert_path"], "/etc/ssl/ca.crt")
			}
			if raw["tls_disable_system_cas"] != false {
				t.Errorf("tls_disable_system_cas = %v, want false", raw["tls_disable_system_cas"])
			}
			if raw["api_key_passthrough"] != true {
				t.Errorf("api_key_passthrough = %v, want true", raw["api_key_passthrough"])
			}
		})
	}
}

func TestMarshalJSON_TypeSpecificFields(t *testing.T) {
	t.Run("OpenAI fields", func(t *testing.T) {
		m := &OpenAI{
			BaseModel:       BaseModel{Model: "gpt-4o"},
			BaseUrl:         "https://api.openai.com",
			MaxTokens:       intPtr(1024),
			Temperature:     float64Ptr(0.7),
			ReasoningEffort: stringPtr("low"),
		}
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("MarshalJSON() error = %v", err)
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if raw["base_url"] != "https://api.openai.com" {
			t.Errorf("base_url = %v, want %q", raw["base_url"], "https://api.openai.com")
		}
		if raw["max_tokens"] != float64(1024) {
			t.Errorf("max_tokens = %v, want 1024", raw["max_tokens"])
		}
		if raw["temperature"] != 0.7 {
			t.Errorf("temperature = %v, want 0.7", raw["temperature"])
		}
		if raw["reasoning_effort"] != "low" {
			t.Errorf("reasoning_effort = %v, want %q", raw["reasoning_effort"], "low")
		}
	})

	t.Run("Anthropic base_url", func(t *testing.T) {
		m := &Anthropic{
			BaseModel: BaseModel{Model: "claude-3"},
			BaseUrl:   "https://api.anthropic.com",
		}
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("MarshalJSON() error = %v", err)
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if raw["base_url"] != "https://api.anthropic.com" {
			t.Errorf("base_url = %v, want %q", raw["base_url"], "https://api.anthropic.com")
		}
	})

	t.Run("Ollama options", func(t *testing.T) {
		m := &Ollama{
			BaseModel: BaseModel{Model: "llama3"},
			Options:   map[string]string{"num_ctx": "2048"},
		}
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("MarshalJSON() error = %v", err)
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		opts, ok := raw["options"].(map[string]any)
		if !ok {
			t.Fatal("options field missing or wrong type")
		}
		if opts["num_ctx"] != "2048" {
			t.Errorf("options[num_ctx] = %v, want %q", opts["num_ctx"], "2048")
		}
	})

	t.Run("Bedrock region", func(t *testing.T) {
		m := &Bedrock{
			BaseModel: BaseModel{Model: "claude-v2"},
			Region:    "us-east-1",
		}
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("MarshalJSON() error = %v", err)
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if raw["region"] != "us-east-1" {
			t.Errorf("region = %v, want %q", raw["region"], "us-east-1")
		}
	})
}

func TestParseModel_Roundtrip(t *testing.T) {
	tests := []struct {
		name     string
		model    Model
		wantType string
	}{
		{
			name: "OpenAI roundtrip",
			model: &OpenAI{
				BaseModel:   BaseModel{Model: "gpt-4o", Headers: map[string]string{"X-Key": "val"}},
				BaseUrl:     "https://api.openai.com",
				Temperature: float64Ptr(0.7),
				MaxTokens:   intPtr(1024),
			},
			wantType: ModelTypeOpenAI,
		},
		{
			name: "Anthropic roundtrip",
			model: &Anthropic{
				BaseModel: BaseModel{Model: "claude-3", APIKeyPassthrough: true},
				BaseUrl:   "https://api.anthropic.com",
			},
			wantType: ModelTypeAnthropic,
		},
		{
			name:     "AzureOpenAI roundtrip",
			model:    &AzureOpenAI{BaseModel: BaseModel{Model: "gpt-4o"}},
			wantType: ModelTypeAzureOpenAI,
		},
		{
			name:     "GeminiVertexAI roundtrip",
			model:    &GeminiVertexAI{BaseModel: BaseModel{Model: "gemini-pro"}},
			wantType: ModelTypeGeminiVertexAI,
		},
		{
			name:     "GeminiAnthropic roundtrip",
			model:    &GeminiAnthropic{BaseModel: BaseModel{Model: "gemini-pro"}},
			wantType: ModelTypeGeminiAnthropic,
		},
		{
			name: "Ollama roundtrip",
			model: &Ollama{
				BaseModel: BaseModel{Model: "llama3", Headers: map[string]string{"User-Agent": "test"}},
				Options:   map[string]string{"num_ctx": "2048", "temperature": "0.8"},
			},
			wantType: ModelTypeOllama,
		},
		{
			name:     "Gemini roundtrip",
			model:    &Gemini{BaseModel: BaseModel{Model: "gemini-pro"}},
			wantType: ModelTypeGemini,
		},
		{
			name: "Bedrock roundtrip",
			model: &Bedrock{
				BaseModel: BaseModel{Model: "claude-v2", TLSDisableVerify: boolPtr(true)},
				Region:    "us-west-2",
			},
			wantType: ModelTypeBedrock,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.model)
			if err != nil {
				t.Fatalf("MarshalJSON() error = %v", err)
			}

			parsed, err := ParseModel(data)
			if err != nil {
				t.Fatalf("ParseModel() error = %v", err)
			}

			if parsed.GetType() != tt.wantType {
				t.Errorf("ParseModel().GetType() = %q, want %q", parsed.GetType(), tt.wantType)
			}

			// Re-marshal and compare
			data2, err := json.Marshal(parsed)
			if err != nil {
				t.Fatalf("second MarshalJSON() error = %v", err)
			}

			if string(data) != string(data2) {
				t.Errorf("roundtrip mismatch:\n  first:  %s\n  second: %s", string(data), string(data2))
			}
		})
	}
}

func TestParseModel_UnknownType(t *testing.T) {
	data := []byte(`{"type":"unknown","model":"test"}`)
	_, err := ParseModel(data)
	if err == nil {
		t.Fatal("ParseModel() expected error for unknown type, got nil")
	}
}

// --- AgentConfig marshal/unmarshal tests ---

func TestAgentConfig_UnmarshalJSON_Minimal(t *testing.T) {
	data := []byte(`{
		"model": {"type":"openai","model":"gpt-4o"},
		"description": "test agent",
		"instruction": "be helpful",
		"stream": true
	}`)
	var cfg AgentConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}
	if cfg.Model.GetType() != ModelTypeOpenAI {
		t.Errorf("Model.GetType() = %q, want %q", cfg.Model.GetType(), ModelTypeOpenAI)
	}
	if cfg.Description != "test agent" {
		t.Errorf("Description = %q, want %q", cfg.Description, "test agent")
	}
	if cfg.Instruction != "be helpful" {
		t.Errorf("Instruction = %q, want %q", cfg.Instruction, "be helpful")
	}
	if !cfg.Stream {
		t.Error("Stream = false, want true")
	}
	if cfg.Memory != nil {
		t.Error("Memory should be nil")
	}
	if cfg.ContextConfig != nil {
		t.Error("ContextConfig should be nil")
	}
}

func TestAgentConfig_UnmarshalJSON_WithMemory(t *testing.T) {
	data := []byte(`{
		"model": {"type":"openai","model":"gpt-4o"},
		"description": "d",
		"instruction": "i",
		"stream": false,
		"memory": {"ttl_days": 30, "embedding": {"provider":"openai","model":"text-embedding-3-small"}}
	}`)
	var cfg AgentConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}
	if cfg.Memory == nil {
		t.Fatal("Memory should not be nil")
	}
	if cfg.Memory.TTLDays != 30 {
		t.Errorf("Memory.TTLDays = %d, want 30", cfg.Memory.TTLDays)
	}
	if cfg.Memory.Embedding == nil {
		t.Fatal("Memory.Embedding should not be nil")
	}
	if cfg.Memory.Embedding.Provider != "openai" {
		t.Errorf("Memory.Embedding.Provider = %q, want %q", cfg.Memory.Embedding.Provider, "openai")
	}
	if cfg.Memory.Embedding.Model != "text-embedding-3-small" {
		t.Errorf("Memory.Embedding.Model = %q, want %q", cfg.Memory.Embedding.Model, "text-embedding-3-small")
	}
}

func TestAgentConfig_UnmarshalJSON_NullMemory(t *testing.T) {
	data := []byte(`{
		"model": {"type":"openai","model":"gpt-4o"},
		"description": "d",
		"instruction": "i",
		"stream": false,
		"memory": null
	}`)
	var cfg AgentConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}
	if cfg.Memory != nil {
		t.Error("Memory should be nil for null value")
	}
}

func TestAgentConfig_UnmarshalJSON_WithContextConfig(t *testing.T) {
	data := []byte(`{
		"model": {"type":"anthropic","model":"claude-3","base_url":""},
		"description": "d",
		"instruction": "i",
		"stream": false,
		"context_config": {
			"compaction": {
				"compaction_interval": 5,
				"overlap_size": 2,
				"summarizer_model": {"type":"openai","model":"gpt-4o-mini","base_url":""},
				"prompt_template": "Summarize this",
				"token_threshold": 50000,
				"event_retention_size": 10
			}
		}
	}`)
	var cfg AgentConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}
	if cfg.Model.GetType() != ModelTypeAnthropic {
		t.Errorf("Model.GetType() = %q, want %q", cfg.Model.GetType(), ModelTypeAnthropic)
	}
	if cfg.ContextConfig == nil {
		t.Fatal("ContextConfig should not be nil")
	}

	comp := cfg.ContextConfig.Compaction
	if comp == nil {
		t.Fatal("Compaction should not be nil")
	}
	if *comp.CompactionInterval != 5 {
		t.Errorf("CompactionInterval = %d, want 5", *comp.CompactionInterval)
	}
	if *comp.OverlapSize != 2 {
		t.Errorf("OverlapSize = %d, want 2", *comp.OverlapSize)
	}
	if comp.SummarizerModel == nil {
		t.Fatal("SummarizerModel should not be nil")
	}
	if comp.SummarizerModel.GetType() != ModelTypeOpenAI {
		t.Errorf("SummarizerModel.GetType() = %q, want %q", comp.SummarizerModel.GetType(), ModelTypeOpenAI)
	}
	if comp.PromptTemplate != "Summarize this" {
		t.Errorf("PromptTemplate = %q, want %q", comp.PromptTemplate, "Summarize this")
	}
	if *comp.TokenThreshold != 50000 {
		t.Errorf("TokenThreshold = %d, want 50000", *comp.TokenThreshold)
	}
	if *comp.EventRetentionSize != 10 {
		t.Errorf("EventRetentionSize = %d, want 10", *comp.EventRetentionSize)
	}
}

func TestAgentConfig_UnmarshalJSON_ContextConfig_CompactionOnly(t *testing.T) {
	data := []byte(`{
		"model": {"type":"openai","model":"gpt-4o"},
		"description": "d",
		"instruction": "i",
		"stream": false,
		"context_config": {
			"compaction": {
				"compaction_interval": 10,
				"overlap_size": 3
			}
		}
	}`)
	var cfg AgentConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}
	if cfg.ContextConfig == nil {
		t.Fatal("ContextConfig should not be nil")
	}
	if cfg.ContextConfig.Compaction == nil {
		t.Fatal("Compaction should not be nil")
	}
	if *cfg.ContextConfig.Compaction.CompactionInterval != 10 {
		t.Errorf("CompactionInterval = %d, want 10", *cfg.ContextConfig.Compaction.CompactionInterval)
	}
	if cfg.ContextConfig.Compaction.SummarizerModel != nil {
		t.Error("SummarizerModel should be nil when not provided")
	}
	if cfg.ContextConfig.Compaction.PromptTemplate != "" {
		t.Errorf("PromptTemplate = %q, want empty", cfg.ContextConfig.Compaction.PromptTemplate)
	}
	if cfg.ContextConfig.Compaction.TokenThreshold != nil {
		t.Error("TokenThreshold should be nil when not provided")
	}
}

func TestAgentConfig_Roundtrip(t *testing.T) {
	original := &AgentConfig{
		Model:       &OpenAI{BaseModel: BaseModel{Model: "gpt-4o"}, BaseUrl: "https://api.openai.com"},
		Description: "test",
		Instruction: "be helpful",
		Stream:      true,
		ExecuteCode: true,
		HttpTools: []HttpMcpServerConfig{
			{
				Params: StreamableHTTPConnectionParams{Url: "http://localhost:8080"},
				Tools:  []string{"tool1"},
			},
		},
		RemoteAgents: []RemoteAgentConfig{
			{Name: "agent1", Url: "http://agent1:8080", Description: "remote agent"},
		},
		Memory: &MemoryConfig{
			TTLDays:   15,
			Embedding: &EmbeddingConfig{Provider: "openai", Model: "text-embedding-3-small"},
		},
		ContextConfig: &AgentContextConfig{
			Compaction: &AgentCompressionConfig{
				CompactionInterval: intPtr(5),
				OverlapSize:        intPtr(2),
				SummarizerModel:    &Anthropic{BaseModel: BaseModel{Model: "claude-3-haiku"}, BaseUrl: ""},
				PromptTemplate:     "Summarize",
				TokenThreshold:     intPtr(50000),
				EventRetentionSize: intPtr(10),
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var parsed AgentConfig
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	// Verify model roundtrip
	if parsed.Model.GetType() != ModelTypeOpenAI {
		t.Errorf("Model.GetType() = %q, want %q", parsed.Model.GetType(), ModelTypeOpenAI)
	}
	if parsed.Description != original.Description {
		t.Errorf("Description = %q, want %q", parsed.Description, original.Description)
	}
	if parsed.Stream != original.Stream {
		t.Errorf("Stream = %v, want %v", parsed.Stream, original.Stream)
	}
	if parsed.ExecuteCode != original.ExecuteCode {
		t.Errorf("ExecuteCode = %v, want %v", parsed.ExecuteCode, original.ExecuteCode)
	}

	// Verify HttpTools roundtrip
	if len(parsed.HttpTools) != 1 {
		t.Fatalf("HttpTools len = %d, want 1", len(parsed.HttpTools))
	}
	if parsed.HttpTools[0].Params.Url != "http://localhost:8080" {
		t.Errorf("HttpTools[0].Params.Url = %q, want %q", parsed.HttpTools[0].Params.Url, "http://localhost:8080")
	}

	// Verify RemoteAgents roundtrip
	if len(parsed.RemoteAgents) != 1 {
		t.Fatalf("RemoteAgents len = %d, want 1", len(parsed.RemoteAgents))
	}
	if parsed.RemoteAgents[0].Name != "agent1" {
		t.Errorf("RemoteAgents[0].Name = %q, want %q", parsed.RemoteAgents[0].Name, "agent1")
	}

	// Verify Memory roundtrip
	if parsed.Memory == nil {
		t.Fatal("Memory should not be nil")
	}
	if parsed.Memory.TTLDays != 15 {
		t.Errorf("Memory.TTLDays = %d, want 15", parsed.Memory.TTLDays)
	}
	if parsed.Memory.Embedding.Provider != "openai" {
		t.Errorf("Memory.Embedding.Provider = %q, want %q", parsed.Memory.Embedding.Provider, "openai")
	}

	// Verify ContextConfig roundtrip
	if parsed.ContextConfig == nil {
		t.Fatal("ContextConfig should not be nil")
	}
	if parsed.ContextConfig.Compaction == nil {
		t.Fatal("Compaction should not be nil")
	}
	if *parsed.ContextConfig.Compaction.CompactionInterval != 5 {
		t.Errorf("CompactionInterval = %d, want 5", *parsed.ContextConfig.Compaction.CompactionInterval)
	}
	if *parsed.ContextConfig.Compaction.OverlapSize != 2 {
		t.Errorf("OverlapSize = %d, want 2", *parsed.ContextConfig.Compaction.OverlapSize)
	}
	if parsed.ContextConfig.Compaction.SummarizerModel == nil {
		t.Fatal("SummarizerModel should not be nil after roundtrip")
	}
	if parsed.ContextConfig.Compaction.SummarizerModel.GetType() != ModelTypeAnthropic {
		t.Errorf("SummarizerModel.GetType() = %q, want %q", parsed.ContextConfig.Compaction.SummarizerModel.GetType(), ModelTypeAnthropic)
	}
	if parsed.ContextConfig.Compaction.PromptTemplate != "Summarize" {
		t.Errorf("PromptTemplate = %q, want %q", parsed.ContextConfig.Compaction.PromptTemplate, "Summarize")
	}
	if *parsed.ContextConfig.Compaction.TokenThreshold != 50000 {
		t.Errorf("TokenThreshold = %d, want 50000", *parsed.ContextConfig.Compaction.TokenThreshold)
	}
	if *parsed.ContextConfig.Compaction.EventRetentionSize != 10 {
		t.Errorf("EventRetentionSize = %d, want 10", *parsed.ContextConfig.Compaction.EventRetentionSize)
	}

	// Re-marshal and compare JSON
	data2, err := json.Marshal(&parsed)
	if err != nil {
		t.Fatalf("second Marshal() error = %v", err)
	}

	// Normalize by unmarshalling both into maps
	var map1, map2 map[string]any
	json.Unmarshal(data, &map1)
	json.Unmarshal(data2, &map2)

	j1, _ := json.Marshal(map1)
	j2, _ := json.Marshal(map2)
	if string(j1) != string(j2) {
		t.Errorf("roundtrip mismatch:\n  first:  %s\n  second: %s", string(j1), string(j2))
	}
}

func TestAgentConfig_UnmarshalJSON_WithTools(t *testing.T) {
	data := []byte(`{
		"model": {"type":"openai","model":"gpt-4o"},
		"description": "d",
		"instruction": "i",
		"stream": false,
		"http_tools": [
			{
				"params": {"url": "http://mcp.example.com/sse", "headers": {"Authorization": "Bearer token"}},
				"tools": ["search", "browse"]
			}
		],
		"sse_tools": [
			{
				"params": {"url": "http://sse.example.com"},
				"tools": ["stream_tool"]
			}
		],
		"remote_agents": [
			{"name": "helper", "url": "http://helper:8080", "description": "A helper"}
		]
	}`)
	var cfg AgentConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}

	if len(cfg.HttpTools) != 1 {
		t.Fatalf("HttpTools len = %d, want 1", len(cfg.HttpTools))
	}
	if cfg.HttpTools[0].Params.Url != "http://mcp.example.com/sse" {
		t.Errorf("HttpTools[0].Params.Url = %q", cfg.HttpTools[0].Params.Url)
	}
	if len(cfg.HttpTools[0].Tools) != 2 {
		t.Errorf("HttpTools[0].Tools len = %d, want 2", len(cfg.HttpTools[0].Tools))
	}
	if cfg.HttpTools[0].Params.Headers["Authorization"] != "Bearer token" {
		t.Errorf("HttpTools[0].Params.Headers[Authorization] = %q", cfg.HttpTools[0].Params.Headers["Authorization"])
	}

	if len(cfg.SseTools) != 1 {
		t.Fatalf("SseTools len = %d, want 1", len(cfg.SseTools))
	}
	if cfg.SseTools[0].Params.Url != "http://sse.example.com" {
		t.Errorf("SseTools[0].Params.Url = %q", cfg.SseTools[0].Params.Url)
	}

	if len(cfg.RemoteAgents) != 1 {
		t.Fatalf("RemoteAgents len = %d, want 1", len(cfg.RemoteAgents))
	}
	if cfg.RemoteAgents[0].Name != "helper" {
		t.Errorf("RemoteAgents[0].Name = %q, want %q", cfg.RemoteAgents[0].Name, "helper")
	}
}

func TestAgentConfig_UnmarshalJSON_InvalidModel(t *testing.T) {
	data := []byte(`{
		"model": {"type":"unknown_type","model":"test"},
		"description": "d",
		"instruction": "i",
		"stream": false
	}`)
	var cfg AgentConfig
	err := json.Unmarshal(data, &cfg)
	if err == nil {
		t.Fatal("expected error for unknown model type, got nil")
	}
}

func TestAgentConfig_UnmarshalJSON_InvalidJSON(t *testing.T) {
	data := []byte(`{not valid json}`)
	var cfg AgentConfig
	err := json.Unmarshal(data, &cfg)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestAgentCompressionConfig_UnmarshalJSON_NoSummarizer(t *testing.T) {
	data := []byte(`{
		"compaction_interval": 5,
		"overlap_size": 2,
		"token_threshold": 1000
	}`)
	var comp AgentCompressionConfig
	if err := json.Unmarshal(data, &comp); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}
	if *comp.CompactionInterval != 5 {
		t.Errorf("CompactionInterval = %d, want 5", *comp.CompactionInterval)
	}
	if *comp.OverlapSize != 2 {
		t.Errorf("OverlapSize = %d, want 2", *comp.OverlapSize)
	}
	if comp.SummarizerModel != nil {
		t.Error("SummarizerModel should be nil when not provided")
	}
	if *comp.TokenThreshold != 1000 {
		t.Errorf("TokenThreshold = %d, want 1000", *comp.TokenThreshold)
	}
}

func TestAgentCompressionConfig_UnmarshalJSON_WithSummarizer(t *testing.T) {
	data := []byte(`{
		"compaction_interval": 10,
		"overlap_size": 3,
		"summarizer_model": {"type":"gemini","model":"gemini-1.5-flash"},
		"prompt_template": "Please summarize"
	}`)
	var comp AgentCompressionConfig
	if err := json.Unmarshal(data, &comp); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}
	if comp.SummarizerModel == nil {
		t.Fatal("SummarizerModel should not be nil")
	}
	if comp.SummarizerModel.GetType() != ModelTypeGemini {
		t.Errorf("SummarizerModel.GetType() = %q, want %q", comp.SummarizerModel.GetType(), ModelTypeGemini)
	}
	if comp.PromptTemplate != "Please summarize" {
		t.Errorf("PromptTemplate = %q, want %q", comp.PromptTemplate, "Please summarize")
	}
}

func TestAgentCompressionConfig_UnmarshalJSON_NullSummarizer(t *testing.T) {
	data := []byte(`{
		"compaction_interval": 5,
		"overlap_size": 2,
		"summarizer_model": null
	}`)
	var comp AgentCompressionConfig
	if err := json.Unmarshal(data, &comp); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}
	if comp.SummarizerModel != nil {
		t.Error("SummarizerModel should be nil for null value")
	}
}

func TestAgentCompressionConfig_UnmarshalJSON_InvalidSummarizer(t *testing.T) {
	data := []byte(`{
		"compaction_interval": 5,
		"overlap_size": 2,
		"summarizer_model": {"type":"bad_type","model":"test"}
	}`)
	var comp AgentCompressionConfig
	err := json.Unmarshal(data, &comp)
	if err == nil {
		t.Fatal("expected error for invalid summarizer model type, got nil")
	}
}

func TestEmbeddingConfig_UnmarshalJSON_ProviderField(t *testing.T) {
	data := []byte(`{"provider":"openai","model":"text-embedding-3-small","base_url":"https://api.openai.com"}`)
	var cfg EmbeddingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}
	if cfg.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", cfg.Provider, "openai")
	}
	if cfg.Model != "text-embedding-3-small" {
		t.Errorf("Model = %q, want %q", cfg.Model, "text-embedding-3-small")
	}
	if cfg.BaseUrl != "https://api.openai.com" {
		t.Errorf("BaseUrl = %q, want %q", cfg.BaseUrl, "https://api.openai.com")
	}
}

func TestEmbeddingConfig_UnmarshalJSON_TypeFallback(t *testing.T) {
	// Test backward compat: "type" is accepted when "provider" is absent
	data := []byte(`{"type":"anthropic","model":"embed-model"}`)
	var cfg EmbeddingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}
	if cfg.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q (should fall back from type)", cfg.Provider, "anthropic")
	}
}

func TestEmbeddingConfig_UnmarshalJSON_ProviderOverridesType(t *testing.T) {
	// When both "provider" and "type" are present, provider wins
	data := []byte(`{"type":"old_type","provider":"new_provider","model":"m"}`)
	var cfg EmbeddingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}
	if cfg.Provider != "new_provider" {
		t.Errorf("Provider = %q, want %q (provider should override type)", cfg.Provider, "new_provider")
	}
}

func TestAgentConfig_ScanAndValue(t *testing.T) {
	original := AgentConfig{
		Model:       &OpenAI{BaseModel: BaseModel{Model: "gpt-4o"}},
		Description: "test",
		Instruction: "be helpful",
		Stream:      true,
	}

	// Test Value (driver.Valuer)
	val, err := original.Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	data, ok := val.([]byte)
	if !ok {
		t.Fatalf("Value() returned %T, want []byte", val)
	}

	// Test Scan (sql.Scanner)
	var scanned AgentConfig
	if err := scanned.Scan(data); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if scanned.Model.GetType() != ModelTypeOpenAI {
		t.Errorf("after Scan: Model.GetType() = %q, want %q", scanned.Model.GetType(), ModelTypeOpenAI)
	}
	if scanned.Description != "test" {
		t.Errorf("after Scan: Description = %q, want %q", scanned.Description, "test")
	}
}
