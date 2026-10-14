package bridge

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

type content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}
type toolContent struct {
	Type    string  `json:"type"`
	Content content `json:"content"`
}
type update struct {
	SessionUpdate string          `json:"sessionUpdate"`
	ToolCallID    string          `json:"toolCallId"`
	Title         string          `json:"title"`
	Status        string          `json:"status"`
	RawInput      json.RawMessage `json:"rawInput"`
	Content       json.RawMessage `json:"content"`
}
type notification struct {
	SessionID string `json:"sessionId"`
	Update    update `json:"update"`
}
type permission struct {
	SessionID string `json:"sessionId"`
	ToolCall  struct {
		ID string `json:"toolCallId"`
	} `json:"toolCall"`
	Options []struct {
		ID   string `json:"optionId"`
		Kind string `json:"kind"`
	} `json:"options"`
}

type tool struct {
	name     string
	args     map[string]any
	declined bool
}
type events struct {
	sessionID string
	tools     map[string]tool
}

func (e *events) apply(data json.RawMessage, sink runtime.EventSink) error {
	var n notification
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("failed to decode ACP update: %w", err)
	}
	if n.SessionID != e.sessionID {
		return fmt.Errorf("ACP update has mismatched session identity")
	}
	u := n.Update
	switch u.SessionUpdate {
	case "agent_message_chunk":
		var c content
		if err := json.Unmarshal(u.Content, &c); err != nil {
			return fmt.Errorf("failed to decode ACP text: %w", err)
		}
		if c.Type != "text" {
			return fmt.Errorf("DSH BYO supports text output only")
		}
		return sink.TextDelta(runtime.TextDelta{Text: c.Text})
	case "tool_call":
		if u.ToolCallID == "" || u.Title == "" {
			return fmt.Errorf("ACP tool call omitted identity or name")
		}
		if _, exists := e.tools[u.ToolCallID]; exists {
			return fmt.Errorf("ACP tool call identity was reused")
		}
		args := make(map[string]any)
		if len(u.RawInput) != 0 {
			var input any
			if err := json.Unmarshal(u.RawInput, &input); err != nil {
				return fmt.Errorf("failed to decode ACP tool input: %w", err)
			}
			var object bool
			args, object = input.(map[string]any)
			if !object {
				// This is an observation, not a request to execute a tool. DSH
				// validates the original input and returns its error to the model.
				// Use JSON-compatible values for A2A's protobuf conversion.
				args = map[string]any{"rawInput": input}
			}
		}
		e.tools[u.ToolCallID] = tool{name: u.Title, args: args}
		return sink.ToolCall(runtime.ToolCall{ID: u.ToolCallID, Name: u.Title, Arguments: args})
	case "tool_call_update":
		t, exists := e.tools[u.ToolCallID]
		if !exists {
			return fmt.Errorf("ACP tool result has no matching call")
		}
		if u.Status == "pending" || u.Status == "in_progress" {
			return nil
		}
		if u.Status != "completed" && u.Status != "failed" {
			return fmt.Errorf("unsupported ACP tool status")
		}
		var output []toolContent
		if len(u.Content) != 0 {
			if err := json.Unmarshal(u.Content, &output); err != nil {
				return fmt.Errorf("failed to decode ACP tool result: %w", err)
			}
		}
		delete(e.tools, u.ToolCallID)
		// A2A data parts must contain JSON-compatible values, not private Go structs.
		var text strings.Builder
		for _, block := range output {
			if block.Type != "content" || block.Content.Type != "text" {
				return fmt.Errorf("DSH BYO supports text tool results only")
			}
			text.WriteString(block.Content.Text)
		}
		status := runtime.ToolRan
		if t.declined {
			status = runtime.ToolDeclined
		}
		return sink.ToolResult(runtime.ToolResult{ID: u.ToolCallID, Name: t.name, Result: text.String(), IsError: u.Status == "failed", Status: status})
	case "agent_thought_chunk", "usage_update", "config_option_update", "current_mode_update":
		// Context occupancy is not per-request token usage. Do not invent ModelCall metrics.
		return nil
	default:
		return fmt.Errorf("unsupported ACP session update")
	}
}

func (e *events) approval(id json.RawMessage, data json.RawMessage) (*runtime.ApprovalRequest, string, string, error) {
	var p permission
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, "", "", fmt.Errorf("failed to decode ACP permission request: %w", err)
	}
	t, exists := e.tools[p.ToolCall.ID]
	if p.SessionID != e.sessionID || !exists {
		return nil, "", "", fmt.Errorf("ACP permission request has no matching session and tool")
	}
	allow, reject := "", ""
	for _, option := range p.Options {
		switch option.Kind {
		case "allow_once":
			allow = option.ID
		case "reject_once":
			reject = option.ID
		}
	}
	if allow == "" || reject == "" {
		return nil, "", "", fmt.Errorf("ACP permission request must offer one-shot allow and reject")
	}
	request := &runtime.ApprovalRequest{ID: "dsh-acp:" + string(id), CallID: p.ToolCall.ID, Name: t.name, Args: t.args}
	return request, allow, reject, nil
}
