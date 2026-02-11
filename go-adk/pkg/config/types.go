package config

import (
	"encoding/json"
)

type Model interface {
	GetType() string
}

type BaseModel struct {
	Type                string            `json:"type"`
	Model               string            `json:"model"`
	Headers             map[string]string `json:"headers,omitempty"`
	TLSDisableVerify    *bool             `json:"tls_disable_verify,omitempty"`
	TLSCACertPath       *string           `json:"tls_ca_cert_path,omitempty"`
	TLSDisableSystemCAs *bool             `json:"tls_disable_system_cas,omitempty"`
}

const (
	ModelTypeOpenAI          = "openai"
	ModelTypeAzureOpenAI     = "azure_openai"
	ModelTypeAnthropic       = "anthropic"
	ModelTypeGeminiVertexAI  = "gemini_vertex_ai"
	ModelTypeGeminiAnthropic = "gemini_anthropic"
	ModelTypeOllama          = "ollama"
	ModelTypeGemini          = "gemini"
)

type OpenAI struct {
	BaseModel
	BaseUrl          string   `json:"base_url"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
	MaxTokens        *int     `json:"max_tokens,omitempty"`
	N                *int     `json:"n,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
	ReasoningEffort  *string  `json:"reasoning_effort,omitempty"`
	Seed             *int     `json:"seed,omitempty"`
	Temperature      *float64 `json:"temperature,omitempty"`
	Timeout          *int     `json:"timeout,omitempty"`
	TopP             *float64 `json:"top_p,omitempty"`
}

func (o *OpenAI) GetType() string { return ModelTypeOpenAI }

type AzureOpenAI struct {
	BaseModel
}

func (a *AzureOpenAI) GetType() string { return ModelTypeAzureOpenAI }

