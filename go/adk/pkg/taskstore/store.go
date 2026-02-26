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
	metadataKeyKagentAdkPartial = "kagent_adk_partial"
	metadataKeyAdkPartial       = "adk_partial"
	headerContentType           = "Content-Type"
	contentTypeJSON             = "application/json"
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

// isPartialMeta checks if a metadata map has a partial flag set to true.
// It checks both the upstream ADK key (adk_partial) and the kagent key
// (kagent_adk_partial) so that events from either prefix are recognised.
func isPartialMeta(meta map[string]any) bool {
	if meta == nil {
		return false
	}
	if partial, ok := meta[metadataKeyAdkPartial].(bool); ok && partial {
		return true
	}
	if partial, ok := meta[metadataKeyKagentAdkPartial].(bool); ok && partial {
		return true
	}
	return false
}

// cleanPartialEvents removes partial streaming events from history.
func cleanPartialEvents(history []*a2atype.Message) []*a2atype.Message {
	var cleaned []*a2atype.Message
	for _, item := range history {
		if item != nil && isPartialMeta(item.Metadata) {
			continue
		}
		cleaned = append(cleaned, item)
	}
	return cleaned
}

// cleanPartialArtifacts removes partial streaming artifacts.
func cleanPartialArtifacts(artifacts []*a2atype.Artifact) []*a2atype.Artifact {
	var cleaned []*a2atype.Artifact
	for _, a := range artifacts {
		if a != nil && isPartialMeta(a.Metadata) {
			continue
		}
		cleaned = append(cleaned, a)
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
		taskCopy.History = cleanPartialEvents(taskCopy.History)
	}
	if taskCopy.Artifacts != nil {
		taskCopy.Artifacts = cleanPartialArtifacts(taskCopy.Artifacts)
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
		return fmt.Errorf("failed to execute save task request: %w", err)
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
		return nil, fmt.Errorf("failed to execute get task request: %w", err)
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
