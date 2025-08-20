package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/pkg/client/api"
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

// BaseClient contains the shared HTTP functionality used by all sub-clients
type BaseClient struct {
	BaseURL    string
	HTTPClient *http.Client
	UserID     string // Default user ID for requests that require it
}

// NewBaseClient creates a new base client with the given configuration
func NewBaseClient(baseURL string, options ...ClientOption) *BaseClient {
	client := &BaseClient{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
	}

	for _, option := range options {
		option(client)
	}

	if client.HTTPClient == nil {
		client.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}

	return client
}

// HTTP helper methods

func (c *BaseClient) buildURL(path string) string {
	return c.BaseURL + path
}

func (c *BaseClient) addUserID(req *http.Request, userID string) {
	if userID == "" {
		return
	}

	u := req.URL
	q := u.Query()
	q.Set("user_id", userID)
	u.RawQuery = q.Encode()
	req.Header.Set("X-User-ID", userID)
}

func (c *BaseClient) doRequest(ctx context.Context, method, path string, body interface{}, userID string) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonBody)
	}

	urlStr := c.buildURL(path)
	req, err := http.NewRequestWithContext(ctx, method, urlStr, reqBody)
	if err != nil {
		return nil, err
	}
	if userID != "" {
		c.addUserID(req, userID)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var apiErr api.APIError
		if json.Unmarshal(bodyBytes, &apiErr) == nil && apiErr.Error != "" {
			return nil, &ClientError{
				StatusCode: resp.StatusCode,
				Message:    apiErr.Error,
				Body:       string(bodyBytes),
			}
		}

		return nil, &ClientError{
			StatusCode: resp.StatusCode,
			Message:    "Request failed",
			Body:       string(bodyBytes),
		}
	}

	return resp, nil
}

func (c *BaseClient) Get(ctx context.Context, path string, userID string) (*http.Response, error) {
	return c.doRequest(ctx, http.MethodGet, path, nil, userID)
}

func (c *BaseClient) Post(ctx context.Context, path string, body interface{}, userID string) (*http.Response, error) {
	return c.doRequest(ctx, http.MethodPost, path, body, userID)
}

func (c *BaseClient) Put(ctx context.Context, path string, body interface{}, userID string) (*http.Response, error) {
	return c.doRequest(ctx, http.MethodPut, path, body, userID)
}

func (c *BaseClient) Delete(ctx context.Context, path string, userID string) (*http.Response, error) {
	return c.doRequest(ctx, http.MethodDelete, path, nil, userID)
}

func DecodeResponse(resp *http.Response, target interface{}) error {
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(target)
}

// GetUserIDOrDefault returns the provided userID or falls back to the client's default
func (c *BaseClient) GetUserIDOrDefault(userID string) string {
	if userID != "" {
		return userID
	}
	return c.UserID
}
