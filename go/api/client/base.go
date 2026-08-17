package client

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ClientError represents a client-side error
type ClientError struct {
	StatusCode int
	Message    string
	Body       string
}

func (e *ClientError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

// ClientOption represents a configuration option for the client
type ClientOption func(*BaseClient)

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *BaseClient) {
		c.HTTPClient = httpClient
	}
}

// WithUserID sets a default user ID for requests
func WithUserID(userID string) ClientOption {
	return func(c *BaseClient) {
		c.UserID = userID
	}
}

// BaseClient contains the shared transport configuration used by all sub-clients.
type BaseClient struct {
	BaseURL    string
	HTTPClient *http.Client
	UserID     string // Default user ID for requests that require it
	grpc       grpcTransport
}

// NewBaseClient creates a new base client with the given configuration
func NewBaseClient(baseURL string, options ...ClientOption) *BaseClient {
	client := &BaseClient{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		grpc:    newGRPCTransport(),
	}

	for _, option := range options {
		option(client)
	}

	if client.HTTPClient == nil {
		client.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}

	return client
}

func (c *BaseClient) checkHealth(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("request health endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return &ClientError{
			StatusCode: resp.StatusCode,
			Message:    "health check failed",
		}
	}

	return nil
}

// GetUserIDOrDefault returns the provided userID or falls back to the client's default
func (c *BaseClient) GetUserIDOrDefault(userID string) string {
	if userID != "" {
		return userID
	}
	return c.UserID
}
