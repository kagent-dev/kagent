package models

import (
	"maps"
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

func TestOllamaCloudModel(t *testing.T) {
	cloud := []string{"minimax-m3:cloud", "deepseek-v4-flash:0731-cloud", "gpt-oss:120b-cloud"}
	// "cloud" has to be the tag, not merely present in the name.
	local := []string{"llama3.2", "cloudy-llm:7b", "nimbus-cloud-13b:q4", "llama3.3:70b"}

	// The catalog lists cloud models by their bare names, which is what
	// api.ollama.com accepts. Recognising only the ":cloud" tag made every
	// catalog entry look local, so a user picking one got a call to the pod's
	// own localhost:11434 instead of the model they chose.
	cloud = append(cloud, OllamaCloudModels...)

	for _, m := range cloud {
		if !isOllamaCloudModel(m) {
			t.Errorf("isOllamaCloudModel(%q) = false, want true", m)
		}
	}
	for _, m := range local {
		if isOllamaCloudModel(m) {
			t.Errorf("isOllamaCloudModel(%q) = true, want false", m)
		}
	}
}

// TestOllamaReachesCloud pins the one routing rule the runtime, the key mount,
// and credential compilation all share. They previously carried three separate
// copies that disagreed, so a valid ModelConfig could compile with an env var
// that had no binding to match, or with an egress list that omitted the host it
// was about to call.
func TestOllamaReachesCloud(t *testing.T) {
	tests := []struct {
		name          string
		model         string
		host          string
		hasCredential bool
		want          bool
	}{
		// The chart ships this host, so it is the common local case.
		{name: "explicit host wins even for a cloud model with a key", model: "minimax-m3:cloud", host: "host.docker.internal:11434", hasCredential: true, want: false},
		{name: "explicit host wins for a bare catalog name", model: "kimi-k3", host: "http://gpu-box.lan:11434", hasCredential: true, want: false},
		// Writing the cloud endpoint out longhand is still the cloud route: the
		// runtime takes its bearer token from IsOllamaCloudEndpoint, so treating
		// this host as local shipped a request that went out unauthenticated.
		{name: "a host that is the cloud endpoint reaches the cloud", model: "minimax-m3:cloud", host: "api.ollama.com", hasCredential: true, want: true},
		{name: "cloud endpoint with a scheme reaches the cloud", model: "kimi-k3", host: "https://api.ollama.com", hasCredential: true, want: true},
		{name: "cloud endpoint still needs a credential", model: "kimi-k3", host: "api.ollama.com", hasCredential: false, want: false},
		{name: "cloud endpoint does not promote a local model", model: "llama3.2", host: "api.ollama.com", hasCredential: true, want: false},
		{name: "bare catalog name without a host reaches the cloud", model: "kimi-k3", hasCredential: true, want: true},
		{name: "tagged cloud model without a host reaches the cloud", model: "minimax-m3:cloud", hasCredential: true, want: true},
		{name: "cloud model without a credential stays local", model: "kimi-k3", hasCredential: false, want: false},
		{name: "local model with a credential stays local", model: "llama3.2", hasCredential: true, want: false},
		// A name that merely contains "cloud" is not a cloud model.
		{name: "name containing cloud is not a cloud model", model: "cloudy-llm:7b", hasCredential: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OllamaReachesCloud(tt.model, tt.host, tt.hasCredential); got != tt.want {
				t.Errorf("OllamaReachesCloud(%q, %q, %v) = %v, want %v", tt.model, tt.host, tt.hasCredential, got, tt.want)
			}
		})
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

// The gateway placeholder travels to the cloud on the existing TransportConfig
// header path, and only there. A local route must acquire no Authorization
// header from the key, and a stale default header must not survive on one that
// does carry the placeholder.
func TestWithOllamaAuthorization(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		apiKey  string
		headers map[string]string
		want    string
	}{
		{name: "cloud endpoint takes the placeholder", host: ollamaCloudURL, apiKey: "placeholder", want: "Bearer placeholder"},
		{name: "bare cloud host takes the placeholder", host: "api.ollama.com", apiKey: "placeholder", want: "Bearer placeholder"},
		{name: "cloud endpoint without a key sends nothing", host: ollamaCloudURL},
		{name: "local daemon never takes the key", host: ollamaLocalURL, apiKey: "placeholder"},
		{name: "operator host never takes the key", host: "http://gpu-box.lan:11434", apiKey: "placeholder"},
		{
			// The placeholder is the credential; a stale default header would
			// silently replace it.
			name: "placeholder replaces a stale authorization", host: ollamaCloudURL, apiKey: "placeholder",
			headers: map[string]string{"Authorization": "stale"}, want: "Bearer placeholder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := map[string]string(nil)
			if tt.headers != nil {
				original = maps.Clone(tt.headers)
			}
			got := withOllamaAuthorization(TransportConfig{Headers: tt.headers}, tt.host, tt.apiKey)
			if value := got.Headers["Authorization"]; value != tt.want {
				t.Errorf("Authorization = %q, want %q", value, tt.want)
			}
			if tt.headers != nil && !maps.Equal(tt.headers, original) {
				t.Errorf("caller headers mutated: %v, want %v", tt.headers, original)
			}
		})
	}
}

// The config returned by withOllamaAuthorization has to survive the real client
// stack, or the placeholder would be set and never sent.
func TestWithOllamaAuthorizationReachesTheWire(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := BuildHTTPClient(withOllamaAuthorization(TransportConfig{}, ollamaCloudURL, "placeholder"))
	if err != nil {
		t.Fatalf("BuildHTTPClient: %v", err)
	}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	response.Body.Close()

	if got != "Bearer placeholder" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer placeholder")
	}
}
