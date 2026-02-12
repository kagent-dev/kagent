package taskstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	a2aschema "github.com/a2aproject/a2a-go/a2a"
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
	Error   bool               `json:"error"`
	Data    *a2aschema.PushConfig `json:"data,omitempty"`
	Message string             `json:"message,omitempty"`
}

// Save stores a push notification configuration
func (s *KAgentPushNotificationStore) Save(ctx context.Context, taskID string, config *a2aschema.PushConfig) (*a2aschema.PushConfig, error) {
	if config == nil {
		return nil, fmt.Errorf("push notification config cannot be nil")
	}
	if taskID == "" {
		return nil, fmt.Errorf("taskID cannot be empty")
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal push notification config: %w", err)
	}

	url := fmt.Sprintf("%s/api/tasks/%s/push-notifications", s.BaseURL, taskID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(configJSON))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("failed to set push notification: status %d", resp.StatusCode)
	}

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
func (s *KAgentPushNotificationStore) Get(ctx context.Context, taskID, configID string) (*a2aschema.PushConfig, error) {
	if taskID == "" {
		return nil, fmt.Errorf("taskID cannot be empty")
	}
	if configID == "" {
		return nil, fmt.Errorf("configID cannot be empty")
	}

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

	var wrapped KAgentPushNotificationResponse
	if err := json.NewDecoder(resp.Body).Decode(&wrapped); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if wrapped.Error {
		return nil, fmt.Errorf("error from server: %s", wrapped.Message)
	}

	return wrapped.Data, nil
}

// List retrieves all push notification configurations for a task
func (s *KAgentPushNotificationStore) List(ctx context.Context, taskID string) ([]*a2aschema.PushConfig, error) {
	if taskID == "" {
		return nil, fmt.Errorf("taskID cannot be empty")
	}

	url := fmt.Sprintf("%s/api/tasks/%s/push-notifications", s.BaseURL, taskID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list push notifications: status %d", resp.StatusCode)
	}

	var result struct {
		Error   bool                    `json:"error"`
		Data    []*a2aschema.PushConfig `json:"data,omitempty"`
		Message string                  `json:"message,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if result.Error {
		return nil, fmt.Errorf("error from server: %s", result.Message)
	}

	return result.Data, nil
}

// Delete removes a push notification configuration
func (s *KAgentPushNotificationStore) Delete(ctx context.Context, taskID, configID string) error {
	if taskID == "" {
		return fmt.Errorf("taskID cannot be empty")
	}
	if configID == "" {
		return fmt.Errorf("configID cannot be empty")
	}

	url := fmt.Sprintf("%s/api/tasks/%s/push-notifications/%s", s.BaseURL, taskID, configID)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete push notification: status %d", resp.StatusCode)
	}

	return nil
}

// DeleteAll removes all push notification configurations for a task
func (s *KAgentPushNotificationStore) DeleteAll(ctx context.Context, taskID string) error {
	if taskID == "" {
		return fmt.Errorf("taskID cannot be empty")
	}

	url := fmt.Sprintf("%s/api/tasks/%s/push-notifications", s.BaseURL, taskID)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete all push notifications: status %d", resp.StatusCode)
	}

	return nil
}
