// Package adk provides the generated ADK runtime contract and its semantic helpers.
package adk

import (
	"bytes"
	"encoding/json"
	"fmt"

	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

type Config = apiv1.ADKConfig
type AgentConfig = apiv1.ADKAgentConfig
type Model = apiv1.ADKModelConfig
type OpenAI = apiv1.ADKOpenAI
type AzureOpenAI = apiv1.ADKAzureOpenAI
type Anthropic = apiv1.ADKAnthropic
type GeminiVertexAI = apiv1.ADKGeminiVertexAI
type GeminiAnthropic = apiv1.ADKGeminiAnthropic
type Ollama = apiv1.ADKOllama
type Gemini = apiv1.ADKGemini
type Bedrock = apiv1.ADKBedrock
type BedrockGuardrail = apiv1.ADKBedrockGuardrail
type SAPAICore = apiv1.ADKSAPAICore
type Foundry = apiv1.ADKFoundry
type Mistral = apiv1.ADKMistral
type Model_Openai = apiv1.ADKModelConfig_Openai
type Model_AzureOpenai = apiv1.ADKModelConfig_AzureOpenai
type Model_Anthropic = apiv1.ADKModelConfig_Anthropic
type Model_GeminiVertexAi = apiv1.ADKModelConfig_GeminiVertexAi
type Model_GeminiAnthropic = apiv1.ADKModelConfig_GeminiAnthropic
type Model_Ollama = apiv1.ADKModelConfig_Ollama
type Model_Gemini = apiv1.ADKModelConfig_Gemini
type Model_Bedrock = apiv1.ADKModelConfig_Bedrock
type Model_SapAiCore = apiv1.ADKModelConfig_SapAiCore
type Model_Foundry = apiv1.ADKModelConfig_Foundry
type Model_Mistral = apiv1.ADKModelConfig_Mistral
type TokenExchangeConfig = apiv1.ADKTokenExchangeConfig
type GDCHTokenExchangeConfig = apiv1.ADKGDCHTokenExchangeConfig
type StreamableHTTPConnectionParams = apiv1.ADKHTTPConnectionParams
type SseConnectionParams = apiv1.ADKSSEConnectionParams
type HttpMcpServerConfig = apiv1.ADKHTTPMCPServerConfig
type SseMcpServerConfig = apiv1.ADKSSEMCPServerConfig
type StdioMcpServerConfig = apiv1.ADKStdioMCPServerConfig
type RemoteAgentConfig = apiv1.ADKRemoteAgentConfig
type EmbeddingConfig = apiv1.ADKEmbeddingConfig
type MemoryConfig = apiv1.ADKMemoryConfig
type NetworkConfig = apiv1.ADKNetworkConfig
type AgentContextConfig = apiv1.ADKContextConfig
type AgentCompressionConfig = apiv1.ADKCompressionConfig
type OutputConfig = apiv1.ADKOutputConfig
type RuntimeConfig = apiv1.ADKRuntimeConfig
type TokenPropagationConfig = apiv1.TokenPropagationConfig

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
