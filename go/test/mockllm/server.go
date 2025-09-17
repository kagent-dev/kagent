package mockllm

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"net/http"
	"time"

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
			Stream:   mock.Stream,
		})
	}

	var anthropicMocks []AnthropicMock
	for _, mock := range config.Anthropic {
		anthropicMocks = append(anthropicMocks, AnthropicMock{
			Name:     mock.Name,
			Match:    mock.Match,
			Response: mock.Response,
			Stream:   mock.Stream,
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
		defer resp.Body.Close()
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
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"service":   "mock-llm",
		"openai":    len(s.config.OpenAI),
		"anthropic": len(s.config.Anthropic),
	})
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":  "Endpoint not found",
		"path":   r.URL.Path,
		"method": r.Method,
		"hint":   "Supported: /v1/chat/completions (OpenAI), /v1/messages (Anthropic)",
	})
}

func generateTLSConfig() *tls.Config {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Mock Server"},
			CommonName:   "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("0.0.0.0")},
		DNSNames:              []string{"localhost"},
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		panic(err)
	}

	// Create TLS certificate
	cert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  privateKey,
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
}

// Example usage:
//
// func TestExample(t *testing.T) {
//     config := mockllm.Config{
//         OpenAI: []mockllm.OpenAIMock{
//             {
//                 Name: "simple-response",
//                 Request: map[string]interface{}{
//                     "model": "gpt-4o-mini",
//                     "messages": []map[string]interface{}{
//                         {"role": "user", "content": "Hello"},
//                     },
//                 },
//                 Response: map[string]interface{}{
//                     "id": "chatcmpl-123",
//                     "object": "chat.completion",
//                     "created": 1677652288,
//                     "model": "gpt-4o-mini",
//                     "choices": []map[string]interface{}{
//                         {
//                             "index": 0,
//                             "message": map[string]interface{}{
//                                 "role": "assistant",
//                                 "content": "Hello! How can I help you today?",
//                             },
//                             "finish_reason": "stop",
//                         },
//                     },
//                 },
//             },
//         },
//     }
//
//     server := mockllm.NewServer(config)
//     baseURL, err := server.Start()
//     require.NoError(t, err)
//     defer server.Stop()
//
//     // Use baseURL with OpenAI client...
// }
