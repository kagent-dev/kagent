/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
)

func TestBuildModelsURL(t *testing.T) {
	tests := []struct {
		name         string
		endpoint     string
		providerType v1alpha2.ModelProvider
		want         string
	}{
		// OpenAI
		{
			name:         "OpenAI - base URL",
			endpoint:     "https://api.openai.com",
			providerType: v1alpha2.ModelProviderOpenAI,
			want:         "https://api.openai.com/v1/models",
		},
		{
			name:         "OpenAI - with v1",
			endpoint:     "https://api.openai.com/v1",
			providerType: v1alpha2.ModelProviderOpenAI,
			want:         "https://api.openai.com/v1/models",
		},
		{
			name:         "OpenAI - trailing slash",
			endpoint:     "https://api.openai.com/v1/",
			providerType: v1alpha2.ModelProviderOpenAI,
			want:         "https://api.openai.com/v1/models",
		},

		// Anthropic
		{
			name:         "Anthropic - base URL",
			endpoint:     "https://api.anthropic.com",
			providerType: v1alpha2.ModelProviderAnthropic,
			want:         "https://api.anthropic.com/v1/models",
		},
		{
			name:         "Anthropic - with v1",
			endpoint:     "https://api.anthropic.com/v1",
			providerType: v1alpha2.ModelProviderAnthropic,
			want:         "https://api.anthropic.com/v1/models",
		},

		// Azure OpenAI
		{
			name:         "Azure OpenAI",
			endpoint:     "https://my-resource.openai.azure.com",
			providerType: v1alpha2.ModelProviderAzureOpenAI,
			want:         "https://my-resource.openai.azure.com/v1/models",
		},

		// Note: Ollama is handled via DiscoverOllamaModels delegation,
		// so buildModelsURL is not called for Ollama providers.
		// See TestDiscoverModels_OllamaDelegation for the integration test.

		// Gemini
		{
			name:         "Gemini - googleapis",
			endpoint:     "https://generativelanguage.googleapis.com",
			providerType: v1alpha2.ModelProviderGemini,
			want:         "https://generativelanguage.googleapis.com/v1beta/models",
		},
		{
			name:         "Gemini - custom endpoint",
			endpoint:     "https://custom-gemini.example.com",
			providerType: v1alpha2.ModelProviderGemini,
			want:         "https://custom-gemini.example.com/v1/models",
		},

		// LiteLLM / OpenAI-compatible
		{
			name:         "LiteLLM gateway",
			endpoint:     "https://litellm.company.com/v1",
			providerType: v1alpha2.ModelProviderOpenAI,
			want:         "https://litellm.company.com/v1/models",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildModelsURL(tt.endpoint, tt.providerType)
			if got != tt.want {
				t.Errorf("buildModelsURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDiscoverModels_OpenAI(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-api-key" {
			t.Errorf("unexpected Authorization header: %s", auth)
		}

		// Return mock response
		response := openAIModelsResponse{
			Data: []struct {
				ID string `json:"id"`
			}{
				{ID: "gpt-4"},
				{ID: "gpt-3.5-turbo"},
				{ID: "text-embedding-ada-002"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	d := NewModelDiscoverer()
	models, err := d.DiscoverModels(context.Background(), v1alpha2.ModelProviderOpenAI, server.URL, "test-api-key")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(models) != 3 {
		t.Errorf("expected 3 models, got %d", len(models))
	}

	expectedModels := map[string]bool{
		"gpt-4":                  true,
		"gpt-3.5-turbo":          true,
		"text-embedding-ada-002": true,
	}

	for _, model := range models {
		if !expectedModels[model] {
			t.Errorf("unexpected model: %s", model)
		}
	}
}

func TestDiscoverModels_Anthropic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Anthropic-specific headers
		apiKey := r.Header.Get("x-api-key")
		if apiKey != "test-anthropic-key" {
			t.Errorf("unexpected x-api-key header: %s", apiKey)
		}

		version := r.Header.Get("anthropic-version")
		if version != "2023-06-01" {
			t.Errorf("unexpected anthropic-version header: %s", version)
		}

		// Return mock response (same format as OpenAI)
		response := openAIModelsResponse{
			Data: []struct {
				ID string `json:"id"`
			}{
				{ID: "claude-3-opus-20240229"},
				{ID: "claude-3-sonnet-20240229"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	d := NewModelDiscoverer()
	models, err := d.DiscoverModels(context.Background(), v1alpha2.ModelProviderAnthropic, server.URL, "test-anthropic-key")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
}

func TestDiscoverModels_ErrorResponses(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		responseBody   string
		wantErrContain string
	}{
		{
			name:           "unauthorized",
			statusCode:     http.StatusUnauthorized,
			responseBody:   `{"error": "Invalid API key"}`,
			wantErrContain: "unauthorized",
		},
		{
			name:           "forbidden",
			statusCode:     http.StatusForbidden,
			responseBody:   `{"error": "Access denied"}`,
			wantErrContain: "forbidden",
		},
		{
			name:           "not found",
			statusCode:     http.StatusNotFound,
			responseBody:   `{"error": "Not found"}`,
			wantErrContain: "not found",
		},
		{
			name:           "server error",
			statusCode:     http.StatusInternalServerError,
			responseBody:   `{"error": "Internal server error"}`,
			wantErrContain: "status 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			d := NewModelDiscoverer()
			_, err := d.DiscoverModels(context.Background(), v1alpha2.ModelProviderOpenAI, server.URL, "test-key")

			if err == nil {
				t.Error("expected error but got nil")
				return
			}

			if !strings.Contains(strings.ToLower(err.Error()), tt.wantErrContain) {
				t.Errorf("error = %v, want error containing %v", err, tt.wantErrContain)
			}
		})
	}
}

func TestDiscoverModels_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	d := NewModelDiscoverer()
	_, err := d.DiscoverModels(context.Background(), v1alpha2.ModelProviderOpenAI, server.URL, "test-key")

	if err == nil {
		t.Error("expected error for invalid JSON")
	}

	if !strings.Contains(err.Error(), "failed to parse") {
		t.Errorf("error = %v, want error containing 'failed to parse'", err)
	}
}

func TestDiscoverModels_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := openAIModelsResponse{
			Data: []struct {
				ID string `json:"id"`
			}{},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	d := NewModelDiscoverer()
	models, err := d.DiscoverModels(context.Background(), v1alpha2.ModelProviderOpenAI, server.URL, "test-key")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(models) != 0 {
		t.Errorf("expected 0 models, got %d", len(models))
	}
}

func TestDiscoverModels_FilterEmptyIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := openAIModelsResponse{
			Data: []struct {
				ID string `json:"id"`
			}{
				{ID: "gpt-4"},
				{ID: ""},      // Empty ID should be filtered
				{ID: "gpt-3"}, // Valid
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	d := NewModelDiscoverer()
	models, err := d.DiscoverModels(context.Background(), v1alpha2.ModelProviderOpenAI, server.URL, "test-key")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(models) != 2 {
		t.Errorf("expected 2 models (empty ID filtered), got %d", len(models))
	}
}

func TestDiscoverOllamaModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		response := ollamaTagsResponse{
			Models: []struct {
				Name string `json:"name"`
			}{
				{Name: "llama2"},
				{Name: "mistral"},
				{Name: "codellama"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	d := NewModelDiscoverer()
	// Test through the public DiscoverModels API for Ollama provider
	models, err := d.DiscoverModels(context.Background(), v1alpha2.ModelProviderOllama, server.URL, "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(models) != 3 {
		t.Errorf("expected 3 models, got %d", len(models))
	}

	expectedModels := map[string]bool{
		"llama2":    true,
		"mistral":   true,
		"codellama": true,
	}

	for _, model := range models {
		if !expectedModels[model] {
			t.Errorf("unexpected model: %s", model)
		}
	}
}

// TestDiscoverModels_OllamaDelegation verifies that DiscoverModels correctly
// delegates to DiscoverOllamaModels when the provider type is Ollama.
func TestDiscoverModels_OllamaDelegation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ollama should call /api/tags, not /v1/models
		if r.URL.Path != "/api/tags" {
			t.Errorf("DiscoverModels with Ollama should call /api/tags, got: %s", r.URL.Path)
		}

		response := ollamaTagsResponse{
			Models: []struct {
				Name string `json:"name"`
			}{
				{Name: "llama2"},
				{Name: "mistral"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	d := NewModelDiscoverer()
	// Use DiscoverModels (not DiscoverOllamaModels directly) to test delegation
	models, err := d.DiscoverModels(context.Background(), v1alpha2.ModelProviderOllama, server.URL, "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}

	expectedModels := map[string]bool{
		"llama2":  true,
		"mistral": true,
	}

	for _, model := range models {
		if !expectedModels[model] {
			t.Errorf("unexpected model: %s", model)
		}
	}
}

func TestNewModelDiscoverer(t *testing.T) {
	d := NewModelDiscoverer()

	if d == nil {
		t.Fatal("NewModelDiscoverer returned nil")
	}

	if d.httpClient == nil {
		t.Fatal("httpClient should be initialized")
	}

	if d.httpClient.Timeout != DefaultTimeout {
		t.Errorf("httpClient timeout = %v, want %v", d.httpClient.Timeout, DefaultTimeout)
	}
}

func TestSetAuthHeaders(t *testing.T) {
	d := NewModelDiscoverer()

	tests := []struct {
		name         string
		providerType v1alpha2.ModelProvider
		apiKey       string
		wantAuthz    string
		wantAPIKey   string
		wantAnthVer  string
	}{
		{
			name:         "OpenAI",
			providerType: v1alpha2.ModelProviderOpenAI,
			apiKey:       "sk-test",
			wantAuthz:    "Bearer sk-test",
		},
		{
			name:         "Azure OpenAI",
			providerType: v1alpha2.ModelProviderAzureOpenAI,
			apiKey:       "azure-key",
			wantAuthz:    "Bearer azure-key",
		},
		{
			name:         "Anthropic",
			providerType: v1alpha2.ModelProviderAnthropic,
			apiKey:       "anth-key",
			wantAPIKey:   "anth-key",
			wantAnthVer:  "2023-06-01",
		},
		{
			name:         "Gemini",
			providerType: v1alpha2.ModelProviderGemini,
			apiKey:       "gemini-key",
			wantAuthz:    "Bearer gemini-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
			d.setAuthHeaders(req, tt.providerType, tt.apiKey)

			if tt.wantAuthz != "" {
				got := req.Header.Get("Authorization")
				if got != tt.wantAuthz {
					t.Errorf("Authorization = %v, want %v", got, tt.wantAuthz)
				}
			}

			if tt.wantAPIKey != "" {
				got := req.Header.Get("x-api-key")
				if got != tt.wantAPIKey {
					t.Errorf("x-api-key = %v, want %v", got, tt.wantAPIKey)
				}
			}

			if tt.wantAnthVer != "" {
				got := req.Header.Get("anthropic-version")
				if got != tt.wantAnthVer {
					t.Errorf("anthropic-version = %v, want %v", got, tt.wantAnthVer)
				}
			}
		})
	}
}
