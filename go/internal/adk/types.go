package adk

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

type StreamableHTTPConnectionParams struct {
	Url              string            `json:"url"`
	Headers          map[string]string `json:"headers"`
	Timeout          *float64          `json:"timeout,omitempty"`
	SseReadTimeout   *float64          `json:"sse_read_timeout,omitempty"`
	TerminateOnClose *bool             `json:"terminate_on_close,omitempty"`
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
}

type SseMcpServerConfig struct {
	Params SseConnectionParams `json:"params"`
	Tools  []string            `json:"tools"`
}

type Model interface {
	GetType() string
}

type BaseModel struct {
	Type    string            `json:"type"`
	Model   string            `json:"model"`
	Headers map[string]string `json:"headers,omitempty"`

	// TLS/SSL configuration (applies to all model types)
	TLSDisableVerify    *bool   `json:"tls_disable_verify,omitempty"`
	TLSCACertPath       *string `json:"tls_ca_cert_path,omitempty"`
	TLSDisableSystemCAs *bool   `json:"tls_disable_system_cas,omitempty"`
}

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

const (
	ModelTypeOpenAI          = "openai"
	ModelTypeAzureOpenAI     = "azure_openai"
	ModelTypeAnthropic       = "anthropic"
	ModelTypeGeminiVertexAI  = "gemini_vertex_ai"
	ModelTypeGeminiAnthropic = "gemini_anthropic"
	ModelTypeOllama          = "ollama"
	ModelTypeGemini          = "gemini"
	ModelTypeBedrock         = "bedrock"
)

func (o *OpenAI) MarshalJSON() ([]byte, error) {
	type Alias OpenAI

	return json.Marshal(&struct {
		Type string `json:"type"`
		*Alias
	}{
		Type:  ModelTypeOpenAI,
		Alias: (*Alias)(o),
	})
}

func (o *OpenAI) GetType() string {
	return ModelTypeOpenAI
}

type AzureOpenAI struct {
	BaseModel
}

func (a *AzureOpenAI) GetType() string {
	return ModelTypeAzureOpenAI
}

func (a *AzureOpenAI) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":    ModelTypeAzureOpenAI,
		"model":   a.Model,
		"headers": a.Headers,
	})
}

type Anthropic struct {
	BaseModel
	BaseUrl string `json:"base_url"`
}

func (a *Anthropic) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":     ModelTypeAnthropic,
		"model":    a.Model,
		"base_url": a.BaseUrl,
		"headers":  a.Headers,
	})
}

func (a *Anthropic) GetType() string {
	return ModelTypeAnthropic
}

type GeminiVertexAI struct {
	BaseModel
}

func (g *GeminiVertexAI) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":    ModelTypeGeminiVertexAI,
		"model":   g.Model,
		"headers": g.Headers,
	})
}

func (g *GeminiVertexAI) GetType() string {
	return ModelTypeGeminiVertexAI
}

type GeminiAnthropic struct {
	BaseModel
}

func (g *GeminiAnthropic) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":    ModelTypeGeminiAnthropic,
		"model":   g.Model,
		"headers": g.Headers,
	})
}

func (g *GeminiAnthropic) GetType() string {
	return ModelTypeGeminiAnthropic
}

type Ollama struct {
	BaseModel
}

func (o *Ollama) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":    ModelTypeOllama,
		"model":   o.Model,
		"headers": o.Headers,
	})
}

func (o *Ollama) GetType() string {
	return ModelTypeOllama
}

type Gemini struct {
	BaseModel
}

func (g *Gemini) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":    ModelTypeGemini,
		"model":   g.Model,
		"headers": g.Headers,
	})
}

func (g *Gemini) GetType() string {
	return ModelTypeGemini
}

type Bedrock struct {
	BaseModel
	// Region is the AWS region where the model is available
	Region string `json:"region,omitempty"`
}

func (b *Bedrock) MarshalJSON() ([]byte, error) {
	data := map[string]any{
		"type":    ModelTypeBedrock,
		"model":   b.Model,
		"headers": b.Headers,
	}
	if b.Region != "" {
		data["region"] = b.Region
	}
	return json.Marshal(data)
}

