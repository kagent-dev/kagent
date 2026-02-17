package taskstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	a2atype "github.com/a2aproject/a2a-go/a2a"
)

// Constants inlined from pkg/a2a to avoid import cycle (taskstore ↔ a2a).
const (
	metadataKeyAdkPartial = "adk_partial"
	headerContentType     = "Content-Type"
	contentTypeJSON       = "application/json"
)

// KAgentTaskStore persists A2A tasks to KAgent via REST API
type KAgentTaskStore struct {
	BaseURL string
	Client  *http.Client
}

// NewKAgentTaskStoreWithClient creates a new KAgentTaskStore with a custom HTTP client.
// If client is nil, http.DefaultClient is used.
func NewKAgentTaskStoreWithClient(baseURL string, client *http.Client) *KAgentTaskStore {
	if client == nil {
		client = http.DefaultClient
	}
	return &KAgentTaskStore{
		BaseURL: baseURL,
		Client:  client,
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
	if partial, ok := item.Metadata[metadataKeyAdkPartial].(bool); ok {
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
	if task == nil {
		return fmt.Errorf("task cannot be nil")
	}

	// Work on a shallow copy so the caller's task is not mutated.
	taskCopy := *task
	if taskCopy.History != nil {
		taskCopy.History = s.cleanPartialEvents(taskCopy.History)
	}

	taskJSON, err := json.Marshal(&taskCopy)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/api/tasks", bytes.NewReader(taskJSON))
	if err != nil {
		return fmt.Errorf("failed to create save request: %w", err)
	}
	req.Header.Set(headerContentType, contentTypeJSON)

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
func (s *KAgentTaskStore) Get(ctx context.Context, taskID string) (*a2atype.Task, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/api/tasks/"+url.PathEscape(taskID), nil)
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
