package adk

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"trpc.group/trpc-go/trpc-a2a-go/server"
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
	Type  string `json:"type"`
	Model string `json:"model"`
}

type OpenAI struct {
	BaseModel
	BaseUrl string `json:"base_url"`
}

func (o *OpenAI) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"type":     "openai",
		"model":    o.Model,
		"base_url": o.BaseUrl,
	})
}

func (o *OpenAI) GetType() string {
	return "openai"
}

type AzureOpenAI struct {
	BaseModel
}

func (a *AzureOpenAI) GetType() string {
	return "azure_openai"
}

func (a *AzureOpenAI) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"type":  "azure_openai",
		"model": a.Model,
	})
}

type Anthropic struct {
	BaseModel
	BaseUrl string `json:"base_url"`
}

func (a *Anthropic) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"type":     "anthropic",
		"model":    a.Model,
		"base_url": a.BaseUrl,
	})
}

func (a *Anthropic) GetType() string {
	return "anthropic"
}

type GeminiVertexAI struct {
	BaseModel
}

func (g *GeminiVertexAI) MarshalJSON() ([]byte, error) {

	return json.Marshal(map[string]interface{}{
		"type":  "gemini_vertex_ai",
		"model": g.Model,
	})
}

func (g *GeminiVertexAI) GetType() string {
	return "gemini_vertex_ai"
}

type GeminiAnthropic struct {
	BaseModel
}

func (g *GeminiAnthropic) MarshalJSON() ([]byte, error) {

	return json.Marshal(map[string]interface{}{
		"type":  "gemini_anthropic",
		"model": g.Model,
	})
}

func (g *GeminiAnthropic) GetType() string {
	return "gemini_anthropic"
}

type Ollama struct {
	BaseModel
}

func (o *Ollama) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"type":  "ollama",
		"model": o.Model,
	})
}

func (o *Ollama) GetType() string {
	return "ollama"
}

type Gemini struct {
	BaseModel
}

func (g *Gemini) MarshalJSON() ([]byte, error) {

	return json.Marshal(map[string]interface{}{
		"type":  "gemini",
		"model": g.Model,
	})
}

func (g *Gemini) GetType() string {
	return "gemini"
}

func ParseModel(bytes []byte) (Model, error) {
	var model BaseModel
	if err := json.Unmarshal(bytes, &model); err != nil {
		return nil, err
	}
	switch model.Type {
	case "openai":
		var openai OpenAI
		if err := json.Unmarshal(bytes, &openai); err != nil {
			return nil, err
		}
		return &openai, nil
	case "anthropic":
		var anthropic Anthropic
		if err := json.Unmarshal(bytes, &anthropic); err != nil {
			return nil, err
		}
		return &anthropic, nil
	case "gemini_vertex_ai":
		var geminiVertexAI GeminiVertexAI
		if err := json.Unmarshal(bytes, &geminiVertexAI); err != nil {
			return nil, err
		}
		return &geminiVertexAI, nil
	case "gemini_anthropic":
		var geminiAnthropic GeminiAnthropic
		if err := json.Unmarshal(bytes, &geminiAnthropic); err != nil {
			return nil, err
		}
		return &geminiAnthropic, nil
	case "ollama":
		var ollama Ollama
		if err := json.Unmarshal(bytes, &ollama); err != nil {
			return nil, err
		}
		return &ollama, nil
	case "gemini":
		var gemini Gemini
		if err := json.Unmarshal(bytes, &gemini); err != nil {
			return nil, err
		}
		return &gemini, nil
	}
	return nil, fmt.Errorf("unknown model type: %s", model.Type)
}

type AgentConfig struct {
	KagentUrl   string                `json:"kagent_url"`
	AgentCard   server.AgentCard      `json:"agent_card"`
	Name        string                `json:"name"`
	Model       Model                 `json:"model"`
	Description string                `json:"description"`
	Instruction string                `json:"instruction"`
	HttpTools   []HttpMcpServerConfig `json:"http_tools"`
	SseTools    []SseMcpServerConfig  `json:"sse_tools"`
	Agents      []AgentConfig         `json:"agents"`
}

func (a *AgentConfig) UnmarshalJSON(data []byte) error {
	var tmp struct {
		KagentUrl   string                `json:"kagent_url"`
		AgentCard   server.AgentCard      `json:"agent_card"`
		Name        string                `json:"name"`
		Model       json.RawMessage       `json:"model"`
		Description string                `json:"description"`
		Instruction string                `json:"instruction"`
		HttpTools   []HttpMcpServerConfig `json:"http_tools"`
		SseTools    []SseMcpServerConfig  `json:"sse_tools"`
		Agents      []AgentConfig         `json:"agents"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	a.KagentUrl = tmp.KagentUrl
	a.AgentCard = tmp.AgentCard
	a.Name = tmp.Name
	model, err := ParseModel(tmp.Model)
	if err != nil {
		return err
	}
	a.Model = model
	a.Description = tmp.Description
	a.Instruction = tmp.Instruction
	a.HttpTools = tmp.HttpTools
	a.SseTools = tmp.SseTools
	a.Agents = tmp.Agents
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
