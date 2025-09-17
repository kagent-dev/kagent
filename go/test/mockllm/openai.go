package mockllm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/openai/openai-go"
)

// Provider handles OpenAI request/response mocking
type OpenAIProvider struct {
	mocks []OpenAIMock
}

// NewOpenAIProvider creates a new OpenAI OpenAIProvider with the given mocks
func NewOpenAIProvider(mocks []OpenAIMock) *OpenAIProvider {
	return &OpenAIProvider{mocks: mocks}
}

// Handle processes an OpenAI chat completion request
func (p *OpenAIProvider) Handle(w http.ResponseWriter, r *http.Request) {
	// Parse the incoming request into SDK type
	var requestBody openai.ChatCompletionNewParams
	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Find a matching mock
	mock := p.findMatchingMock(requestBody)
	if mock == nil {
		http.Error(w, "No matching mock found.", http.StatusNotFound)
		return
	}

	// Return the response
	p.handleNonStreamingResponse(w, mock.Response)
}

// findMatchingMock finds the first mock that matches the request
func (p *OpenAIProvider) findMatchingMock(request openai.ChatCompletionNewParams) *OpenAIMock {
	for _, mock := range p.mocks {
		if p.requestsMatch(mock.Match, request) {
			return &mock
		}
	}
	return nil
}

// requestsMatch checks if two requests are equivalent
func (p *OpenAIProvider) requestsMatch(expected OpenAIRequestMatch, actual openai.ChatCompletionNewParams) bool {
	// Simple deep equal comparison for now
	// In the future, we could add more sophisticated matching
	switch expected.MatchType {
	case MatchTypeExact:
		// get Last message from actual
		if len(actual.Messages) == 0 {
			return false
		}
		lastMessage := actual.Messages[len(actual.Messages)-1]
		// Check json is equal
		jsonExpected, err := json.Marshal(expected.Message)
		if err != nil {
			return false
		}
		jsonActual, err := json.Marshal(lastMessage)
		if err != nil {
			return false
		}
		return bytes.Equal(jsonExpected, jsonActual)
	case MatchTypeContains:
		// Check if the last message contains the expected message
		if len(actual.Messages) == 0 {
			return false
		}
		lastMessage := actual.Messages[len(actual.Messages)-1]
		if *lastMessage.GetRole() != *expected.Message.GetRole() {
			return false
		}
		strExpected, ok := expected.Message.GetContent().AsAny().(*string)
		if !ok {
			return false
		}
		strActual, ok := lastMessage.GetContent().AsAny().(*string)
		if !ok {
			return false
		}
		return strings.Contains(*strActual, *strExpected)
	default:
		return false
	}
}

// handleNonStreamingResponse sends a JSON response
func (p *OpenAIProvider) handleNonStreamingResponse(w http.ResponseWriter, response interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
	}
}
