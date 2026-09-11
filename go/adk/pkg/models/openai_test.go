package models

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewOpenAIModel_EmptyAPIKeySendsNoAuthorization(t *testing.T) {
	var gotAuth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Values("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "sk-from-env")
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
