package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	autogen_client "github.com/kagent-dev/kagent/go/internal/autogen/client"
)

type InMemoryAutogenClient struct {
	mu sync.RWMutex

	// Minimal storage for FetchTools functionality
	toolsByServer map[string][]*autogen_client.NamedTool
}

func NewInMemoryAutogenClient() *InMemoryAutogenClient {
	return &InMemoryAutogenClient{
		toolsByServer: make(map[string][]*autogen_client.NamedTool),
	}
}

// NewMockAutogenClient creates a new in-memory autogen client for backward compatibility
func NewMockAutogenClient() *InMemoryAutogenClient {
	return NewInMemoryAutogenClient()
}

// GetVersion implements the Client interface
func (m *InMemoryAutogenClient) GetVersion(_ context.Context) (string, error) {
	return "1.0.0-inmemory", nil
}

// InvokeTask implements the Client interface
func (m *InMemoryAutogenClient) InvokeTask(ctx context.Context, req *autogen_client.InvokeTaskRequest) (*autogen_client.InvokeTaskResult, error) {

	// Determine the response based on context (session/no session)
	// If Messages is set (even if empty), it's a session-based call

	return &autogen_client.InvokeTaskResult{
		TaskResult: autogen_client.TaskResult{
			Messages: []autogen_client.Event{
				&autogen_client.TextMessage{
					BaseChatMessage: autogen_client.BaseChatMessage{
						BaseEvent: autogen_client.BaseEvent{
							Type: "TextMessage",
						},
						Source:   "assistant",
						Metadata: map[string]string{},
						ModelsUsage: &autogen_client.ModelsUsage{
							PromptTokens:     0,
							CompletionTokens: 0,
						},
					},
					Content: fmt.Sprintf("Session task completed: %s", req.Task),
				},
			},
		},
	}, nil
}

// InvokeTaskStream implements the Client interface
func (m *InMemoryAutogenClient) InvokeTaskStream(ctx context.Context, req *autogen_client.InvokeTaskRequest) (<-chan *autogen_client.SseEvent, error) {
	ch := make(chan *autogen_client.SseEvent, 1)
	go func() {
		defer close(ch)
		// Create a proper TextMessage event in JSON format
		textEvent := map[string]interface{}{
			"type":     "TextMessage",
			"source":   "assistant",
			"content":  fmt.Sprintf("Session task completed: %s", req.Task),
			"metadata": map[string]string{},
			"models_usage": map[string]interface{}{
				"prompt_tokens":     0,
				"completion_tokens": 0,
			},
		}

		jsonData, err := json.Marshal(textEvent)
		if err != nil {
			return
		}

		ch <- &autogen_client.SseEvent{
			Event: "message",
			Data:  jsonData,
		}
	}()

	return ch, nil
}

// FetchTools implements the Client interface
func (m *InMemoryAutogenClient) FetchTools(ctx context.Context, req *autogen_client.ToolServerRequest) (*autogen_client.ToolServerResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tools, exists := m.toolsByServer[req.Server.Label]
	if !exists {
		return &autogen_client.ToolServerResponse{
			Tools: []*autogen_client.NamedTool{},
		}, nil
	}

	return &autogen_client.ToolServerResponse{
		Tools: tools,
	}, nil
}

// Validate implements the Client interface
func (m *InMemoryAutogenClient) Validate(ctx context.Context, req *autogen_client.ValidationRequest) (*autogen_client.ValidationResponse, error) {
	return &autogen_client.ValidationResponse{
		IsValid:  true,
		Errors:   []*autogen_client.ValidationError{},
		Warnings: []*autogen_client.ValidationError{},
	}, nil
}

// Helper method to add tools for testing purposes (not part of the interface)
func (m *InMemoryAutogenClient) AddToolsForServer(serverLabel string, tools []*autogen_client.NamedTool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.toolsByServer[serverLabel] = tools
}

func (m *InMemoryAutogenClient) ListSupportedModels(ctx context.Context) (*autogen_client.ProviderModels, error) {
	return &autogen_client.ProviderModels{
		"openai": {
			{
				Name: "gpt-4o",
			},
		},
	}, nil
}
