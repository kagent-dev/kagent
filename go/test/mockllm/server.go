package mockllm

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"

	"github.com/gorilla/mux"
	"k8s.io/client-go/util/retry"
)

// Server is the main mock LLM server
type Server struct {
	config            Config
	openaiProvider    *OpenAIProvider
	anthropicProvider *AnthropicProvider
	router            *mux.Router
	listener          net.Listener
}

// NewServer creates a new mock LLM server with the given config
func NewServer(config Config) *Server {
	// Convert config to provider mocks
	var openaiMocks []OpenAIMock
	for _, mock := range config.OpenAI {
		openaiMocks = append(openaiMocks, OpenAIMock{
			Name:     mock.Name,
			Match:    mock.Match,
			Response: mock.Response,
		})
	}

	var anthropicMocks []AnthropicMock
	for _, mock := range config.Anthropic {
		anthropicMocks = append(anthropicMocks, AnthropicMock{
			Name:     mock.Name,
			Match:    mock.Match,
			Response: mock.Response,
		})
	}

	return &Server{
		config:            config,
		openaiProvider:    NewOpenAIProvider(openaiMocks),
		anthropicProvider: NewAnthropicProvider(anthropicMocks),
	}
}

// LoadConfigFromFile loads configuration from a JSON file
func LoadConfigFromFile(path string, filesys fs.ReadFileFS) (Config, error) {
	data, err := filesys.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("failed to parse config JSON: %w", err)
	}

	return config, nil
}

// Start starts the server on a random available port and returns the base URL
func (s *Server) Start() (string, error) {
	s.setupRoutes()

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return "", fmt.Errorf("failed to create listener: %w", err)
	}
	s.listener = listener

	go func() {
		if err := http.Serve(listener, s.router); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Server error: %v\n", err)
		}
	}()

	if err := retry.OnError(retry.DefaultBackoff, func(err error) bool {
		return err != nil
	}, func() error {
		resp, err := http.Get(fmt.Sprintf("http://%s/health", listener.Addr().String()))
		if err != nil {
			return err
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("health check failed: %d", resp.StatusCode)
		}
		return nil
	}); err != nil {
		return "", fmt.Errorf("failed to health check server: %w", err)
	}

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	return baseURL, nil
}

// Stop stops the server
func (s *Server) Stop() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

func (s *Server) setupRoutes() {
	r := mux.NewRouter()

	// Health check
	r.HandleFunc("/health", s.handleHealth).Methods("GET")

	// OpenAI Chat Completions API
	r.HandleFunc("/v1/chat/completions", s.openaiProvider.Handle).Methods("POST")

	// Anthropic Messages API
	r.HandleFunc("/v1/messages", s.anthropicProvider.Handle).Methods("POST")

	// Debug route
	r.NotFoundHandler = http.HandlerFunc(s.handleNotFound)

	s.router = r
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"status":    "healthy",
		"service":   "mock-llm",
		"openai":    len(s.config.OpenAI),
		"anthropic": len(s.config.Anthropic),
	})
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"error":  "Endpoint not found",
		"path":   r.URL.Path,
		"method": r.Method,
		"hint":   "Supported: /v1/chat/completions (OpenAI), /v1/messages (Anthropic)",
	})
}
