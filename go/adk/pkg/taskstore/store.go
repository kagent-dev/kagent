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
	metadataKeyKagentType       = "kagent_type"
	metadataKeyAdkType          = "adk_type"
	metadataKeyKagentLongRun    = "kagent_is_long_running"
	metadataKeyAdkLongRun       = "adk_is_long_running"
	headerContentType           = "Content-Type"
	contentTypeJSON             = "application/json"
	partTypeFunctionCall        = "function_call"
	partTypeFunctionResponse    = "function_response"
	partKeyName                 = "name"
	partKeyResponse             = "response"
	confirmationFunctionName    = "adk_request_confirmation"
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
		if normalized := normalizeMessage(item); normalized != nil {
			cleaned = append(cleaned, normalized)
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
		if normalized := normalizeArtifact(a); normalized != nil {
			cleaned = append(cleaned, normalized)
		}
	}
	return cleaned
}

func normalizeArtifact(artifact *a2atype.Artifact) *a2atype.Artifact {
	if artifact == nil {
		return nil
	}
	parts := normalizeParts(artifact.Parts)
	if len(parts) == 0 {
		return nil
	}
	copyArtifact := *artifact
	copyArtifact.Parts = parts
	return &copyArtifact
}

func normalizeMessage(message *a2atype.Message) *a2atype.Message {
	if message == nil {
		return nil
	}
	parts := normalizeParts(message.Parts)
	if len(parts) == 0 {
		return nil
	}
	copyMessage := *message
	copyMessage.Parts = parts
	return &copyMessage
}

func normalizeParts(parts a2atype.ContentParts) a2atype.ContentParts {
	var normalized a2atype.ContentParts
	for _, part := range parts {
		switch typed := part.(type) {
		case *a2atype.DataPart:
			if cleaned := normalizeDataPart(*typed); cleaned != nil {
				normalized = append(normalized, cleaned)
			}
		case a2atype.DataPart:
			if cleaned := normalizeDataPart(typed); cleaned != nil {
				normalized = append(normalized, cleaned)
			}
		default:
			normalized = append(normalized, part)
		}
	}
	return normalized
}

func normalizeDataPart(part a2atype.DataPart) a2atype.Part {
	if isTransientHitlResponse(part) {
		return nil
	}
	return part
}

func isTransientHitlResponse(part a2atype.DataPart) bool {
	if readPartType(part.Metadata) != partTypeFunctionResponse {
		return false
	}
	response, _ := part.Data[partKeyResponse].(map[string]any)
	if response == nil {
		return false
	}
	status, _ := response["status"].(string)
	switch status {
	case "confirmation_requested", "pending":
		return true
	default:
		return false
	}
}

func normalizeStatusMessage(task *a2atype.Task) {
	if task == nil {
		return
	}

	task.Status.Message = normalizeMessage(task.Status.Message)
	if task.Status.State != a2atype.TaskStateInputRequired {
		return
	}
	if hasPendingConfirmationMessage(task.Status.Message) {
		return
	}
	for i := len(task.History) - 1; i >= 0; i-- {
		if hasPendingConfirmationMessage(task.History[i]) {
			task.Status.Message = task.History[i]
			return
		}
	}
}

func hasPendingConfirmationMessage(message *a2atype.Message) bool {
	if message == nil {
		return false
	}
	for _, part := range message.Parts {
		dp := asDataPart(part)
		if dp == nil {
			continue
		}
		if readPartType(dp.Metadata) != partTypeFunctionCall {
			continue
		}
		if !readLongRunning(dp.Metadata) {
			continue
		}
		name, _ := dp.Data[partKeyName].(string)
		if name == confirmationFunctionName {
			return true
		}
	}
	return false
}

func asDataPart(part a2atype.Part) *a2atype.DataPart {
	switch typed := part.(type) {
	case *a2atype.DataPart:
		return typed
	case a2atype.DataPart:
		return &typed
	default:
		return nil
	}
}

func readPartType(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	if value, _ := metadata[metadataKeyAdkType].(string); value != "" {
		return value
	}
	if value, _ := metadata[metadataKeyKagentType].(string); value != "" {
		return value
	}
	return ""
}

func readLongRunning(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	if value, ok := metadata[metadataKeyAdkLongRun].(bool); ok {
		return value
	}
	if value, ok := metadata[metadataKeyKagentLongRun].(bool); ok {
		return value
	}
	return false
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
	normalizeStatusMessage(&taskCopy)

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
