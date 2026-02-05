package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// KAgentTaskStore persists A2A tasks to KAgent via REST API
type KAgentTaskStore struct {
	BaseURL string
	Client  *http.Client
	// Event-based sync: track pending save operations
	saveEvents map[string]chan struct{}
	closed     map[string]bool // Track which channels have been closed
	mu         sync.RWMutex
}

// NewKAgentTaskStoreWithClient creates a new KAgentTaskStore with a custom HTTP client
func NewKAgentTaskStoreWithClient(baseURL string, client *http.Client) *KAgentTaskStore {
	return &KAgentTaskStore{
		BaseURL:    baseURL,
		Client:     client,
		saveEvents: make(map[string]chan struct{}),
		closed:     make(map[string]bool),
	}
}

// KAgentTaskResponse wraps KAgent controller API responses
type KAgentTaskResponse struct {
	Error   bool           `json:"error"`
	Data    *protocol.Task `json:"data,omitempty"`
	Message string         `json:"message,omitempty"`
}

// isPartialEvent checks if a history item is a partial ADK streaming event
func (s *KAgentTaskStore) isPartialEvent(item protocol.Message) bool {
	if item.Metadata == nil {
		return false
	}
	if partial, ok := item.Metadata["adk_partial"].(bool); ok {
		return partial
	}
	return false
}

// cleanPartialEvents removes partial streaming events from history
func (s *KAgentTaskStore) cleanPartialEvents(history []protocol.Message) []protocol.Message {
	var cleaned []protocol.Message
	for _, item := range history {
		if !s.isPartialEvent(item) {
			cleaned = append(cleaned, item)
		}
	}
	return cleaned
}

// Save saves a task to KAgent
func (s *KAgentTaskStore) Save(ctx context.Context, task *protocol.Task) error {
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
	req.Header.Set(HeaderContentType, ContentTypeJSON)

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("failed to save task: status %d", resp.StatusCode)
	}

	// Signal that save completed (event-based sync)
	s.mu.Lock()
	if ch, ok := s.saveEvents[task.ID]; ok {
		// Only close if channel hasn't been closed yet
		if !s.closed[task.ID] {
			close(ch)
			s.closed[task.ID] = true
		}
		delete(s.saveEvents, task.ID)
		delete(s.closed, task.ID)
	}
	s.mu.Unlock()

	return nil
}

// Get retrieves a task from KAgent
func (s *KAgentTaskStore) Get(ctx context.Context, taskID string) (*protocol.Task, error) {
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
		return nil, fmt.Errorf("failed to get task: status %d", resp.StatusCode)
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
		return fmt.Errorf("failed to delete task: status %d", resp.StatusCode)
	}

	return nil
}

// WaitForSave waits for a task to be saved (event-based sync)
func (s *KAgentTaskStore) WaitForSave(ctx context.Context, taskID string, timeout time.Duration) error {
	s.mu.Lock()
	ch := make(chan struct{})
	s.saveEvents[taskID] = ch
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.saveEvents, taskID)
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
