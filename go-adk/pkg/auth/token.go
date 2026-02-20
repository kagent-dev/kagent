package auth

import (
	"context"
	"net/http"
	"os"
	"sync"
	"time"
)

const kagentTokenPath = "/var/run/secrets/tokens/kagent-token"

// KAgentTokenService reads a k8s token from a file and reloads it periodically
type KAgentTokenService struct {
	token    string
	mu       sync.RWMutex
	appName  string
	stopChan chan struct{}
	stopOnce sync.Once // guards close(stopChan) to prevent double-close panic
}

// NewKAgentTokenService creates a new KAgentTokenService
func NewKAgentTokenService(appName string) *KAgentTokenService {
	return &KAgentTokenService{
		appName:  appName,
		stopChan: make(chan struct{}),
	}
}

// Start starts the token update loop
func (s *KAgentTokenService) Start(ctx context.Context) error {
	// Read initial token
	token, err := s.readToken()
	if err == nil {
		s.mu.Lock()
		s.token = token
		s.mu.Unlock()
	}

	// Start refresh loop
	go s.refreshTokenLoop(ctx)

	return nil
}

// Stop stops the token refresh loop. Safe to call multiple times.
func (s *KAgentTokenService) Stop() {
	s.stopOnce.Do(func() { close(s.stopChan) })
}

// GetToken returns the current token
func (s *KAgentTokenService) GetToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.token
}

// AddHeaders adds authorization and agent headers to an HTTP request
func (s *KAgentTokenService) AddHeaders(req *http.Request) {
	req.Header.Set("X-Agent-Name", s.appName)
	if token := s.GetToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// readToken reads the token from the file
func (s *KAgentTokenService) readToken() (string, error) {
	data, err := os.ReadFile(kagentTokenPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// refreshTokenLoop periodically refreshes the token
func (s *KAgentTokenService) refreshTokenLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopChan:
			return
		case <-ticker.C:
			token, err := s.readToken()
			if err == nil {
				s.mu.Lock()
				currentToken := s.token
				if token != currentToken {
					s.token = token
				}
				s.mu.Unlock()
			}
		}
	}
}

// RoundTripper wraps HTTP transport to add token headers
type TokenRoundTripper struct {
	base         http.RoundTripper
	tokenService *KAgentTokenService
}

func (rt *TokenRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.tokenService.AddHeaders(req)
	return rt.base.RoundTrip(req)
}

// NewHTTPClientWithToken creates an HTTP client with token service integration
func NewHTTPClientWithToken(tokenService *KAgentTokenService) *http.Client {
	return &http.Client{
		Transport: &TokenRoundTripper{
			base:         http.DefaultTransport,
			tokenService: tokenService,
		},
		Timeout: 30 * time.Second,
	}
}
