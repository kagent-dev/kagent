package mockllm

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// For now, we'll use a simple approach where we create mocks with JSON-compatible structures
// that can be marshaled to/from the SDK types. This allows us to test the basic functionality
// while using the SDK types in the type definitions.

func TestSimpleOpenAIMock(t *testing.T) {
	// Create a simple config - we'll use JSON marshaling to convert to SDK types
	openaiRequest := openai.ChatCompletionNewParams{
		Model: "gpt-4o-mini",
		Messages: []openai.ChatCompletionMessageParamUnion{
			{
				OfUser: &openai.ChatCompletionUserMessageParam{
					Role: "user",
					Content: openai.ChatCompletionUserMessageParamContentUnion{
						OfString: openai.String("Hello"),
					},
				},
			},
		},
	}

	openaiResponse := openai.ChatCompletion{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Created: 1677652288,
		Model:   "gpt-4o-mini",
		Choices: []openai.ChatCompletionChoice{
			{
				Index: 0,
				Message: openai.ChatCompletionMessage{
					Role:    "assistant",
					Content: "Hello! How can I help you today?",
				},
				FinishReason: "stop",
			},
		},
	}

	// Convert to JSON and back to get SDK-compatible structure
	var mock OpenAIMock
	mock.Name = "test-response"
	mock.Response = openaiResponse
	mock.Stream = false

	// Marshal and unmarshal the request to get it in the right format
	reqBytes, err := json.Marshal(openaiRequest)
	require.NoError(t, err)
	err = json.Unmarshal(reqBytes, &mock.Request)
	require.NoError(t, err)

	config := Config{
		OpenAI: []OpenAIMock{mock},
	}

	// Start server
	server := NewServer(config)
	baseURL, err := server.Start()
	require.NoError(t, err)
	defer server.Stop()

	// Make request
	req, err := http.NewRequest("POST", baseURL+"/v1/chat/completions", bytes.NewReader(reqBytes))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Check response
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var responseBody map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&responseBody)
	require.NoError(t, err)

	assert.Equal(t, "chatcmpl-123", responseBody["id"])
	assert.Equal(t, "chat.completion", responseBody["object"])
}

func TestSimpleAnthropicMock(t *testing.T) {
	// Create a simple config - we'll use JSON marshaling to convert to SDK types
	anthropicRequest := anthropic.MessageNewParams{
		Model:     "claude-3-5-sonnet-20240620",
		MaxTokens: 1000,
		Messages: []anthropic.MessageParam{
			{
				Role: anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{
					{
						OfText: &anthropic.TextBlockParam{
							Text: "Hello",
						},
					},
				},
			},
		},
	}

	anthropicResponse := anthropic.Message{
		ID:   "msg_123",
		Type: "message",
		Role: "assistant",
		Content: []anthropic.ContentBlockUnion{
			{
				Type: "text",
				Text: "Hello! How can I assist you today?",
			},
		},
		Model:      "claude-3-5-sonnet-20240620",
		StopReason: "end_turn",
	}

	// Convert to JSON and back to get SDK-compatible structure
	var mock AnthropicMock
	mock.Name = "test-response"
	mock.Response = anthropicResponse

	// Marshal and unmarshal the request to get it in the right format
	reqBytes, err := json.Marshal(anthropicRequest)
	require.NoError(t, err)
	err = json.Unmarshal(reqBytes, &mock.Request)
	require.NoError(t, err)

	config := Config{
		Anthropic: []AnthropicMock{mock},
	}

	// Start server
	server := NewServer(config)
	baseURL, err := server.Start()
	require.NoError(t, err)
	defer server.Stop()

	// Make request
	req, err := http.NewRequest("POST", baseURL+"/v1/messages", bytes.NewReader(reqBytes))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "test-key")
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Check response
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var responseBody map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&responseBody)
	require.NoError(t, err)

	assert.Equal(t, "msg_123", responseBody["id"])
	assert.Equal(t, "message", responseBody["type"])
}

func TestHealthCheck(t *testing.T) {
	config := Config{}
	server := NewServer(config)
	baseURL, err := server.Start()
	require.NoError(t, err)
	defer server.Stop()

	resp, err := http.Get(baseURL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var responseBody map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&responseBody)
	require.NoError(t, err)

	assert.Equal(t, "healthy", responseBody["status"])
	assert.Equal(t, "mock-llm", responseBody["service"])
}
