package ws

import (
	"encoding/json"
	"time"

	"github.com/kagent-dev/kagent/go/autogen/api"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
)

const (
	// InputTimeoutDuration defines how long to wait for user input
	InputTimeoutDuration = 5 * time.Minute
)

// WebSocketMessageType represents the type of WebSocket message
type MessageType string

const (
	MessageTypeStart         MessageType = "start"
	MessageTypeMessage       MessageType = "message"
	MessageTypeMessageChunk  MessageType = "message_chunk"
	MessageTypeInputRequest  MessageType = "input_request"
	MessageTypeResult        MessageType = "result"
	MessageTypeCompletion    MessageType = "completion"
	MessageTypeError         MessageType = "error"
	MessageTypeStop          MessageType = "stop"
	MessageTypeInputResponse MessageType = "input_response"
)

// MessageType represents the type of message within the WebSocket message/event
type ContentType string

const (
	ContentTypeText                ContentType = "TextMessage"
	ContentTypeToolCallRequest     ContentType = "ToolCallRequestEvent"
	ContentTypeToolCallExecution   ContentType = "ToolCallExecutionEvent"
	ContentTypeStop                ContentType = "StopMessage"
	ContentTypeHandoff             ContentType = "HandoffMessage"
	ContentTypeModelStreaming      ContentType = "ModelClientStreamingChunkEvent"
	ContentTypeLLMCallEventMessage ContentType = "LLMCallEventMessage"
)

type BaseWebSocketMessage struct {
	Type   MessageType     `json:"type"`
	Data   json.RawMessage `json:"data,omitempty"`
	Status string          `json:"status,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// StartMessage represents the initial message sent to start a task
type StartMessage struct {
	Type       MessageType    `json:"type"`
	Task       string         `json:"task"`
	TeamConfig *api.Component `json:"team_config"`
}

type TextMessage struct {
	Type        MessageType                `json:"type"`
	Content     string                     `json:"content"`
	Source      string                     `json:"source"`
	ModelsUsage autogen_client.ModelsUsage `json:"models_usage"`
}

type ToolCallRequest struct {
	Type        MessageType                `json:"type"`
	Content     []FunctionCall             `json:"content"`
	Source      string                     `json:"source"`
	ModelsUsage autogen_client.ModelsUsage `json:"models_usage"`
}

type ToolCallExecution struct {
	Type        MessageType                `json:"type"`
	Content     []FunctionExecutionResult  `json:"content"`
	Source      string                     `json:"source"`
	ModelsUsage autogen_client.ModelsUsage `json:"models_usage"`
}

type LLMCallEvent struct {
	Type    ContentType `json:"type"`
	Content string      `json:"content"`
	Source  string      `json:"source"`
}

type ModelStreamingEvent struct {
	Type        MessageType                `json:"type"`
	Content     string                     `json:"content"`
	Source      string                     `json:"source"`
	ModelsUsage autogen_client.ModelsUsage `json:"models_usage"`
}

type FunctionExecutionResult struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`
}

type FunctionCall struct {
	ID        string `json:"id"`
	Arguments string `json:"arguments"`
	Name      string `json:"name"`
}

// CompletionMessage represents the message sent to stop a task
type CompletionMessage struct {
	Type   MessageType `json:"type"`
	Status string      `json:"status"`
	Data   string      `json:"data"`
}

type StopMessage struct {
	Type   MessageType `json:"type"`
	Reason string      `json:"reason"`
	Code   int         `json:"code"`
}

// InputResponseMessage represents the message sent in response to an input request
type InputResponseMessage struct {
	Type     MessageType `json:"type"`
	Response string      `json:"response"`
}
type InputRequestMessage struct {
	Content string `json:"content"`
	Source  string `json:"source"`
}
