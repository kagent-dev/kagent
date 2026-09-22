package models

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The key is read in exactly one direction. Its absence keeps a cloud-tagged
// model on the local daemon, which proxies cloud models on the signed-in
// identity; its presence must never promote an untagged model to the cloud,
// where a privately pulled name does not exist.
func TestResolveOllamaEndpoint(t *testing.T) {
	const key = "sk-ollama-test"

	tests := []struct {
		name   string
		model  string
		apiKey string
		host   string
		want   string
	}{
		{name: "local model", model: "llama3.2", want: ollamaLocalURL},
		{
			// The regression this function exists to prevent: a key exported for
			// some cloud model must not reroute local models to the cloud.
			name: "local model with a key exported stays local", model: "qwen3.8:27b-mlx",
			apiKey: key, want: ollamaLocalURL,
		},
		{name: "cloud tag with a key goes to the cloud", model: "minimax-m3:cloud", apiKey: key, want: ollamaCloudURL},
		{
			// The form most of the cloud catalog uses.
			name: "sized cloud tag goes to the cloud", model: "deepseek-v4-flash:0731-cloud",
			apiKey: key, want: ollamaCloudURL,
		},
		{
			// Unauthenticated api.ollama.com answers 401 before it looks at the
			// model, so the daemon is the only endpoint that can serve this.
			name: "cloud tag without a key falls back to the daemon", model: "minimax-m3:cloud",
			want: ollamaLocalURL,
		},
		{name: "explicit host wins over the cloud tag", model: "minimax-m3:cloud", apiKey: key, host: "http://gpu-box.lan:11434", want: "http://gpu-box.lan:11434"},
		{name: "explicit host wins over a local model", model: "llama3.2", apiKey: key, host: "https://ollama.internal.example.com", want: "https://ollama.internal.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveOllamaEndpoint(&OllamaConfig{Model: tt.model, APIKey: tt.apiKey}, tt.host)
			if got != tt.want {
				t.Errorf("resolveOllamaEndpoint(model=%q, key=%q, host=%q) = %q, want %q", tt.model, tt.apiKey, tt.host, got, tt.want)
			}
		})
	}
}

func TestIsOllamaCloudModel(t *testing.T) {
	cloud := []string{"minimax-m3:cloud", "deepseek-v4-flash:0731-cloud", "gpt-oss:120b-cloud"}
	// "cloud" has to be the tag, not merely present in the name.
	local := []string{"llama3.2", "cloudy-llm:7b", "nimbus-cloud-13b:q4", "llama3.3:70b"}

	for _, m := range cloud {
		if !IsOllamaCloudModel(m) {
			t.Errorf("IsOllamaCloudModel(%q) = false, want true", m)
		}
	}
	for _, m := range local {
		if IsOllamaCloudModel(m) {
			t.Errorf("IsOllamaCloudModel(%q) = true, want false", m)
		}
	}
}

func TestIsOllamaCloudEndpoint(t *testing.T) {
	cloud := []string{
		ollamaCloudURL,
		"https://api.ollama.com/",
		"https://ollama.com",
		"api.ollama.com", // no scheme, as a host is often written
		"HTTPS://API.OLLAMA.COM",
	}
	local := []string{
		ollamaLocalURL,
		// Unparseable, so a malformed host is treated as a host someone runs.
		"http://[::1",
		"http://127.0.0.1:11434",
		"http://gpu-box.lan:11434",
		"https://ollama.internal.example.com",
		// A proxy that merely mentions the cloud host is not the cloud host.
		"https://ollama-proxy.example.com/api.ollama.com",
		"",
	}

	for _, u := range cloud {
		if !IsOllamaCloudEndpoint(u) {
			t.Errorf("IsOllamaCloudEndpoint(%q) = false, want true", u)
		}
	}
	for _, u := range local {
		if IsOllamaCloudEndpoint(u) {
			t.Errorf("IsOllamaCloudEndpoint(%q) = true, want false", u)
		}
	}
}

// withBearerToken is what carries OLLAMA_API_KEY to the cloud; an empty key must
// leave the client untouched so a local daemon still receives no Authorization.
func TestWithBearerToken(t *testing.T) {
	send := func(client *http.Client) string {
		t.Helper()
		var got string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		resp, err := client.Get(srv.URL)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		resp.Body.Close()
		return got
	}

	t.Run("injects the bearer token", func(t *testing.T) {
		client, err := BuildHTTPClientWithBearer(TransportConfig{}, "ollama_secret")
		if err != nil {
			t.Fatalf("BuildHTTPClientWithBearer: %v", err)
		}
		if got := send(client); got != "Bearer ollama_secret" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer ollama_secret")
		}
	})

	t.Run("an empty token adds no header", func(t *testing.T) {
		client, err := BuildHTTPClientWithBearer(TransportConfig{}, "")
		if err != nil {
			t.Fatalf("BuildHTTPClientWithBearer: %v", err)
		}
		if got := send(client); got != "" {
			t.Errorf("Authorization = %q, want no header", got)
		}
	})

	t.Run("the key wins over a same-named default header", func(t *testing.T) {
		// The explicit API key is the operator's intent; a stale Authorization in
		// defaultHeaders must not silently replace it.
		client, err := BuildHTTPClientWithBearer(
			TransportConfig{Headers: map[string]string{"Authorization": "stale"}}, "fresh")
		if err != nil {
			t.Fatalf("BuildHTTPClientWithBearer: %v", err)
		}
		if got := send(client); got != "Bearer fresh" {
			t.Errorf("Authorization = %q, want the API key to win", got)
		}
	})

	t.Run("the key survives a non-Authorization default header", func(t *testing.T) {
		client, err := BuildHTTPClientWithBearer(
			TransportConfig{Headers: map[string]string{"X-Tenant": "acme"}}, "fresh")
		if err != nil {
			t.Fatalf("BuildHTTPClientWithBearer: %v", err)
		}
		if got := send(client); got != "Bearer fresh" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer fresh")
		}
	})
}
