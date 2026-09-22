package models

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/ollama/ollama/api"
)

// OllamaConfig holds Ollama configuration
type OllamaConfig struct {
	TransportConfig
	Model   string
	Host    string            // Ollama server host (e.g., http://localhost:11434)
	APIKey  string            // Ollama Cloud API key; empty for a local daemon
	Options map[string]string // Ollama-specific options (temperature, top_p, num_ctx, etc.)
}

// Ollama's two endpoints, mirroring the routing rules in pi-go. Which one a
// model reaches is decided by its tag plus whether a key is set — never by the
// key alone.
const (
	ollamaLocalURL = "http://localhost:11434"
	// api.ollama.com, not ollama.com. The Ollama Go SDK sends a request to
	// ollama.com with an Authorization header it derives from a locally signed
	// nonce, and returns the signing error before the request leaves the
	// process when that key is absent — as it is in an agent pod. api.ollama.com
	// misses that special case, so the Bearer token we set is the one used.
	ollamaCloudURL = "https://api.ollama.com"
)

// IsOllamaCloudModel reports whether an Ollama model name is a cloud model.
// Only the tag counts, so a name that merely contains "cloud" stays local.
func IsOllamaCloudModel(modelName string) bool {
	return strings.HasSuffix(modelName, ":cloud") || strings.HasSuffix(modelName, "-cloud")
}

// IsOllamaCloudEndpoint reports whether an endpoint is ollama.com's hosted API.
// Callers use it to tell a missing credential from an unreachable daemon.
//
// A bare "api.ollama.com" is accepted: url.Parse reads it as a path, not a host,
// so the scheme is added before matching, the same way a host:port is written.
func IsOllamaCloudEndpoint(host string) bool {
	u, err := url.Parse(withDefaultScheme(host))
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "api.ollama.com" || h == "ollama.com"
}

// withDefaultScheme makes a bare host an absolute URL so net/url reads it as one.
// The scheme check is case-insensitive; the host is returned unchanged when it
// already has one.
func withDefaultScheme(host string) string {
	lower := strings.ToLower(host)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return host
	}
	return "https://" + host
}

// resolveOllamaEndpoint picks the server a model should be sent to.
//
// An explicit host always wins: that is how an operator points at another
// machine, a container, or an authenticated proxy. Otherwise a cloud-tagged
// model with a key goes to api.ollama.com, and everything else goes to the
// local daemon — including a cloud-tagged model with no key, because a signed-in
// daemon proxies cloud models and an unauthenticated api.ollama.com answers 401
// before it looks at the model, so the alternative is not a different result but
// a guaranteed failure.
//
// The key is read in exactly one direction. Its absence keeps a cloud-tagged
// model local; its presence never promotes an untagged model. Treating the key
// as a global switch instead sends every local model to the cloud, where a
// privately pulled name does not exist.
func resolveOllamaEndpoint(config *OllamaConfig, host string) string {
	if host != "" {
		return host
	}
	if IsOllamaCloudModel(config.Model) && config.APIKey != "" {
		return ollamaCloudURL
	}
	return ollamaLocalURL
}

// OllamaModel implements model.LLM for Ollama models using the native Ollama SDK.
type OllamaModel struct {
	Config *OllamaConfig
	Client *api.Client
	Logger *slog.Logger
}

// Name returns the model name.
func (m *OllamaModel) Name() string {
	return m.Config.Model
}

// convertOllamaOptions converts string option values to their proper types
// based on known Ollama option types.
func convertOllamaOptions(opts map[string]string) map[string]any {
	if opts == nil {
		return nil
	}

	converted := make(map[string]any, len(opts))

	// Known Ollama option types (from ollama API documentation)
	// https://github.com/ollama/ollama/blob/main/api/types.go
	intOptions := map[string]bool{
		"num_ctx":       true,
		"num_predict":   true,
		"top_k":         true,
		"seed":          true,
		"num_keep":      true,
		"num_gpu":       true,
		"num_thread":    true,
		"repeat_last_n": true,
		"numa":          true,
		"main_gpu":      true,
		"mirostat":      true,
	}

	floatOptions := map[string]bool{
		"temperature":       true,
		"top_p":             true,
		"repeat_penalty":    true,
		"presence_penalty":  true,
		"frequency_penalty": true,
		"tfs_z":             true,
		"typical_p":         true,
		"mirostat_eta":      true,
		"penalty_newline":   true,
		"min_p":             true,
	}

	boolOptions := map[string]bool{
		"penalize_newline": true,
		"low_vram":         true,
		"f16_kv":           true,
		"vocab_only":       true,
		"use_mmap":         true,
		"use_mlock":        true,
		"embedding_only":   true,
		"rope_scaling":     true,
	}

	for key, value := range opts {
		// Try to convert based on known option types
		if intOptions[key] {
			if v, err := strconv.Atoi(value); err == nil {
				converted[key] = v
				continue
			}
		} else if floatOptions[key] {
			if v, err := strconv.ParseFloat(value, 64); err == nil {
				converted[key] = v
				continue
			}
		} else if boolOptions[key] {
			if v, err := strconv.ParseBool(value); err == nil {
				converted[key] = v
				continue
			}
		}

		// If no known type or conversion failed, keep as string
		converted[key] = value
	}

	return converted
}

// NewOllamaModel creates a new Ollama model instance with a logger.
// It uses the native Ollama SDK client for full option support.
func NewOllamaModel(ctx context.Context, config *OllamaConfig) (*OllamaModel, error) {
	logger := logging.FromContext(ctx)
	host := config.Host
	if host == "" {
		host = os.Getenv("OLLAMA_API_BASE")
	}
	host = resolveOllamaEndpoint(config, host)

	// Parse host URL
	baseURL, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("invalid Ollama host URL %q: %w", host, err)
	}

	// Only the cloud carries a key; a local daemon must keep sending none.
	token := ""
	if IsOllamaCloudEndpoint(host) {
		token = config.APIKey
	}
	httpClient, err := BuildHTTPClientWithBearer(config.TransportConfig, token)
	if err != nil {
		return nil, fmt.Errorf("failed to create Ollama HTTP client: %w", err)
	}

	// Create Ollama SDK client (NewClient takes *url.URL then *http.Client)
	client := api.NewClient(baseURL, httpClient)

	logger.InfoContext(ctx, "initialized Ollama model", "model", config.Model, "host", host)

	return &OllamaModel{
		Config: config,
		Client: client,
		Logger: logger,
	}, nil
}
