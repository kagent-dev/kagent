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
	ModelTypeXAI             = "xai"
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
	return json.Marshal(map[string]interface{}{
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
	return json.Marshal(map[string]interface{}{
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
	return json.Marshal(map[string]interface{}{
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
	return json.Marshal(map[string]interface{}{
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
	return json.Marshal(map[string]interface{}{
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
	return json.Marshal(map[string]interface{}{
		"type":    ModelTypeGemini,
		"model":   g.Model,
		"headers": g.Headers,
	})
}

func (g *Gemini) GetType() string {
	return ModelTypeGemini
}

// XAI uses OpenAI-compatible API but with a different default baseURL and server-side tools support
type XAI struct {
	OpenAI
	// Server-side tools (e.g., web_search, x_search, code_execution, collections_search)
	Tools []string `json:"tools,omitempty"`
	// Live search mode for real-time data retrieval ("off", "auto", "on")
	LiveSearchMode string `json:"live_search_mode,omitempty"`
}

func (x *XAI) MarshalJSON() ([]byte, error) {
	// Create a map to ensure we override the embedded BaseModel.Type
	result := make(map[string]interface{})

	// Marshal the embedded OpenAI to get all its fields
	openaiBytes, err := json.Marshal(x.OpenAI)
	if err != nil {
		return nil, err
	}
	var openaiMap map[string]interface{}
	if err := json.Unmarshal(openaiBytes, &openaiMap); err != nil {
		return nil, err
	}

	// Copy OpenAI fields
	for k, v := range openaiMap {
		result[k] = v
	}

	// Override type to xai (this ensures BaseModel.Type is overridden)
	result["type"] = ModelTypeXAI

	// Add XAI-specific fields
	if len(x.Tools) > 0 {
		result["tools"] = x.Tools
	}
	if x.LiveSearchMode != "" {
		result["live_search_mode"] = x.LiveSearchMode
	}

	return json.Marshal(result)
}

func (x *XAI) GetType() string {
	return ModelTypeXAI
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
	case ModelTypeXAI:
		var xai XAI
		if err := json.Unmarshal(bytes, &xai); err != nil {
			return nil, err
		}
		return &xai, nil
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

func (a *AgentConfig) Scan(value interface{}) error {
	return json.Unmarshal(value.([]byte), a)
}

var _ driver.Valuer = &AgentConfig{}

func (a AgentConfig) Value() (driver.Value, error) {
	return json.Marshal(a)
}

// MarshalJSON ensures the Model interface is properly marshaled using its concrete type's MarshalJSON
func (a AgentConfig) MarshalJSON() ([]byte, error) {
	type Alias AgentConfig
	return json.Marshal(&struct {
		Model json.RawMessage `json:"model"`
		*Alias
	}{
		Model: func() json.RawMessage {
			if a.Model == nil {
				return nil
			}
			b, err := json.Marshal(a.Model)
			if err != nil {
				return nil
			}
			return b
		}(),
		Alias: (*Alias)(&a),
	})
}
