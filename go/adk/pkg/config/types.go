package config

// This package re-exports types from github.com/kagent-dev/kagent/go/api/adk
// to maintain backward compatibility for existing go-adk consumers.
// All canonical type definitions live in go/api/adk.

import (
	"github.com/kagent-dev/kagent/go/api/adk"
)

// Re-exported types from go/api/adk
type (
	Model                          = adk.Model
	BaseModel                      = adk.BaseModel
	OpenAI                         = adk.OpenAI
	AzureOpenAI                    = adk.AzureOpenAI
	Anthropic                      = adk.Anthropic
	GeminiVertexAI                 = adk.GeminiVertexAI
	GeminiAnthropic                = adk.GeminiAnthropic
	Ollama                         = adk.Ollama
	Gemini                         = adk.Gemini
	Bedrock                        = adk.Bedrock
	GenericModel                   = adk.GenericModel
	StreamableHTTPConnectionParams = adk.StreamableHTTPConnectionParams
	HttpMcpServerConfig            = adk.HttpMcpServerConfig
	SseConnectionParams            = adk.SseConnectionParams
	SseMcpServerConfig             = adk.SseMcpServerConfig
	RemoteAgentConfig              = adk.RemoteAgentConfig
	AgentConfig                    = adk.AgentConfig
)

// Re-exported constants from go/api/adk
const (
	ModelTypeOpenAI          = adk.ModelTypeOpenAI
	ModelTypeAzureOpenAI     = adk.ModelTypeAzureOpenAI
	ModelTypeAnthropic       = adk.ModelTypeAnthropic
	ModelTypeGeminiVertexAI  = adk.ModelTypeGeminiVertexAI
	ModelTypeGeminiAnthropic = adk.ModelTypeGeminiAnthropic
	ModelTypeOllama          = adk.ModelTypeOllama
	ModelTypeGemini          = adk.ModelTypeGemini
	ModelTypeBedrock         = adk.ModelTypeBedrock
)

// Re-exported functions from go/api/adk
var ParseModel = adk.ParseModel
