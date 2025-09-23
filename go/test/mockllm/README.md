## Mock LLM Server Design

This document describes the current implementation of a simple mock LLM server for end-to-end tests. It provides basic request/response mocking for OpenAI and Anthropic APIs using their official SDK types.

### Goals
- Support OpenAI Chat Completions API and Anthropic Messages API request/response schemas.
- Simple configuration using Go structs with official SDK types.
- Deterministic responses for testing without network calls.
- Minimal setup for basic testing scenarios.

### Current Implementation Status
- ✅ Basic OpenAI Chat Completions API support (non-streaming)
- ✅ Basic Anthropic Messages API support (non-streaming)
- ✅ Simple exact and contains matching
- ✅ In-memory configuration using Go structs
- ✅ Tool/function calls
- ✅ JSON configuration files
- ❌ Streaming responses (not implemented)
- ❌ Complex scenario engine (not implemented)

### High-Level Architecture
The current implementation uses a simplified architecture:
- **Server**: HTTP server with Gorilla mux router that handles provider-specific endpoints
- **Provider Handlers**: Separate handlers for OpenAI and Anthropic that process requests and return mocked responses
- **Simple Matching**: Basic matching logic that compares incoming requests against predefined mocks
- **Direct SDK Integration**: Uses official OpenAI and Anthropic SDK types directly

### Key Types
Current implementation uses these core types:

#### Configuration
- `Config`: Root configuration containing arrays of OpenAI and Anthropic mocks
- `OpenAIMock`: Maps OpenAI requests to responses using official SDK types
- `AnthropicMock`: Maps Anthropic requests to responses using official SDK types

#### Matching
- `MatchType`: Enum for matching strategies (`exact`, `contains`)
- `OpenAIRequestMatch`: Defines how to match OpenAI requests (match type + message)
- `AnthropicRequestMatch`: Defines how to match Anthropic requests (match type + message)

### Provider Coverage

#### OpenAI Chat Completions
- **Endpoint**: `POST /v1/chat/completions`
- **Auth**: `Authorization: Bearer <token>` (presence check only)
- **Request Type**: `openai.ChatCompletionNewParams`
- **Response Type**: `openai.ChatCompletion`
- **Matching**: Exact or contains matching on the last message in the conversation

#### Anthropic Messages API
- **Endpoint**: `POST /v1/messages`
- **Auth**: `x-api-key` (presence check only)
- **Headers**: `anthropic-version` required
- **Request Type**: `anthropic.MessageNewParams`
- **Response Type**: `anthropic.Message`
- **Matching**: Exact matching on the last message in the conversation (contains not implemented)

### Configuration

```go
config := mockllm.Config{
    OpenAI: []mockllm.OpenAIMock{
        {
            Name: "simple-response",
            Match: mockllm.OpenAIRequestMatch{
                MatchType: mockllm.MatchTypeExact,
                Message: /* openai.ChatCompletionMessageParamUnion */,
            },
            Response: /* openai.ChatCompletion */,
        },
    },
    Anthropic: []mockllm.AnthropicMock{
        {
            Name: "simple-response",
            Match: mockllm.AnthropicRequestMatch{
                MatchType: mockllm.MatchTypeExact,
                Message: /* anthropic.MessageParam */,
            },
            Response: /* anthropic.Message */,
        },
    },
}
```

```json
{
  "openai": [
    {
      "name": "initial_request",
      "match": {
        "match_type": "exact",
        "message" : {
          "content": "List all nodes in the cluster",
          "role": "user"
        }
      },
      "response": {
        "id": "chatcmpl-1",
        "object": "chat.completion",
        "created": 1677652288,
        "model": "gpt-4.1-mini",
        "choices": [
          {
            "index": 0,
            "role": "assistant",
            "message": {
              "content": "",
              "tool_calls": [
                ...
              ]
            },
            "finish_reason": "tool_calls"
          }
        ]
      }
    },
    {
      "name": "k8s_get_resources_response",
      "match": {
        "match_type": "contains",
        "message" : {
          "content": "kagent-control-plane",
          "role": "tool",
          "tool_call_id": "call_1"
        }
      },
      "response": {
        "id": "call_1",
        "object": "chat.completion.tool_message",
        "created": 1677652288,
        "model": "gpt-4.1-mini",
        "choices": [
          ...
        ]
      }
    }
  ]
}
```

### Matching Algorithm
Simple linear search through mocks:
1. Parse incoming request into appropriate SDK type
2. Iterate through provider-specific mocks in order
3. For each mock, check if the match criteria are met:
   - **Exact**: JSON comparison of the last message
   - **Contains**: String contains check on message content (OpenAI only)
4. Return the response from the first matching mock
5. Return 404 if no match found

### Response Generation
- All responses are non-streaming JSON
- Uses official SDK response types directly
- No transformation or adaptation layer
- Standard HTTP headers (`Content-Type: application/json`)

### Files and Layout
Current implementation consists of:
- `server.go` — HTTP server setup, routing, and lifecycle management
- `types.go` — Core configuration types using official SDK types
- `openai.go` — OpenAI provider handler and matching logic
- `anthropic.go` — Anthropic provider handler and matching logic
- `server_test.go` — Basic integration tests

### Running in Tests
```go
config := mockllm.Config{/* mocks */}
server := mockllm.NewServer(config)
baseURL, err := server.Start() // Starts on random port
defer server.Stop()

// Use baseURL for API calls in tests
```

### SDK Dependencies
- **OpenAI Go SDK**: `github.com/openai/openai-go`
- **Anthropic Go SDK**: `github.com/anthropics/anthropic-sdk-go`
- **HTTP Router**: `github.com/gorilla/mux`

### Limitations of Current Implementation
1. **No Streaming**: Only supports non-streaming responses
2. **Simple Matching**: Only last message matching, no complex predicates
5. **No Multi-turn**: No stateful conversation tracking
6. **Limited Error Handling**: Basic error responses only
7. **No Latency Simulation**: No timing controls

### Potential Future Enhancements (Not Implemented)
The original design document outlined more sophisticated features that could be added:
- Streaming response support
- Complex matching predicates
- Error injection and latency simulation


