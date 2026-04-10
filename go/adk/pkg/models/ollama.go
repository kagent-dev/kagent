package models

import (
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/go-logr/logr"
	"github.com/ollama/ollama/api"
)

// OllamaConfig holds Ollama configuration
type OllamaConfig struct {
	TransportConfig
	Model   string
	Host    string            // Ollama server host (e.g., http://localhost:11434)
	Options map[string]string // Ollama-specific options (temperature, top_p, num_ctx, etc.)
}

// OllamaModel implements model.LLM for Ollama models using the native Ollama SDK.
type OllamaModel struct {
	Config *OllamaConfig
	Client *api.Client
	Logger logr.Logger
}

// Name returns the model name.
func (m *OllamaModel) Name() string {
	return m.Config.Model
}

// convertOllamaOptions converts string option values to their proper types
// based on known Ollama option types. This matches Python's _convert_ollama_options.
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

// NewOllamaModelWithLogger creates a new Ollama model instance with a logger.
// It uses the native Ollama SDK client for full option support.
func NewOllamaModelWithLogger(config *OllamaConfig, logger logr.Logger) (*OllamaModel, error) {
	host := config.Host
	if host == "" {
		host = os.Getenv("OLLAMA_API_BASE")
	}
	if host == "" {
		host = "http://localhost:11434"
	}

	// Parse host URL
	baseURL, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("invalid Ollama host URL %q: %w", host, err)
	}

	// Create HTTP client with TLS, passthrough, and header support
	httpClient, err := BuildHTTPClient(config.TransportConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Ollama HTTP client: %w", err)
	}

	// Create Ollama SDK client (NewClient takes *url.URL then *http.Client)
	client := api.NewClient(baseURL, httpClient)

	if logger.GetSink() != nil {
		logger.Info("Initialized Ollama model", "model", config.Model, "host", host)
	}

	return &OllamaModel{
		Config: config,
		Client: client,
		Logger: logger,
	}, nil
}
