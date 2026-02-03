package models

import (
	"context"
	"time"
)

// LLMRequest represents a request to an LLM
type LLMRequest struct {
	Model             string
	Contents          []Content
	Config            *LLMRequestConfig
	SystemInstruction interface{} // Can be string or Content
}

// LLMRequestConfig holds configuration for LLM requests
type LLMRequestConfig struct {
	SystemInstruction interface{}
	Tools             []Tool
}

// Content represents a message content with role and parts
type Content struct {
	Role  string
	Parts []Part
}

// Part represents a content part (text, function call, etc.)
type Part struct {
	Text             *string
	FunctionCall     *FunctionCall
	FunctionResponse *FunctionResponse
	InlineData       *InlineData
	FileData         *FileData
}

// FunctionCall represents a function call
type FunctionCall struct {
	ID   string
	Name string
	Args map[string]interface{}
}

// FunctionResponse represents a function response
type FunctionResponse struct {
	ID       string
	Name     string
	Response interface{}
}

// InlineData represents inline data (e.g., images)
type InlineData struct {
	MimeType string
	Data     []byte
}

// FileData represents file data with URI
type FileData struct {
	FileURI  string
	MimeType string
}

// Tool represents a tool/function definition
type Tool struct {
	FunctionDeclarations []FunctionDeclaration
}

// FunctionDeclaration represents a function declaration
type FunctionDeclaration struct {
	Name        string
	Description string
	Parameters  map[string]interface{} // JSON schema
}

// LLMResponse represents a response from an LLM
type LLMResponse struct {
	Content       *Content
	Partial       bool
	TurnComplete  bool
	FinishReason  string
	UsageMetadata *UsageMetadata
	ErrorCode     string
	ErrorMessage  string
}

// UsageMetadata represents token usage information
type UsageMetadata struct {
	PromptTokenCount     int
	CandidatesTokenCount int
	TotalTokenCount      int
}

// BaseLLM is the interface that all LLM implementations must satisfy
type BaseLLM interface {
	// GenerateContent generates content from an LLM request
	// Returns a channel of LLMResponse events
	GenerateContent(ctx context.Context, request *LLMRequest, stream bool) (<-chan *LLMResponse, error)
}

// FinishReason constants
const (
	FinishReasonStop       = "STOP"
	FinishReasonMaxTokens  = "MAX_TOKENS"
	FinishReasonSafety     = "SAFETY"
	FinishReasonToolCalls  = "TOOL_CALLS"
	FinishReasonRecitation = "RECITATION"
)

// Default execution timeout (30 minutes)
// This is used as the default HTTP client timeout when no timeout is specified
const DefaultExecutionTimeout = 30 * time.Minute
