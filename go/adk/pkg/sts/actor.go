package sts

import (
	"fmt"
	"os"
	"strings"
)

// DefaultServiceAccountTokenPath is the default path for Kubernetes service account tokens.
const DefaultServiceAccountTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

// ActorTokenService provides actor tokens for STS delegation.
// It reads Kubernetes service account tokens from a file path.
type ActorTokenService struct {
	tokenPath string
}

// NewActorTokenService creates a new ActorTokenService.
// If tokenPath is empty, it uses the default Kubernetes service account token path.
func NewActorTokenService(tokenPath string) *ActorTokenService {
	if tokenPath == "" {
		tokenPath = DefaultServiceAccountTokenPath
	}
	return &ActorTokenService{
		tokenPath: tokenPath,
	}
}

// GetActorToken retrieves the actor token for STS delegation.
// This method reads the token from the file each time it's called.
// If loading fails, it returns an empty string and an error (or nil if file doesn't exist).
func (s *ActorTokenService) GetActorToken() (string, error) {
	// Check if file exists first
	if _, err := os.Stat(s.tokenPath); os.IsNotExist(err) {
		return "", nil
	}

	data, err := os.ReadFile(s.tokenPath)
	if err != nil {
		return "", fmt.Errorf("failed to read actor token from %s: %w", s.tokenPath, err)
	}

	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("empty actor token found at %s", s.tokenPath)
	}

	return token, nil
}

// TokenPath returns the configured token path.
func (s *ActorTokenService) TokenPath() string {
	return s.tokenPath
}
