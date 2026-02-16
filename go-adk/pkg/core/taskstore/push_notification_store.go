package taskstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// KAgentPushNotificationStore handles push notification operations via KAgent API
type KAgentPushNotificationStore struct {
	BaseURL string
	Client  *http.Client
}

// NewKAgentPushNotificationStoreWithClient creates a new KAgentPushNotificationStore with a custom HTTP client
func NewKAgentPushNotificationStoreWithClient(baseURL string, client *http.Client) *KAgentPushNotificationStore {
	return &KAgentPushNotificationStore{
		BaseURL: baseURL,
		Client:  client,
	}
}

// KAgentPushNotificationResponse wraps KAgent controller API responses for push notifications
type KAgentPushNotificationResponse struct {
	Error   bool                                 `json:"error"`
	Data    *protocol.TaskPushNotificationConfig `json:"data,omitempty"`
	Message string                               `json:"message,omitempty"`
}

// Set stores a push notification configuration
func (s *KAgentPushNotificationStore) Set(ctx context.Context, config *protocol.TaskPushNotificationConfig) (*protocol.TaskPushNotificationConfig, error) {
	if config == nil {
		return nil, fmt.Errorf("push notification config cannot be nil")
	}
	if config.TaskID == "" {
		return nil, fmt.Errorf("push notification config TaskID cannot be empty")
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal push notification config: %w", err)
	}

	// Use /api/tasks/{task_id}/push-notifications endpoint
	url := fmt.Sprintf("%s/api/tasks/%s/push-notifications", s.BaseURL, config.TaskID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(configJSON))
	if err != nil {
		return nil, err
	}
	req.Header.Set(a2a.HeaderContentType, a2a.ContentTypeJSON)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("failed to set push notification: status %d", resp.StatusCode)
	}

	// Unwrap the StandardResponse envelope from the Go controller
	var wrapped KAgentPushNotificationResponse
	if err := json.NewDecoder(resp.Body).Decode(&wrapped); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if wrapped.Error {
		return nil, fmt.Errorf("error from server: %s", wrapped.Message)
	}

	return wrapped.Data, nil
}

// Get retrieves a push notification configuration
func (s *KAgentPushNotificationStore) Get(ctx context.Context, taskID, configID string) (*protocol.TaskPushNotificationConfig, error) {
	if taskID == "" {
		return nil, fmt.Errorf("taskID cannot be empty")
	}
	if configID == "" {
		return nil, fmt.Errorf("configID cannot be empty")
	}

	// Use /api/tasks/{task_id}/push-notifications/{config_id} endpoint
	url := fmt.Sprintf("%s/api/tasks/%s/push-notifications/%s", s.BaseURL, taskID, configID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get push notification: status %d", resp.StatusCode)
	}

	// Unwrap the StandardResponse envelope from the Go controller
	var wrapped KAgentPushNotificationResponse
	if err := json.NewDecoder(resp.Body).Decode(&wrapped); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if wrapped.Error {
		return nil, fmt.Errorf("error from server: %s", wrapped.Message)
	}

	return wrapped.Data, nil
}
