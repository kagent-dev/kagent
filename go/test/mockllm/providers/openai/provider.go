package openai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"

	"github.com/openai/openai-go"
)

// Provider handles OpenAI request/response mocking
type Provider struct {
	mocks []Mock
}

// Mock represents a single OpenAI request/response pair using SDK types
type Mock struct {
	Name     string
	Request  openai.ChatCompletionNewParams // OpenAI SDK request type
	Response openai.ChatCompletion          // openai.ChatCompletion or openai.ChatCompletionChunk
	Stream   bool
}

// NewProvider creates a new OpenAI provider with the given mocks
func NewProvider(mocks []Mock) *Provider {
	return &Provider{mocks: mocks}
}

// Handle processes an OpenAI chat completion request
func (p *Provider) Handle(w http.ResponseWriter, r *http.Request) {
	// Parse the incoming request into SDK type
	var requestBody openai.ChatCompletionNewParams
	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Find a matching mock
	mock := p.findMatchingMock(requestBody)
	if mock == nil {
		http.Error(w, "No matching mock found", http.StatusNotFound)
		return
	}

	// Return the response
	if mock.Stream {
		p.handleStreamingResponse(w, mock.Response)
	} else {
		p.handleNonStreamingResponse(w, mock.Response)
	}
}

// findMatchingMock finds the first mock that matches the request
func (p *Provider) findMatchingMock(request openai.ChatCompletionNewParams) *Mock {
	for _, mock := range p.mocks {
		if p.requestsMatch(mock.Request, request) {
			return &mock
		}
	}
	return nil
}

// requestsMatch checks if two requests are equivalent
func (p *Provider) requestsMatch(expected, actual openai.ChatCompletionNewParams) bool {
	// Simple deep equal comparison for now
	// In the future, we could add more sophisticated matching
	return reflect.DeepEqual(expected, actual)
}

// handleNonStreamingResponse sends a JSON response
func (p *Provider) handleNonStreamingResponse(w http.ResponseWriter, response interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
	}
}

// handleStreamingResponse sends an SSE stream response
func (p *Provider) handleStreamingResponse(w http.ResponseWriter, response interface{}) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// For now, just send the response as a single chunk
	// In the future, we could support proper streaming with multiple chunks
	responseBytes, err := json.Marshal(response)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal response: %v", err), http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "data: %s\n\n", responseBytes)
	fmt.Fprintf(w, "data: [DONE]\n\n")

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
