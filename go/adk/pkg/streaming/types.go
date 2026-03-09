package streaming

import (
	"encoding/json"
	"time"
)

// StreamEvent represents a real-time event published over NATS for LLM tokens,
// tool progress, approval requests, and errors.
type StreamEvent struct {
	Type      EventType `json:"type"`
	Data      string    `json:"data"`
	Timestamp int64     `json:"timestamp"`
}

// EventType classifies the kind of streaming event.
type EventType string

const (
	EventTypeToken           EventType = "token"
	EventTypeToolStart       EventType = "tool_start"
	EventTypeToolEnd         EventType = "tool_end"
	EventTypeApprovalRequest EventType = "approval_request"
	EventTypeCompletion      EventType = "completion"
	EventTypeError           EventType = "error"
)

// NewStreamEvent creates a StreamEvent with the current timestamp.
func NewStreamEvent(eventType EventType, data string) *StreamEvent {
	return &StreamEvent{
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now().UnixMilli(),
	}
}

// ApprovalRequest is published when a workflow requires HITL approval.
type ApprovalRequest struct {
	WorkflowID string `json:"workflowID"`
	RunID      string `json:"runID"`
	SessionID  string `json:"sessionID"`
	Message    string `json:"message"`
	ToolName   string `json:"toolName,omitempty"`
	ToolID     string `json:"toolID,omitempty"`
}

// ToolCallEvent carries structured tool call data for the UI.
type ToolCallEvent struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// ToolResultEvent carries structured tool result data for the UI.
type ToolResultEvent struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response,omitempty"`
	IsError  bool            `json:"isError,omitempty"`
}

// SubjectForAgent returns the NATS subject for an agent's session stream.
// Pattern: agent.{agentName}.{sessionID}.stream
func SubjectForAgent(agentName, sessionID string) string {
	return "agent." + agentName + "." + sessionID + ".stream"
}
