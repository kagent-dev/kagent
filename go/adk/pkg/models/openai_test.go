package models

import (
	"io"
	"net/http"
	"net/http/httptest"
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
			name:   "no key with custom base URL is unauthenticated",
			config: &OpenAIConfig{Model: "llama", BaseUrl: "http://vllm.default.svc/v1"},
			want:   "",
		},
		{
			name:   "env key wins over an unauthenticated client",
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

func TestNewOpenAIModel_KeylessBaseURLSendsNoAuthorization(t *testing.T) {
	var gotAuth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Values("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "")
	m, err := NewOpenAIModel(t.Context(), &OpenAIConfig{Model: "llama", BaseUrl: srv.URL})
	if err != nil {
		t.Fatalf("NewOpenAIModel() error: %v", err)
	}
	if _, err := m.Client.Models.List(t.Context()); err != nil {
		t.Fatalf("Models.List() error: %v", err)
	}
	if len(gotAuth) != 0 {
		t.Errorf("Authorization header = %q, want none", gotAuth)
	}
}
