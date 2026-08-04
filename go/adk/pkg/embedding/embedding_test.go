package embedding

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/auth"
	"github.com/kagent-dev/kagent/go/api/adk"
)

func TestOpenAIProvider_UsesAPIKeyNotKagentToken(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-openai-key")

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/embeddings" {
			t.Errorf("path = %q, want /embeddings", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var req struct {
			Model      string   `json:"model"`
			Input      []string `json:"input"`
			Dimensions int      `json:"dimensions"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if req.Model != "text-embedding-3-small" {
			t.Errorf("model = %q, want text-embedding-3-small", req.Model)
		}
		if req.Dimensions != TargetDimension {
			t.Errorf("dimensions = %d, want %d", req.Dimensions, TargetDimension)
		}
		if len(req.Input) != 1 || req.Input[0] != "hello" {
			t.Errorf("input = %v, want [hello]", req.Input)
		}

		vec := make([]float64, TargetDimension)
		vec[0] = 1.0
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]any{{"object": "embedding", "index": 0, "embedding": vec}},
			"model":  req.Model,
			"usage":  map[string]any{"prompt_tokens": 1, "total_tokens": 1},
		})
	}))
	defer srv.Close()

	// kagent auth client exists in-process but must not reach embedding providers.
	tokenSvc := auth.NewKAgentTokenService("test-app")
	_ = auth.NewHTTPClientWithToken(tokenSvc)

	client, err := New(Config{
		EmbeddingConfig: &adk.EmbeddingConfig{
			Provider: "openai",
			Model:    "text-embedding-3-small",
			BaseUrl:  srv.URL,
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	vecs, err := client.Generate(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(vecs) != 1 || len(vecs[0]) != TargetDimension {
		t.Fatalf("got %d vectors of len %d, want 1 vector of len %d", len(vecs), len(vecs[0]), TargetDimension)
	}
	if gotAuth != "Bearer sk-openai-key" {
		t.Errorf("Authorization = %q, want Bearer sk-openai-key", gotAuth)
	}
	if strings.Contains(gotAuth, "kagent") {
		t.Errorf("Authorization must not contain kagent token, got %q", gotAuth)
	}
}

func TestNormalizeL2(t *testing.T) {
	normed := normalizeL2([]float32{3, 4})
	var sum float64
	for _, v := range normed {
		sum += float64(v) * float64(v)
	}
	const eps = 1e-5
	if diff := 1.0 - sum; diff > eps || diff < -eps {
		t.Errorf("L2 norm squared = %v, want ~1.0", sum)
	}
}

func TestProcessEmbeddings_RejectsUndersized(t *testing.T) {
	_, err := processEmbeddings(logr.Discard(), [][]float32{{1, 2, 3}}, "test")
	if err == nil {
		t.Fatal("expected error for undersized embedding")
	}
}

func azureEmbeddingResponse(model string) map[string]any {
	vec := make([]float64, TargetDimension)
	vec[0] = 1.0
	return map[string]any{
		"object": "list",
		"data":   []map[string]any{{"object": "embedding", "index": 0, "embedding": vec}},
		"model":  model,
		"usage":  map[string]any{"prompt_tokens": 1, "total_tokens": 1},
	}
}

func TestAzureOpenAIProvider_RequestShape(t *testing.T) {
	const (
		deployment = "text-embedding-3-small"
		apiVersion = "2024-02-15-preview"
		apiKey     = "azure-test-key"
	)

	tests := []struct {
		name        string
		setEnv      bool
		useEndpoint bool   // populate cfg.Endpoint instead of cfg.BaseUrl
		baseURL     string // used when setEnv/useEndpoint is false; may contain "%s" for srv.URL
		deployment  string // populate cfg.Deployment; falls back to cfg.Model when empty
		wantPath    string
		wantAPIVer  string
	}{
		{
			name:       "root endpoint in base_url",
			baseURL:    "%s",
			wantPath:   "/openai/deployments/text-embedding-3-small/embeddings",
			wantAPIVer: apiVersion,
		},
		{
			name:       "root endpoint trailing slash",
			baseURL:    "%s/",
			wantPath:   "/openai/deployments/text-embedding-3-small/embeddings",
			wantAPIVer: apiVersion,
		},
		{
			name:        "endpoint field",
			useEndpoint: true,
			wantPath:    "/openai/deployments/text-embedding-3-small/embeddings",
			wantAPIVer:  apiVersion,
		},
		{
			name:        "explicit deployment field",
			useEndpoint: true,
			deployment:  "custom-deploy",
			wantPath:    "/openai/deployments/custom-deploy/embeddings",
			wantAPIVer:  apiVersion,
		},
		{
			name:       "endpoint from AZURE_OPENAI_ENDPOINT env",
			setEnv:     true,
			wantPath:   "/openai/deployments/text-embedding-3-small/embeddings",
			wantAPIVer: apiVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AZURE_OPENAI_API_KEY", apiKey)
			t.Setenv("OPENAI_API_VERSION", apiVersion)

			var gotPath, gotAPIKey, gotAPIVersion string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotAPIKey = r.Header.Get("Api-Key")
				gotAPIVersion = r.URL.Query().Get("api-version")
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(azureEmbeddingResponse(deployment))
			}))
			defer srv.Close()

			cfg := &adk.EmbeddingConfig{
				Provider:   "azure_openai",
				Model:      deployment,
				Deployment: tt.deployment,
			}
			switch {
			case tt.setEnv:
				t.Setenv("AZURE_OPENAI_ENDPOINT", srv.URL)
			case tt.useEndpoint:
				t.Setenv("AZURE_OPENAI_ENDPOINT", "")
				cfg.Endpoint = srv.URL
			default:
				t.Setenv("AZURE_OPENAI_ENDPOINT", "")
				cfg.BaseUrl = strings.Replace(tt.baseURL, "%s", srv.URL, 1)
			}

			client, err := New(Config{EmbeddingConfig: cfg})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			if _, err := client.Generate(context.Background(), []string{"hello"}); err != nil {
				t.Fatalf("Generate() error = %v", err)
			}
			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}
			if gotAPIVersion != tt.wantAPIVer {
				t.Errorf("api-version = %q, want %q", gotAPIVersion, tt.wantAPIVer)
			}
			if gotAPIKey != apiKey {
				t.Errorf("Api-Key = %q, want %q", gotAPIKey, apiKey)
			}
		})
	}
}

func TestAzureOpenAIProvider_MissingEndpoint(t *testing.T) {
	t.Setenv("AZURE_OPENAI_ENDPOINT", "")
	_, err := New(Config{
		EmbeddingConfig: &adk.EmbeddingConfig{
			Provider: "azure_openai",
			Model:    "text-embedding-3-small",
		},
	})
	if err == nil {
		t.Fatal("expected error when Azure OpenAI endpoint is missing")
	}
	if !strings.Contains(err.Error(), "Azure OpenAI endpoint") {
		t.Errorf("error = %q, want mention of Azure OpenAI endpoint", err)
	}
}

func TestOllamaProvider_RequestShape(t *testing.T) {
	const model = "nomic-embed-text"

	tests := []struct {
		name     string
		useEnv   bool
		baseURL  string // may contain "%s" for srv.URL
		wantPath string
	}{
		{
			name:     "base_url host",
			baseURL:  "%s",
			wantPath: "/api/embed",
		},
		{
			name:     "base_url host trailing slash",
			baseURL:  "%s/",
			wantPath: "/api/embed",
		},
		{
			name:     "host from OLLAMA_API_BASE env",
			useEnv:   true,
			wantPath: "/api/embed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			var gotBody struct {
				Model      string   `json:"model"`
				Input      []string `json:"input"`
				Dimensions int      `json:"dimensions"`
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				if err := json.Unmarshal(body, &gotBody); err != nil {
					t.Fatalf("unmarshal body: %v", err)
				}
				vec := make([]float32, TargetDimension)
				vec[0] = 1.0
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"model":      model,
					"embeddings": [][]float32{vec},
				})
			}))
			defer srv.Close()

			cfg := &adk.EmbeddingConfig{
				Provider: "ollama",
				Model:    model,
			}
			if tt.useEnv {
				t.Setenv("OLLAMA_API_BASE", srv.URL)
			} else {
				t.Setenv("OLLAMA_API_BASE", "")
				cfg.BaseUrl = strings.Replace(tt.baseURL, "%s", srv.URL, 1)
			}

			client, err := New(Config{EmbeddingConfig: cfg})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if _, err := client.Generate(context.Background(), []string{"hello"}); err != nil {
				t.Fatalf("Generate() error = %v", err)
			}
			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}
			if gotBody.Model != model {
				t.Errorf("model = %q, want %q", gotBody.Model, model)
			}
			if gotBody.Dimensions != TargetDimension {
				t.Errorf("dimensions = %d, want %d", gotBody.Dimensions, TargetDimension)
			}
			if len(gotBody.Input) != 1 || gotBody.Input[0] != "hello" {
				t.Errorf("input = %v, want [hello]", gotBody.Input)
			}
		})
	}
}

func TestOllamaProvider_InvalidHost(t *testing.T) {
	t.Setenv("OLLAMA_API_BASE", "")
	_, err := New(Config{
		EmbeddingConfig: &adk.EmbeddingConfig{
			Provider: "ollama",
			Model:    "nomic-embed-text",
			BaseUrl:  "://bad-url",
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid Ollama host URL")
	}
	if !strings.Contains(err.Error(), "invalid Ollama host URL") {
		t.Errorf("error = %q, want invalid Ollama host URL", err)
	}
}
