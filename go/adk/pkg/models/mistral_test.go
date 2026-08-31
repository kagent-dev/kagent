package models

import (
	"testing"

	"github.com/go-logr/logr"
)

func TestNewMistralModelWithLogger(t *testing.T) {
	tests := []struct {
		name        string
		envAPIKey   string
		envAPIBase  string
		config      *MistralConfig
		wantErr     bool
		wantBaseURL string
	}{
		{
			name:      "missing API key without passthrough returns error",
			envAPIKey: "",
			config:    &MistralConfig{Model: "mistral-large-latest"},
			wantErr:   true,
		},
		{
			name:        "default base URL when nothing set",
			envAPIKey:   "test-key",
			config:      &MistralConfig{Model: "mistral-large-latest"},
			wantBaseURL: DefaultMistralBaseURL,
		},
		{
			name:        "MISTRAL_API_BASE env overrides default",
			envAPIKey:   "test-key",
			envAPIBase:  "https://gateway.example.com/mistral/v1",
			config:      &MistralConfig{Model: "mistral-large-latest"},
			wantBaseURL: "https://gateway.example.com/mistral/v1",
		},
		{
			name:       "config BaseUrl wins over env",
			envAPIKey:  "test-key",
			envAPIBase: "https://from-env.example.com/v1",
			config: &MistralConfig{
				Model:   "mistral-large-latest",
				BaseUrl: "https://from-config.example.com/v1",
			},
			wantBaseURL: "https://from-config.example.com/v1",
		},
		{
			name:      "passthrough skips MISTRAL_API_KEY check",
			envAPIKey: "",
			config: &MistralConfig{
				TransportConfig: TransportConfig{APIKeyPassthrough: true},
				Model:           "mistral-medium-latest",
			},
			wantBaseURL: DefaultMistralBaseURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MISTRAL_API_KEY", tt.envAPIKey)
			t.Setenv("MISTRAL_API_BASE", tt.envAPIBase)

			m, err := NewMistralModelWithLogger(tt.config, logr.Discard())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if m == nil {
				t.Fatal("expected non-nil MistralModel")
			}
			if m.Name() != tt.config.Model {
				t.Errorf("Name() = %q, want %q", m.Name(), tt.config.Model)
			}
			if m.inner == nil {
				t.Fatal("expected inner OpenAIModel to be initialized")
			}
			if m.inner.Config.BaseUrl != tt.wantBaseURL {
				t.Errorf("inner BaseUrl = %q, want %q", m.inner.Config.BaseUrl, tt.wantBaseURL)
			}
			if m.inner.Config.Model != tt.config.Model {
				t.Errorf("inner Model = %q, want %q", m.inner.Config.Model, tt.config.Model)
			}
		})
	}
}

func TestMistralModel_NamePropagatesModelNotProvider(t *testing.T) {
	m := &MistralModel{
		Config: &MistralConfig{Model: "mistral-large-latest"},
	}
	if got := m.Name(); got != "mistral-large-latest" {
		t.Errorf("Name() = %q, want %q", got, "mistral-large-latest")
	}
	if m.Name() == "mistral" {
		t.Error("Name() returns provider name 'mistral' instead of model name — this causes 404 from Mistral API")
	}
}
