package taskstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a"
)

// KAgentTaskStore persists A2A tasks to KAgent via REST API
type KAgentTaskStore struct {
	BaseURL string
	Client  *http.Client
	// Event-based sync: track pending save operations
	// Multiple waiters per taskID are supported via slice of channels
	saveEvents map[string][]chan struct{}
	mu         sync.RWMutex
}

// NewKAgentTaskStoreWithClient creates a new KAgentTaskStore with a custom HTTP client
func NewKAgentTaskStoreWithClient(baseURL string, client *http.Client) *KAgentTaskStore {
	return &KAgentTaskStore{
		BaseURL:    baseURL,
		Client:     client,
		saveEvents: make(map[string][]chan struct{}),
	}
}

// KAgentTaskResponse wraps KAgent controller API responses
type KAgentTaskResponse struct {
	Error   bool          `json:"error"`
	Data    *a2atype.Task `json:"data,omitempty"`
	Message string        `json:"message,omitempty"`
}

// isPartialEvent checks if a history item is a partial ADK streaming event
func (s *KAgentTaskStore) isPartialEvent(item *a2atype.Message) bool {
	if item == nil || item.Metadata == nil {
		return false
	}
	if partial, ok := item.Metadata[a2a.MetadataKeyAdkPartial].(bool); ok {
		return partial
	}
	return false
}

// cleanPartialEvents removes partial streaming events from history.
// History in a2a-go Task is []*Message.
func (s *KAgentTaskStore) cleanPartialEvents(history []*a2atype.Message) []*a2atype.Message {
	var cleaned []*a2atype.Message
	for _, item := range history {
		if !s.isPartialEvent(item) {
			cleaned = append(cleaned, item)
		}
	}
	return cleaned
}

// Save saves a task to KAgent
func (s *KAgentTaskStore) Save(ctx context.Context, task *a2atype.Task) error {
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
		return fmt.Errorf("failed to create save request: %w", err)
	}
	req.Header.Set(a2a.HeaderContentType, a2a.ContentTypeJSON)

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to save task: status %d, body: %s", resp.StatusCode, string(body))
	}

	// Signal that save completed (event-based sync) - notify all waiters
	s.mu.Lock()
	if channels, ok := s.saveEvents[string(task.ID)]; ok {
		for _, ch := range channels {
			close(ch)
		}
		delete(s.saveEvents, string(task.ID))
	}
	s.mu.Unlock()

	return nil
}

// Get retrieves a task from KAgent
func (s *KAgentTaskStore) Get(ctx context.Context, taskID string) (*a2atype.Task, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/api/tasks/"+taskID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get request: %w", err)
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
		return fmt.Errorf("failed to create delete request: %w", err)
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

// WaitForSave waits for a task to be saved (event-based sync)
// Multiple waiters for the same taskID are supported
func (s *KAgentTaskStore) WaitForSave(ctx context.Context, taskID string, timeout time.Duration) error {
	ch := make(chan struct{})

	s.mu.Lock()
	s.saveEvents[taskID] = append(s.saveEvents[taskID], ch)
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		// Remove this specific channel from the slice
		if channels, ok := s.saveEvents[taskID]; ok {
			for i, c := range channels {
				if c == ch {
					s.saveEvents[taskID] = append(channels[:i], channels[i+1:]...)
					break
				}
			}
			// Clean up empty slice
			if len(s.saveEvents[taskID]) == 0 {
				delete(s.saveEvents, taskID)
			}
		}
		s.mu.Unlock()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("timeout waiting for task save")
	}
}
