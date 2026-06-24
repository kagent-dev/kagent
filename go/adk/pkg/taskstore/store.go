package taskstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2ataskstore "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
)

// Constants for partial-event metadata keys (inlined to avoid import cycle).
const (
	metadataKeyKagentPartial    = "kagent_partial"
	metadataKeyKagentAdkPartial = "kagent_adk_partial"
	metadataKeyAdkPartial       = "adk_partial"
	headerContentType           = "Content-Type"
	contentTypeJSON             = "application/json"
)

// KAgentTaskStore persists A2A tasks to KAgent via REST API and implements
// a2asrv.TaskStore.
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
// It checks the canonical kagent key (kagent_adk_partial) as well as legacy keys
// (adk_partial, kagent_partial) so that events from any prefix are recognised.
func isPartialMeta(meta map[string]any) bool {
	if meta == nil {
		return false
	}
	for _, key := range []string{metadataKeyKagentPartial, metadataKeyAdkPartial, metadataKeyKagentAdkPartial} {
		if partial, ok := meta[key].(bool); ok && partial {
			return true
		}
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
		if item != nil && len(item.Parts) > 0 {
			cleaned = append(cleaned, item)
		}
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
		if a != nil && len(a.Parts) > 0 {
			cleaned = append(cleaned, a)
		}
	}
	return cleaned
}

func (s *KAgentTaskStore) saveTask(ctx context.Context, task *a2atype.Task) (a2ataskstore.TaskVersion, error) {
	if task == nil {
		return a2ataskstore.TaskVersionMissing, fmt.Errorf("task cannot be nil")
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
		return a2ataskstore.TaskVersionMissing, fmt.Errorf("failed to marshal task: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/api/tasks", bytes.NewReader(taskJSON))
	if err != nil {
		return a2ataskstore.TaskVersionMissing, fmt.Errorf("failed to create save request: %w", err)
	}
	req.Header.Set(headerContentType, contentTypeJSON)
	req.Header.Set(a2atype.SvcParamVersion, string(a2atype.Version))

	resp, err := s.Client.Do(req)
	if err != nil {
		return a2ataskstore.TaskVersionMissing, fmt.Errorf("failed to execute save task request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return a2ataskstore.TaskVersionMissing, fmt.Errorf("failed to save task: status %d, body: %s", resp.StatusCode, string(body))
	}

	return a2ataskstore.TaskVersionMissing, nil
}

// Create implements taskstore.Store.
func (s *KAgentTaskStore) Create(ctx context.Context, task *a2atype.Task) (a2ataskstore.TaskVersion, error) {
	return s.saveTask(ctx, task)
}

// Update implements taskstore.Store.
func (s *KAgentTaskStore) Update(ctx context.Context, update *a2ataskstore.UpdateRequest) (a2ataskstore.TaskVersion, error) {
	if update == nil {
		return a2ataskstore.TaskVersionMissing, fmt.Errorf("update request cannot be nil")
	}
	return s.saveTask(ctx, update.Task)
}

// Get implements taskstore.Store.
func (s *KAgentTaskStore) Get(ctx context.Context, taskID a2atype.TaskID) (*a2ataskstore.StoredTask, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/api/tasks/"+url.PathEscape(string(taskID)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get request: %w", err)
	}
	req.Header.Set(a2atype.SvcParamVersion, string(a2atype.Version))

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute get task request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, a2atype.ErrTaskNotFound
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get task: status %d, body: %s", resp.StatusCode, string(body))
	}

	var wrapped KAgentTaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&wrapped); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	if wrapped.Data == nil {
		return nil, a2atype.ErrTaskNotFound
	}

	return &a2ataskstore.StoredTask{
		Task:    wrapped.Data,
		Version: a2ataskstore.TaskVersionMissing,
	}, nil
}

// List implements a2asrv.TaskStore. Listing is not supported against the KAgent task API.
func (s *KAgentTaskStore) List(ctx context.Context, req *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	return nil, fmt.Errorf("task listing is not supported by the KAgent task store")
}
