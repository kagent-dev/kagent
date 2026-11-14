package embedding

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
)

func TestOpenAIEmbeddingRequestsCarryHostSignedIdentity(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "placeholder")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get(egresspolicy.ActorIdentityHeader); got != "required" {
			t.Errorf("actor identity header = %q, want required", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(embeddingResponse("embedding")); err != nil {
			t.Errorf("write embedding response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	client, err := New(Config{EmbeddingConfig: &configv1.ADKEmbeddingConfig{
		Provider: "openai", Model: "embedding", BaseUrl: server.URL,
	}})
	if err != nil {
		t.Fatal(err)
	}
	vectors, err := client.Generate(t.Context(), []string{"verified knowledge"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 1 || len(vectors[0]) != TargetDimension {
		t.Fatalf("embedding shape = %d vectors, want one %d-dimensional vector", len(vectors), TargetDimension)
	}
}
