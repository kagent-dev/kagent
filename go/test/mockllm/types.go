package mockllm

import (
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go"
)

// Very simple mock configuration - just maps requests to responses using official SDK types

// Config holds all the mock responses
type Config struct {
	OpenAI    []OpenAIMock    `json:"openai,omitempty"`
	Anthropic []AnthropicMock `json:"anthropic,omitempty"`
}

// OpenAIMock maps an OpenAI request to a response using official SDK types
type OpenAIMock struct {
	Name     string                         `json:"name"`             // identifier for this mock
	Request  openai.ChatCompletionNewParams `json:"request"`          // OpenAI request body to match
	Response openai.ChatCompletion          `json:"response"`         // OpenAI response to return (ChatCompletion or ChatCompletionChunk)
	Stream   bool                           `json:"stream,omitempty"` // whether this is a streaming response
}

// AnthropicMock maps an Anthropic request to a response using official SDK types
type AnthropicMock struct {
	Name     string                     `json:"name"`             // identifier for this mock
	Request  anthropic.MessageNewParams `json:"request"`          // Anthropic request body to match
	Response anthropic.Message          `json:"response"`         // Anthropic response to return (Message or streaming event)
	Stream   bool                       `json:"stream,omitempty"` // whether this is a streaming response
}