type Anthropic struct {
	BaseModel
	BaseUrl     string   `json:"base_url,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	TopK        *int     `json:"top_k,omitempty"`
	Timeout     *int     `json:"timeout,omitempty"`
}

func (a *Anthropic) GetType() string { return ModelTypeAnthropic }

type GeminiVertexAI struct {
	BaseModel
}

func (g *GeminiVertexAI) GetType() string { return ModelTypeGeminiVertexAI }

type GeminiAnthropic struct {
	BaseModel
}

func (g *GeminiAnthropic) GetType() string { return ModelTypeGeminiAnthropic }

type Ollama struct {
	BaseModel
}

func (o *Ollama) GetType() string { return ModelTypeOllama }

type Gemini struct {
	BaseModel
}

func (g *Gemini) GetType() string { return ModelTypeGemini }

type GenericModel struct {
	BaseModel
}

func (g *GenericModel) GetType() string { return g.Type }

// StreamableHTTPConnectionParams matches go/internal/adk.StreamableHTTPConnectionParams
type StreamableHTTPConnectionParams struct {
	Url              string            `json:"url"`
	Headers          map[string]string `json:"headers"`
	Timeout          *float64          `json:"timeout,omitempty"`
	SseReadTimeout   *float64          `json:"sse_read_timeout,omitempty"`
	TerminateOnClose *bool             `json:"terminate_on_close,omitempty"`
	TlsDisableVerify    *bool   `json:"tls_disable_verify,omitempty"`
	TlsCaCertPath       *string `json:"tls_ca_cert_path,omitempty"`
	TlsDisableSystemCas *bool   `json:"tls_disable_system_cas,omitempty"`
}

type HttpMcpServerConfig struct {
	Params StreamableHTTPConnectionParams `json:"params"`
	Tools  []string                       `json:"tools"`
}

type SseConnectionParams struct {
	Url            string            `json:"url"`
	Headers        map[string]string `json:"headers"`
	Timeout        *float64          `json:"timeout,omitempty"`
	SseReadTimeout *float64          `json:"sse_read_timeout,omitempty"`
	TlsDisableVerify    *bool   `json:"tls_disable_verify,omitempty"`
	TlsCaCertPath       *string `json:"tls_ca_cert_path,omitempty"`
	TlsDisableSystemCas *bool   `json:"tls_disable_system_cas,omitempty"`
}

type SseMcpServerConfig struct {
	Params SseConnectionParams `json:"params"`
	Tools  []string            `json:"tools"`
}

type RemoteAgentConfig struct {
	Name        string            `json:"name"`
	Url         string            `json:"url"`
	Headers     map[string]string `json:"headers,omitempty"`
	Description string            `json:"description,omitempty"`
}

type AgentConfig struct {
	Model        Model                 `json:"model"`
	Description  string                `json:"description"`
	Instruction  string                `json:"instruction"`
	HttpTools    []HttpMcpServerConfig `json:"http_tools,omitempty"`
	SseTools     []SseMcpServerConfig  `json:"sse_tools,omitempty"`
	RemoteAgents []RemoteAgentConfig   `json:"remote_agents,omitempty"`
	ExecuteCode  *bool                 `json:"execute_code,omitempty"`
	Stream       *bool                 `json:"stream,omitempty"`
}

func (a *AgentConfig) GetStream() bool {
	if a.Stream != nil {
		return *a.Stream
	}
	return false
}

func (a *AgentConfig) GetExecuteCode() bool {
	if a.ExecuteCode != nil {
		return *a.ExecuteCode
	}
	return false
}

func (a *AgentConfig) UnmarshalJSON(data []byte) error {
	var tmp struct {
		Model        json.RawMessage       `json:"model"`
		Description  string                `json:"description"`
		Instruction  string                `json:"instruction"`
		HttpTools    []HttpMcpServerConfig `json:"http_tools,omitempty"`
		SseTools     []SseMcpServerConfig  `json:"sse_tools,omitempty"`
		RemoteAgents []RemoteAgentConfig   `json:"remote_agents,omitempty"`
		ExecuteCode  *bool                 `json:"execute_code,omitempty"`
		Stream       *bool                 `json:"stream,omitempty"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	var base BaseModel
	if err := json.Unmarshal(tmp.Model, &base); err != nil {
		return err
	}

	switch base.Type {
	case ModelTypeOpenAI:
		var m OpenAI
		if err := json.Unmarshal(tmp.Model, &m); err != nil {
			return err
		}
		a.Model = &m
	case ModelTypeAzureOpenAI:
		var m AzureOpenAI
		if err := json.Unmarshal(tmp.Model, &m); err != nil {
			return err
		}
		a.Model = &m
	case ModelTypeAnthropic:
		var m Anthropic
		if err := json.Unmarshal(tmp.Model, &m); err != nil {
			return err
		}
		a.Model = &m
	case ModelTypeGeminiVertexAI:
		var m GeminiVertexAI
		if err := json.Unmarshal(tmp.Model, &m); err != nil {
			return err
		}
		a.Model = &m
	case ModelTypeGeminiAnthropic:
		var m GeminiAnthropic
		if err := json.Unmarshal(tmp.Model, &m); err != nil {
			return err
		}
		a.Model = &m
	case ModelTypeGemini:
		var m Gemini
		if err := json.Unmarshal(tmp.Model, &m); err != nil {
			return err
		}
		a.Model = &m
	case ModelTypeOllama:
		var m Ollama
		if err := json.Unmarshal(tmp.Model, &m); err != nil {
			return err
		}
		a.Model = &m
	default:
		var m GenericModel
		if err := json.Unmarshal(tmp.Model, &m); err != nil {
			return err
		}
		a.Model = &m
	}

	a.Description = tmp.Description
	a.Instruction = tmp.Instruction
	a.HttpTools = tmp.HttpTools
	if a.HttpTools == nil {
		a.HttpTools = []HttpMcpServerConfig{}
	}
	a.SseTools = tmp.SseTools
	if a.SseTools == nil {
		a.SseTools = []SseMcpServerConfig{}
	}
	a.RemoteAgents = tmp.RemoteAgents
	if a.RemoteAgents == nil {
		a.RemoteAgents = []RemoteAgentConfig{}
	}
	a.ExecuteCode = tmp.ExecuteCode
	a.Stream = tmp.Stream
	return nil
}
