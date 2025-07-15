package client

import (
	"encoding/json"
	"fmt"
)

type ModelsUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func (m *ModelsUsage) Add(other *ModelsUsage) {
	if other == nil {
		return
	}
	m.PromptTokens += other.PromptTokens
	m.CompletionTokens += other.CompletionTokens
}

func (m *ModelsUsage) String() string {
	return fmt.Sprintf("Prompt Tokens: %d, Completion Tokens: %d", m.PromptTokens, m.CompletionTokens)
}

func (m *ModelsUsage) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"prompt_tokens":     m.PromptTokens,
		"completion_tokens": m.CompletionTokens,
	}
}

type Event interface {
	GetType() string
}

type BaseEvent struct {
	Type string `json:"type"`
}

func (e *BaseEvent) GetType() string {
	return e.Type
}

type BaseChatMessage struct {
	BaseEvent   `json:",inline"`
	Source      string            `json:"source"`
	Metadata    map[string]string `json:"metadata"`
	ModelsUsage *ModelsUsage      `json:"models_usage"`
}

func newBaseChatMessage(source string, eventType string) BaseChatMessage {
	return BaseChatMessage{
		BaseEvent:   BaseEvent{Type: eventType},
		Source:      source,
		Metadata:    make(map[string]string),
		ModelsUsage: &ModelsUsage{},
	}
}

type TextMessage struct {
	BaseChatMessage `json:",inline"`
	Content         string `json:"content"`
}

func NewTextMessage(content, source string) *TextMessage {
	return &TextMessage{
		BaseChatMessage: newBaseChatMessage(source, TextMessageLabel),
		Content:         content,
	}
}

type ModelClientStreamingChunkEvent struct {
	BaseChatMessage `json:",inline"`
	Content         string `json:"content"`
}

type FunctionCall struct {
	ID        string `json:"id"`
	Arguments string `json:"arguments"`
	Name      string `json:"name"`
}

type ToolCallRequestEvent struct {
	BaseChatMessage `json:",inline"`
	Content         []FunctionCall `json:"content"`
}

type FunctionExecutionResult struct {
	Name    string `json:"name"`
	CallID  string `json:"call_id"`
	Content string `json:"content"`
}

type ToolCallExecutionEvent struct {
	BaseChatMessage `json:",inline"`
	Content         []FunctionExecutionResult `json:"content"`
}

type MemoryQueryEvent struct {
	BaseChatMessage `json:",inline"`
	Content         []map[string]interface{} `json:"content"`
}

type ToolCallSummaryMessage struct {
	BaseChatMessage `json:",inline"`
	Content         string                    `json:"content"`
	ToolCalls       []FunctionCall            `json:"tool_calls"`
	Results         []FunctionExecutionResult `json:"results"`
}

const (
	TextMessageLabel                    = "TextMessage"
	ToolCallRequestEventLabel           = "ToolCallRequestEvent"
	ToolCallExecutionEventLabel         = "ToolCallExecutionEvent"
	StopMessageLabel                    = "StopMessage"
	ModelClientStreamingChunkEventLabel = "ModelClientStreamingChunkEvent"
	LLMCallEventMessageLabel            = "LLMCallEventMessage"
	MemoryQueryEventLabel               = "MemoryQueryEvent"
	ToolCallSummaryMessageLabel         = "ToolCallSummaryMessage"
)

func ParseEvent(event []byte) (Event, error) {
	var baseEvent BaseEvent
	if err := json.Unmarshal(event, &baseEvent); err != nil {
		return nil, err
	}

	switch baseEvent.Type {
	case TextMessageLabel:
		var textMessage TextMessage
		if err := json.Unmarshal(event, &textMessage); err != nil {
			return nil, err
		}
		return &textMessage, nil
	case ModelClientStreamingChunkEventLabel:
		var modelClientStreamingChunkEvent ModelClientStreamingChunkEvent
		if err := json.Unmarshal(event, &modelClientStreamingChunkEvent); err != nil {
			return nil, err
		}
		return &modelClientStreamingChunkEvent, nil
	case ToolCallRequestEventLabel:
		var toolCallRequestEvent ToolCallRequestEvent
		if err := json.Unmarshal(event, &toolCallRequestEvent); err != nil {
			return nil, err
		}
		return &toolCallRequestEvent, nil
	case ToolCallExecutionEventLabel:
		var toolCallExecutionEvent ToolCallExecutionEvent
		if err := json.Unmarshal(event, &toolCallExecutionEvent); err != nil {
			return nil, err
		}
		return &toolCallExecutionEvent, nil
	case MemoryQueryEventLabel:
		var memoryQueryEvent MemoryQueryEvent
		if err := json.Unmarshal(event, &memoryQueryEvent); err != nil {
			return nil, err
		}
		return &memoryQueryEvent, nil
	case ToolCallSummaryMessageLabel:
		var ToolCallSummaryMessage ToolCallSummaryMessage
		if err := json.Unmarshal(event, &ToolCallSummaryMessage); err != nil {
			return nil, err
		}
		return &ToolCallSummaryMessage, nil
	default:
		return nil, fmt.Errorf("unknown event type: %s", baseEvent.Type)
	}
}

func GetLastStringMessage(events []Event) string {
	for i := len(events) - 1; i >= 0; i-- {
		if _, ok := events[i].(*TextMessage); ok {
			return events[i].(*TextMessage).Content
		}
	}
	return ""
}
