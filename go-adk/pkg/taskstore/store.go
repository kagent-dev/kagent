package taskstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	a2aschema "github.com/a2aproject/a2a-go/a2a"
)

// KAgentTaskStore persists A2A tasks to KAgent via REST API
type KAgentTaskStore struct {
	BaseURL string
	Client  *http.Client
}

// NewKAgentTaskStoreWithClient creates a new KAgentTaskStore with a custom HTTP client
func NewKAgentTaskStoreWithClient(baseURL string, client *http.Client) *KAgentTaskStore {
	return &KAgentTaskStore{
		BaseURL: baseURL,
		Client:  client,
	}
}

// KAgentTaskResponse wraps KAgent controller API responses
type KAgentTaskResponse struct {
	Error   bool            `json:"error"`
	Data    *a2aschema.Task `json:"data,omitempty"`
	Message string          `json:"message,omitempty"`
}

// isPartialEvent checks if a history item is a partial ADK streaming event
func (s *KAgentTaskStore) isPartialEvent(item *a2aschema.Message) bool {
	if item == nil || item.Metadata == nil {
		return false
	}
	if partial, ok := item.Metadata["adk_partial"].(bool); ok {
		return partial
	}
	return false
}

// cleanPartialEvents removes partial streaming events from history
func (s *KAgentTaskStore) cleanPartialEvents(history []*a2aschema.Message) []*a2aschema.Message {
	var cleaned []*a2aschema.Message
	for _, item := range history {
		if !s.isPartialEvent(item) {
			cleaned = append(cleaned, item)
		}
	}
	return cleaned
}

// Save saves a task to KAgent
func (s *KAgentTaskStore) Save(ctx context.Context, task *a2aschema.Task) error {
	// Clean any partial events from history before saving
	if task.History != nil {
		task.History = s.cleanPartialEvents(task.History)
	}

	taskJSON, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/api/tasks", bytes.NewReader(taskJSON))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to save task: status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Get retrieves a task from KAgent
func (s *KAgentTaskStore) Get(ctx context.Context, taskID string) (*a2aschema.Task, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/api/tasks/"+taskID, nil)
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
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get task: status %d, body: %s", resp.StatusCode, string(body))
	}

	// Unwrap the StandardResponse envelope from the Go controller
	var wrapped KAgentTaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&wrapped); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return wrapped.Data, nil
}

// Delete deletes a task from KAgent
func (s *KAgentTaskStore) Delete(ctx context.Context, taskID string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", s.BaseURL+"/api/tasks/"+taskID, nil)
	if err != nil {
		return err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete task: status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}
