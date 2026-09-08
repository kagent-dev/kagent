package models

import (
	"testing"
)

func TestResolveOpenAIAPIKey(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		config  *OpenAIConfig
		want    string
		wantErr bool
	}{
		{
			name:    "no key and no base URL fails",
			config:  &OpenAIConfig{Model: "gpt-4o"},
			wantErr: true,
		},
		{
			name:   "no key with custom base URL uses placeholder",
			config: &OpenAIConfig{Model: "llama", BaseUrl: "http://vllm.default.svc/v1"},
			want:   keylessOpenAIPlaceholder,
		},
		{
			name:   "env key wins over placeholder",
			env:    "sk-test",
			config: &OpenAIConfig{Model: "llama", BaseUrl: "http://vllm.default.svc/v1"},
			want:   "sk-test",
		},
		{
			name:   "env key without base URL",
			env:    "sk-test",
			config: &OpenAIConfig{Model: "gpt-4o"},
			want:   "sk-test",
		},
		{
			name: "passthrough ignores env and base URL",
			config: &OpenAIConfig{
				Model:           "gpt-4o",
				TransportConfig: TransportConfig{APIKeyPassthrough: true},
			},
			want: "passthrough",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OPENAI_API_KEY", tt.env)
			got, err := resolveOpenAIAPIKey(t.Context(), tt.config)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got key %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveOpenAIAPIKey() = %q, want %q", got, tt.want)
			}
		})
	}
}
