package driver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

// eventTranslator validates one turn's native identities and normalizes the
// pinned App Server notification vocabulary into runtime events.
type eventTranslator struct {
	threadID  string
	turnID    string
	tools     map[string]string
	approvals map[string][]string
}

func newEventTranslator(threadID, turnID string) *eventTranslator {
	return &eventTranslator{threadID: threadID, turnID: turnID, tools: make(map[string]string), approvals: make(map[string][]string)}
}

func (t *eventTranslator) translate(message rpcMessage, sink runtime.EventSink) (runtime.Outcome, bool, error) {
	switch message.Method {
	case "item/agentMessage/delta":
		var params struct{ ThreadID, TurnID, ItemID, Delta string }
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return runtime.Outcome{}, false, fmt.Errorf("decode Codex text delta: %w", err)
		}
		if params.ThreadID != t.threadID || params.TurnID != t.turnID || params.ItemID == "" {
			return runtime.Outcome{}, false, fmt.Errorf("codex text delta has mismatched identity")
		}
		return runtime.Outcome{}, false, sink.TextDelta(runtime.TextDelta{Text: params.Delta})
	case "item/started", "item/completed":
		var params struct {
			ThreadID, TurnID string
			Item             json.RawMessage
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return runtime.Outcome{}, false, fmt.Errorf("decode Codex item event: %w", err)
		}
		if params.ThreadID != t.threadID || params.TurnID != t.turnID {
			return runtime.Outcome{}, false, fmt.Errorf("codex item event has mismatched identity")
		}
		if err := t.translateItem(message.Method == "item/completed", params.Item, sink); err != nil {
			return runtime.Outcome{}, false, err
		}
		return runtime.Outcome{}, false, nil
	case "turn/completed":
		var params struct {
			ThreadID string
			Turn     struct {
				ID, Status string
				Error      *struct{ Message string }
			}
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return runtime.Outcome{}, false, fmt.Errorf("decode Codex terminal event: %w", err)
		}
		if params.ThreadID != t.threadID || params.Turn.ID != t.turnID {
			return runtime.Outcome{}, false, fmt.Errorf("codex terminal event has mismatched identity")
		}
		if err := t.closeActiveTools(sink); err != nil {
			return runtime.Outcome{}, false, err
		}
		switch params.Turn.Status {
		case "completed":
			return runtime.Outcome{}, true, nil
		case "interrupted":
			return runtime.Outcome{Failure: &runtime.Failure{Message: "Codex execution was interrupted"}}, true, nil
		case "failed":
			return runtime.Outcome{Failure: &runtime.Failure{Message: "Codex execution failed"}}, true, nil
		default:
			return runtime.Outcome{}, false, fmt.Errorf("unsupported Codex terminal status %q", params.Turn.Status)
		}
	default:
		// The pinned protocol is explicitly additive. Notifications unrelated to
		// the public text/tool/terminal contract are safe to ignore.
		return runtime.Outcome{}, false, nil
	}
}

func (t *eventTranslator) translateItem(completed bool, raw json.RawMessage, sink runtime.EventSink) error {
	var item struct {
		Type             string `json:"type"`
		ID               string `json:"id"`
		Command          string `json:"command"`
		CWD              string `json:"cwd"`
		AggregatedOutput string `json:"aggregatedOutput"`
		ExitCode         *int   `json:"exitCode"`
		Changes          any    `json:"changes"`
		Server           string `json:"server"`
		Tool             string `json:"tool"`
		Arguments        any    `json:"arguments"`
		Result           any    `json:"result"`
		Error            any    `json:"error"`
		Prompt           string `json:"prompt"`
		Status           string `json:"status"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return fmt.Errorf("decode Codex thread item: %w", err)
	}
	if item.ID == "" {
		return nil
	}
	name := ""
	approvalServer := ""
	var arguments map[string]any
	result := map[string]any{"status": item.Status}
	switch item.Type {
	case "commandExecution":
		name, arguments = "command_execution", map[string]any{"command": bounded(item.Command), "cwd": bounded(item.CWD)}
		result["output"] = bounded(item.AggregatedOutput)
		if item.ExitCode != nil {
			result["exitCode"] = *item.ExitCode
		}
	case "fileChange":
		name, arguments = "file_change", map[string]any{"changes": boundValue(item.Changes)}
		result["changes"] = boundValue(item.Changes)
	case "mcpToolCall":
		name, arguments = item.Server+"."+item.Tool, map[string]any{"arguments": boundValue(item.Arguments)}
		approvalServer = item.Server
		result["result"], result["error"] = boundValue(item.Result), boundValue(item.Error)
	case "collabAgentToolCall":
		name, arguments = "Agent", map[string]any{"prompt": bounded(item.Prompt), "tool": item.Tool}
	default:
		return nil
	}
	if !completed {
		if _, exists := t.tools[item.ID]; exists {
			return fmt.Errorf("codex tool item %q started more than once", item.ID)
		}
		t.tools[item.ID] = name
		if approvalServer != "" {
			t.approvals[approvalServer] = append(t.approvals[approvalServer], item.ID)
		}
		return sink.ToolCall(runtime.ToolCall{ID: item.ID, Name: name, Arguments: arguments})
	}
	startedName, exists := t.tools[item.ID]
	if !exists {
		return fmt.Errorf("codex tool item %q completed without starting", item.ID)
	}
	if startedName != name {
		return fmt.Errorf("codex tool item %q changed name from %q to %q", item.ID, startedName, name)
	}
	delete(t.tools, item.ID)
	return sink.ToolResult(runtime.ToolResult{ID: item.ID, Name: name, Result: result, IsError: item.Status == "failed"})
}

func (t *eventTranslator) closeActiveTools(sink runtime.EventSink) error {
	ids := make([]string, 0, len(t.tools))
	for id := range t.tools {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if err := sink.ToolResult(runtime.ToolResult{
			ID: id, Name: t.tools[id], Result: map[string]any{"error": "Codex turn ended before tool completion"}, IsError: true,
		}); err != nil {
			return err
		}
		delete(t.tools, id)
	}
	return nil
}

func (t *eventTranslator) approvalTool(server string) (string, string, error) {
	queue := t.approvals[server]
	for len(queue) != 0 {
		id := queue[0]
		queue = queue[1:]
		t.approvals[server] = queue
		if name, active := t.tools[id]; active {
			return id, name, nil
		}
	}
	return "", "", fmt.Errorf("codex requested approval without an active tool for server %q", server)
}

func rejectBufferedPostTerminalActivity(frames <-chan rpcFrame) error {
	for {
		select {
		case frame, ok := <-frames:
			if !ok {
				return nil
			}
			if frame.err != nil {
				return frame.err
			}
			if frame.message.Method == "turn/completed" {
				return fmt.Errorf("codex emitted duplicate terminal event")
			}
			if strings.HasPrefix(frame.message.Method, "item/") || strings.HasPrefix(frame.message.Method, "turn/") {
				return fmt.Errorf("codex emitted activity after its terminal event")
			}
		default:
			return nil
		}
	}
}

func boundValue(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{"omitted": "unencodable payload"}
	}
	if len(raw) <= 16<<10 {
		return value
	}
	return map[string]any{"omitted": "payload exceeded 16384 bytes"}
}

func bounded(value string) string {
	const limit = 16 << 10
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