func (b *Bedrock) GetType() string {
	return ModelTypeBedrock
}

func ParseModel(bytes []byte) (Model, error) {
	var model BaseModel
	if err := json.Unmarshal(bytes, &model); err != nil {
		return nil, err
	}
	switch model.Type {
	case ModelTypeGemini:
		var gemini Gemini
		if err := json.Unmarshal(bytes, &gemini); err != nil {
			return nil, err
		}
		return &gemini, nil
	case ModelTypeAzureOpenAI:
		var azureOpenAI AzureOpenAI
		if err := json.Unmarshal(bytes, &azureOpenAI); err != nil {
			return nil, err
		}
		return &azureOpenAI, nil
	case ModelTypeOpenAI:
		var openai OpenAI
		if err := json.Unmarshal(bytes, &openai); err != nil {
			return nil, err
		}
		return &openai, nil
	case ModelTypeAnthropic:
		var anthropic Anthropic
		if err := json.Unmarshal(bytes, &anthropic); err != nil {
			return nil, err
		}
		return &anthropic, nil
	case ModelTypeGeminiVertexAI:
		var geminiVertexAI GeminiVertexAI
		if err := json.Unmarshal(bytes, &geminiVertexAI); err != nil {
			return nil, err
		}
		return &geminiVertexAI, nil
	case ModelTypeGeminiAnthropic:
		var geminiAnthropic GeminiAnthropic
		if err := json.Unmarshal(bytes, &geminiAnthropic); err != nil {
			return nil, err
		}
		return &geminiAnthropic, nil
	case ModelTypeOllama:
		var ollama Ollama
		if err := json.Unmarshal(bytes, &ollama); err != nil {
			return nil, err
		}
		return &ollama, nil
	case ModelTypeBedrock:
		var bedrock Bedrock
		if err := json.Unmarshal(bytes, &bedrock); err != nil {
			return nil, err
		}
		return &bedrock, nil
	}
	return nil, fmt.Errorf("unknown model type: %s", model.Type)
}

type RemoteAgentConfig struct {
	Name        string            `json:"name"`
	Url         string            `json:"url"`
	Headers     map[string]string `json:"headers,omitempty"`
	Description string            `json:"description,omitempty"`
}

// See `python/packages/kagent-adk/src/kagent/adk/types.py` for the python version of this
type AgentConfig struct {
	Model        Model                 `json:"model"`
	Description  string                `json:"description"`
	Instruction  string                `json:"instruction"`
	HttpTools    []HttpMcpServerConfig `json:"http_tools"`
	SseTools     []SseMcpServerConfig  `json:"sse_tools"`
	RemoteAgents []RemoteAgentConfig   `json:"remote_agents"`
	ExecuteCode  bool                  `json:"execute_code,omitempty"`
	Stream       bool                  `json:"stream"`
}

func (a *AgentConfig) UnmarshalJSON(data []byte) error {
	var tmp struct {
		Model        json.RawMessage       `json:"model"`
		Description  string                `json:"description"`
		Instruction  string                `json:"instruction"`
		HttpTools    []HttpMcpServerConfig `json:"http_tools"`
		SseTools     []SseMcpServerConfig  `json:"sse_tools"`
		RemoteAgents []RemoteAgentConfig   `json:"remote_agents"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	model, err := ParseModel(tmp.Model)
	if err != nil {
		return err
	}
	a.Model = model
	a.Description = tmp.Description
	a.Instruction = tmp.Instruction
	a.HttpTools = tmp.HttpTools
	a.SseTools = tmp.SseTools
	a.RemoteAgents = tmp.RemoteAgents
	return nil
}

var _ sql.Scanner = &AgentConfig{}

func (a *AgentConfig) Scan(value any) error {
	return json.Unmarshal(value.([]byte), a)
}

var _ driver.Valuer = &AgentConfig{}

func (a AgentConfig) Value() (driver.Value, error) {
	return json.Marshal(a)
}
