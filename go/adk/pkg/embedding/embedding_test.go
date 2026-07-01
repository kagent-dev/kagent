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
