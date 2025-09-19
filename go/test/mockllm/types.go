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

type MatchType string

const (
	MatchTypeExact    MatchType = "exact"
	MatchTypeContains MatchType = "contains"
)

type OpenAIRequestMatch struct {
	MatchType MatchType                              `json:"match_type"`
	Message   openai.ChatCompletionMessageParamUnion `json:"message"`
}

// OpenAIMock maps an OpenAI request to a response using official SDK types
type OpenAIMock struct {
	Name     string                `json:"name"`     // identifier for this mock
	Match    OpenAIRequestMatch    `json:"match"`    // Match type and value
	Response openai.ChatCompletion `json:"response"` // OpenAI response to return (ChatCompletion or ChatCompletionChunk)
}

type AnthropicRequestMatch struct {
	MatchType MatchType              `json:"match_type"`
	Message   anthropic.MessageParam `json:"message"`
}

// AnthropicMock maps an Anthropic request to a response using official SDK types
type AnthropicMock struct {
	Name     string                `json:"name"`     // identifier for this mock
	Match    AnthropicRequestMatch `json:"match"`    // Match type and value
	Response anthropic.Message     `json:"response"` // Anthropic response to return (Message or streaming event)
}
