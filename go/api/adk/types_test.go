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
