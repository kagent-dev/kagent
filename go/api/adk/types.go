// Package adk provides the generated ADK runtime contract and its semantic helpers.
package adk

import (
	"bytes"
	"encoding/json"
	"fmt"

	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

type Config = configv1.ADKConfig
type AgentConfig = configv1.ADKAgentConfig
type Model = configv1.ADKModelConfig
type OpenAI = configv1.ADKOpenAI
type AzureOpenAI = configv1.ADKAzureOpenAI
type Anthropic = configv1.ADKAnthropic
type GeminiVertexAI = configv1.ADKGeminiVertexAI
type GeminiAnthropic = configv1.ADKGeminiAnthropic
type Ollama = configv1.ADKOllama
type Gemini = configv1.ADKGemini
type Bedrock = configv1.ADKBedrock
type BedrockGuardrail = configv1.ADKBedrockGuardrail
type SAPAICore = configv1.ADKSAPAICore
type Foundry = configv1.ADKFoundry
type Mistral = configv1.ADKMistral
type Model_Openai = configv1.ADKModelConfig_Openai
type Model_AzureOpenai = configv1.ADKModelConfig_AzureOpenai
type Model_Anthropic = configv1.ADKModelConfig_Anthropic
type Model_GeminiVertexAi = configv1.ADKModelConfig_GeminiVertexAi
type Model_GeminiAnthropic = configv1.ADKModelConfig_GeminiAnthropic
type Model_Ollama = configv1.ADKModelConfig_Ollama
type Model_Gemini = configv1.ADKModelConfig_Gemini
type Model_Bedrock = configv1.ADKModelConfig_Bedrock
type Model_SapAiCore = configv1.ADKModelConfig_SapAiCore
type Model_Foundry = configv1.ADKModelConfig_Foundry
type Model_Mistral = configv1.ADKModelConfig_Mistral
type TokenExchangeConfig = configv1.ADKTokenExchangeConfig
type GDCHTokenExchangeConfig = configv1.ADKGDCHTokenExchangeConfig
type StreamableHTTPConnectionParams = configv1.ADKHTTPConnectionParams
type SseConnectionParams = configv1.ADKSSEConnectionParams
type HttpMcpServerConfig = configv1.ADKHTTPMCPServerConfig
type SseMcpServerConfig = configv1.ADKSSEMCPServerConfig
type StdioMcpServerConfig = configv1.ADKStdioMCPServerConfig
type RemoteAgentConfig = configv1.ADKRemoteAgentConfig
type EmbeddingConfig = configv1.ADKEmbeddingConfig
type MemoryConfig = configv1.ADKMemoryConfig
type NetworkConfig = configv1.ADKNetworkConfig
type AgentContextConfig = configv1.ADKContextConfig
type AgentCompressionConfig = configv1.ADKCompressionConfig
type OutputConfig = configv1.ADKOutputConfig
type RuntimeConfig = configv1.ADKRuntimeConfig
type TokenPropagationConfig = configv1.TokenPropagationConfig

const (
	ModelTypeOpenAI           = "openai"
	ModelTypeAzureOpenAI      = "azure_openai"
	ModelTypeAnthropic        = "anthropic"
	ModelTypeGeminiVertexAI   = "gemini_vertex_ai"
	ModelTypeGeminiAnthropic  = "gemini_anthropic"
	ModelTypeOllama           = "ollama"
	ModelTypeGemini           = "gemini"
	ModelTypeBedrock          = "bedrock"
	ModelTypeSAPAICore        = "sap_ai_core"
	ModelTypeFoundry          = "foundry"
	ModelTypeMistral          = "mistral"
	FoundryAPIFormatOpenAI    = "openai"
	FoundryAPIFormatAnthropic = "anthropic"
)

// ModelType is the selected provider's schema name.
func ModelType(m *Model) string {
	if m == nil {
		return ""
	}
	message := m.ProtoReflect()
	field := message.WhichOneof(message.Descriptor().Oneofs().ByName("provider"))
	if field == nil {
		return ""
	}
	return string(field.Name())
}

// Marshal validates compiled runtime input before publishing it to an actor.
func Marshal(cfg *Config) ([]byte, error) {
	if err := protovalidate.Validate(cfg); err != nil {
		return nil, fmt.Errorf("failed to validate ADK config: %w", err)
	}
	raw, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ADK config: %w", err)
	}
	// ProtoJSON permits variable whitespace. Revisions hash these bytes.
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, fmt.Errorf("failed to compact ADK config: %w", err)
	}
	return compact.Bytes(), nil
}

// Unmarshal accepts only the protobuf contract and leaves cfg intact on failure.
func Unmarshal(data []byte, cfg *Config) error {
	parsed := new(Config)
	if err := protojson.Unmarshal(data, parsed); err != nil {
		return fmt.Errorf("failed to parse ADK config: %w", err)
	}
	if err := protovalidate.Validate(parsed); err != nil {
		return fmt.Errorf("failed to validate ADK config: %w", err)
	}
	proto.Reset(cfg)
	proto.Merge(cfg, parsed)
	return nil
}

// ModelToEmbeddingConfig selects the model settings supported by memory providers.
func ModelToEmbeddingConfig(m *Model) *EmbeddingConfig {
	if m == nil {
		return nil
	}
	e := &EmbeddingConfig{Provider: ModelType(m), Model: m.Model, TlsInsecureSkipVerify: m.TlsInsecureSkipVerify, TlsCaCertPath: m.TlsCaCertPath, TlsDisableSystemCas: m.TlsDisableSystemCas}
	switch p := m.Provider.(type) {
	case *Model_Openai:
		e.BaseUrl = p.Openai.BaseUrl
		e.ApiKeyPassthrough = m.ApiKeyPassthrough
	case *Model_AzureOpenai:
		e.Endpoint = p.AzureOpenai.Endpoint
		e.Deployment = p.AzureOpenai.Deployment
		e.ApiVersion = p.AzureOpenai.ApiVersion
		e.ApiKeyPassthrough = m.ApiKeyPassthrough
	case *Model_Anthropic:
		e.BaseUrl = p.Anthropic.BaseUrl
	case *Model_Mistral:
		e.BaseUrl = p.Mistral.BaseUrl
	case *Model_SapAiCore:
		e.BaseUrl = p.SapAiCore.BaseUrl
	case *Model_Foundry:
		e.Endpoint = p.Foundry.Endpoint
		e.Deployment = p.Foundry.Deployment
		e.ApiVersion = p.Foundry.ApiVersion
		e.ApiKeyPassthrough = m.ApiKeyPassthrough
	}
	return e
}
