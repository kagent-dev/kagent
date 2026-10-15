package driver

import (
	"encoding/json"
	"fmt"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

// The frames of one prompt the adapter acts on. Everything else omp emits —
// turn and message boundaries, queue and status updates, presentation-only UI
// requests — carries nothing the shared runtime model has a place for.
const (
	frameMessageUpdate  = "message_update"
	frameToolStart      = "tool_execution_start"
	frameToolEnd        = "tool_execution_end"
	framePromptResult   = "prompt_result"
	frameSessionSettled = "session_settled"
	frameUIRequest      = "extension_ui_request"
	frameResponse       = "response"
)

type messageUpdate struct {
	Event struct {
		Type  string `json:"type"`
		Delta string `json:"delta"`
	} `json:"assistantMessageEvent"`
}

type toolStart struct {
	ID   string         `json:"toolCallId"`
	Name string         `json:"toolName"`
	Args map[string]any `json:"args"`
}

type toolEnd struct {
	ID      string `json:"toolCallId"`
	Name    string `json:"toolName"`
	Result  any    `json:"result"`
	IsError bool   `json:"isError"`
}

type promptResult struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	SessionSettled bool   `json:"sessionSettled"`
	Error          *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type uiRequest struct {
	ID     string `json:"id"`
	Method string `json:"method"`
}

// uiCancel answers a dialog omp should not have asked: the adapter runs omp
// without a UI, so a dialog resolves to its default instead of parking the turn.
type uiCancel struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Cancelled bool   `json:"cancelled"`
}

var dialogMethods = map[string]bool{"select": true, "confirm": true, "input": true, "editor": true, "ask": true}

// promptTracker follows one prompt from its command to the session settling.
type promptTracker struct {
	promptID string
	result   *promptResult
	settled  bool
}

// translate forwards one frame of the running prompt to sink and reports
// whether the prompt is over: omp has answered it and nothing can wake the
// session again, so the process can be stopped without losing work.
func (p *promptTracker) translate(f frame, sink runtime.EventSink, reply func(any) error) (bool, error) {
	switch f.Type {
	case frameMessageUpdate:
		var update messageUpdate
		if err := json.Unmarshal(f.Raw, &update); err != nil {
			return false, fmt.Errorf("decode omp message update: %w", err)
		}
		if update.Event.Type == "text_delta" && update.Event.Delta != "" {
			return false, sink.TextDelta(runtime.TextDelta{Text: update.Event.Delta})
		}
	case frameToolStart:
		var start toolStart
		if err := json.Unmarshal(f.Raw, &start); err != nil {
			return false, fmt.Errorf("decode omp tool start: %w", err)
		}
		return false, sink.ToolCall(runtime.ToolCall{ID: start.ID, Name: start.Name, Arguments: start.Args})
	case frameToolEnd:
		var end toolEnd
		if err := json.Unmarshal(f.Raw, &end); err != nil {
			return false, fmt.Errorf("decode omp tool end: %w", err)
		}
		return false, sink.ToolResult(runtime.ToolResult{ID: end.ID, Name: end.Name, Result: end.Result, IsError: end.IsError})
	case frameUIRequest:
		var request uiRequest
		if err := json.Unmarshal(f.Raw, &request); err != nil {
			return false, fmt.Errorf("decode omp UI request: %w", err)
		}
		if dialogMethods[request.Method] {
			return false, reply(uiCancel{Type: "extension_ui_response", ID: request.ID, Cancelled: true})
		}
	case frameResponse:
		// A prompt that fails before admission answers with an error response
		// and still completes through its prompt_result. One that a builtin
		// slash command finished locally completes here, with no prompt_result.
		var admitted struct {
			AgentInvoked *bool `json:"agentInvoked"`
		}
		if f.ID == p.promptID && f.Success && json.Unmarshal(f.Data, &admitted) == nil &&
			admitted.AgentInvoked != nil && !*admitted.AgentInvoked {
			p.result, p.settled = &promptResult{ID: f.ID, Status: "completed"}, true
		}
	case framePromptResult:
		var result promptResult
		if err := json.Unmarshal(f.Raw, &result); err != nil {
			return false, fmt.Errorf("decode omp prompt result: %w", err)
		}
		if result.ID != p.promptID {
			return false, nil
		}
		p.result, p.settled = &result, result.SessionSettled
	case frameSessionSettled:
		p.settled = p.result != nil
	}
	return p.result != nil && p.settled, nil
}

// failure is the prompt's public failure, nil when it completed.
func (p *promptTracker) failure() *runtime.Failure {
	switch {
	case p.result.Status == "completed":
		return nil
	case p.result.Error != nil && p.result.Error.Message != "":
		return &runtime.Failure{Message: p.result.Error.Message}
	default:
		return &runtime.Failure{Message: "omp ended the turn with status " + p.result.Status}
	}
}

// sessionStats is the part of get_session_stats a turn's usage is computed
// from: omp reports the session's running totals.
type sessionStats struct {
	AssistantMessages int `json:"assistantMessages"`
	Tokens            struct {
		Input      int `json:"input"`
		Output     int `json:"output"`
		CacheRead  int `json:"cacheRead"`
		CacheWrite int `json:"cacheWrite"`
	} `json:"tokens"`
	Cost float64 `json:"cost"`
}

// turnUsage is what the session consumed between two snapshots. omp counts
// prompt tokens as Anthropic does, without the cached part; the runtime model
// counts every prompt token as input and the cache reads among them.
func turnUsage(before, after sessionStats) *runtime.Usage {
	cacheRead := after.Tokens.CacheRead - before.Tokens.CacheRead
	return &runtime.Usage{
		TotalCostUSD: after.Cost - before.Cost,
		NumTurns:     after.AssistantMessages - before.AssistantMessages,
		InputTokens: after.Tokens.Input - before.Tokens.Input + cacheRead +
			after.Tokens.CacheWrite - before.Tokens.CacheWrite,
		CachedTokens: cacheRead,
		OutputTokens: after.Tokens.Output - before.Tokens.Output,
	}
}
