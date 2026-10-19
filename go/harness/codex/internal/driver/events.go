package driver

import (
	"cmp"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

// eventTranslator validates one turn's native identities and normalizes the
// pinned App Server notification vocabulary into runtime events.
type eventTranslator struct {
	threadID string
	turnID   string
	tools    map[string]activeTool

	now func() time.Time
	// modelCalls holds the current model call of each thread, keyed by thread ID. Codex sends no call times.
	modelCalls map[string]callTime
	// seenTotals holds the last running total per thread, to skip a replayed notification.
	seenTotals map[string]codexUsage
	// spawnedBy maps a subagent thread to the spawnAgent call that started it.
	spawnedBy map[string]string
	// retries counts each thread's retries of its open model call.
	retries map[string]int
	// completed is set when the turn finished before a cancel took effect, so no call is open.
	completed bool
}

// callTime estimates a model call. It starts at the previous call's usage, or when its thread
// starts, and ends when its first tool starts or the turn pauses for input.
type callTime struct{ start, end time.Time }

func newEventTranslator(threadID, turnID string) *eventTranslator {
	now := time.Now
	return &eventTranslator{
		threadID: threadID, turnID: turnID, tools: make(map[string]activeTool), now: now,
		modelCalls: map[string]callTime{threadID: {start: now()}},
		seenTotals: map[string]codexUsage{}, spawnedBy: map[string]string{}, retries: map[string]int{},
	}
}

type codexUsage struct {
	InputTokens, CachedInputTokens, CacheWriteInputTokens, OutputTokens, ReasoningOutputTokens int64
}

type activeTool struct {
	name            string
	tool            string
	server          string
	thread          string
	parentToolID    string
	arguments       any
	approvalMatched bool
}

func (t *eventTranslator) translate(message rpcMessage, sink runtime.EventSink) (runtime.Outcome, bool, error) {
	if subagent, err := t.subagentItem(message, sink); err != nil || subagent {
		return runtime.Outcome{}, false, err
	}
	if other, err := t.isOtherThreadNotification(message); err != nil || other {
		return runtime.Outcome{}, false, err
	}
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
		if err := t.translateItem(message.Method == "item/completed", params.Item, params.ThreadID, "", sink); err != nil {
			return runtime.Outcome{}, false, err
		}
		return runtime.Outcome{}, false, nil
	case "error":
		return runtime.Outcome{}, false, t.codexError(message.Params, sink)
	case "thread/tokenUsage/updated":
		return runtime.Outcome{}, false, t.usage(message.Params, sink)
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

func (t *eventTranslator) usage(raw json.RawMessage, sink runtime.ModelCallSink) error {
	var params struct {
		ThreadID, TurnID string
		TokenUsage       struct{ Total, Last codexUsage }
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return fmt.Errorf("decode Codex token usage: %w", err)
	}
	// The process serves one turn, so another thread is a subagent whose calls also count.
	own := params.ThreadID == t.threadID
	if own && params.TurnID != t.turnID {
		return nil
	}
	if total := params.TokenUsage.Total; total != (codexUsage{}) {
		if t.seenTotals[params.ThreadID] == total {
			return nil
		}
		t.seenTotals[params.ThreadID] = total
	}
	last := params.TokenUsage.Last
	call := runtime.ModelCall{
		InputTokens:           last.InputTokens,
		CacheReadTokens:       last.CachedInputTokens,
		CacheWriteTokens:      last.CacheWriteInputTokens,
		ReasoningOutputTokens: last.ReasoningOutputTokens,
		OutputTokens:          last.OutputTokens,
	}
	t.closeCall(&call, params.ThreadID)
	sink.ModelCall(call)
	return nil
}

// codexError counts a retried Codex error toward the thread's open model call, and records the
// final error as a failed call. Codex reports no usage for a failed call.
func (t *eventTranslator) codexError(raw json.RawMessage, sink runtime.ModelCallSink) error {
	var params struct {
		ThreadID, TurnID string
		Error            struct{ CodexErrorInfo json.RawMessage }
		WillRetry        bool
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return fmt.Errorf("decode Codex error: %w", err)
	}
	errorType, modelRequest := codexErrorType(params.Error.CodexErrorInfo)
	if (params.ThreadID == t.threadID && params.TurnID != t.turnID) || !modelRequest {
		return nil
	}
	if params.WillRetry {
		t.retries[params.ThreadID]++
		return nil
	}
	call := runtime.ModelCall{StopReason: tracing.FinishReasonError, ErrorType: errorType, UsagePartial: true}
	t.closeCall(&call, params.ThreadID)
	sink.ModelCall(call)
	return nil
}

// closeCall gives a call its thread, retries and estimated times; the thread's next call starts now.
// A thread not known yet has no start time, so its call is zero-length at arrival.
func (t *eventTranslator) closeCall(call *runtime.ModelCall, threadID string) {
	end := t.now()
	call.ParentToolID, call.Start, call.End = t.spawnedBy[threadID], end, end
	call.Retries = t.retries[threadID]
	delete(t.retries, threadID)
	if timing, known := t.modelCalls[threadID]; known {
		call.Start, call.End = timing.start, cmp.Or(timing.end, end)
		t.modelCalls[threadID] = callTime{start: end}
	}
}

// codexErrorType turns Codex's error category into a snake_case error.type, or "" for an
// uncategorized error. It reports false for errors that are not about a model request.
func codexErrorType(raw json.RawMessage) (string, bool) {
	var name string
	if json.Unmarshal(raw, &name) != nil {
		// A category with details, such as {"responseStreamDisconnected":{"httpStatusCode":null}}.
		var variant map[string]json.RawMessage
		if json.Unmarshal(raw, &variant) != nil || len(variant) != 1 {
			return "", true
		}
		for key := range variant {
			name = key
		}
	}
	switch name {
	// A sandbox error comes from a command, and a session budget error follows a call that succeeded.
	case "sandboxError", "sessionBudgetExceeded":
		return "", false
	case "", "other":
		return "", true
	}
	var snake strings.Builder
	for _, r := range name {
		if unicode.IsUpper(r) {
			snake.WriteByte('_')
		}
		snake.WriteRune(unicode.ToLower(r))
	}
	return snake.String(), true
}

// endOpenCalls ends the open call of every thread at a pause, so no call counts the wait.
func (t *eventTranslator) endOpenCalls() {
	for thread, call := range t.modelCalls {
		if call.end.IsZero() {
			t.modelCalls[thread] = callTime{start: call.start, end: t.now()}
		}
	}
}

// flushOpenCall records the main thread's call still streaming at a cancel. Codex reports no usage
// for an interrupted response; a call it reported after the interrupt is no longer open.
func (t *eventTranslator) flushOpenCall(before time.Time, sink runtime.ModelCallSink) {
	if open, known := t.modelCalls[t.threadID]; t.completed || !known || !open.end.IsZero() || !open.start.Before(before) {
		return
	}
	call := runtime.ModelCall{StopReason: tracing.FinishReasonError, UsagePartial: true, Canceled: true}
	t.closeCall(&call, t.threadID)
	sink.ModelCall(call)
}

// subagentItem translates an item of a spawned subagent, so its tools go under the spawn call.
// It reports false for any other notification. A subagent item that does not fit is dropped.
func (t *eventTranslator) subagentItem(message rpcMessage, sink runtime.EventSink) (bool, error) {
	if message.Method != "item/started" && message.Method != "item/completed" {
		return false, nil
	}
	var params struct {
		ThreadID string
		Item     json.RawMessage
	}
	if err := json.Unmarshal(message.Params, &params); err != nil {
		return false, fmt.Errorf("decode Codex item event: %w", err)
	}
	spawn, ok := t.spawnedBy[params.ThreadID]
	if !ok {
		return false, nil
	}
	var item struct{ ID string }
	if err := json.Unmarshal(params.Item, &item); err != nil {
		return true, nil
	}
	// The threads share one stream, so a start can come before the spawn completes, or reuse
	// another thread's ID. Such items are dropped, so they do not fail the main turn.
	completed := message.Method == "item/completed"
	if started, open := t.tools[item.ID]; open != completed || (completed && started.thread != params.ThreadID) {
		return true, nil
	}
	return true, t.translateItem(completed, params.Item, params.ThreadID, spawn, sink)
}

func (t *eventTranslator) translateItem(completed bool, raw json.RawMessage, thread, parentToolID string, sink runtime.EventSink) error {
	var item struct {
		Type              string   `json:"type"`
		ID                string   `json:"id"`
		Command           string   `json:"command"`
		CWD               string   `json:"cwd"`
		AggregatedOutput  string   `json:"aggregatedOutput"`
		ExitCode          *int     `json:"exitCode"`
		Changes           any      `json:"changes"`
		Server            string   `json:"server"`
		Tool              string   `json:"tool"`
		Arguments         any      `json:"arguments"`
		Result            any      `json:"result"`
		Error             any      `json:"error"`
		Prompt            string   `json:"prompt"`
		Status            string   `json:"status"`
		ReceiverThreadIDs []string `json:"receiverThreadIds"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return fmt.Errorf("decode Codex thread item: %w", err)
	}
	if item.ID == "" {
		return nil
	}
	name, mcpServer, mcpTool := "", "", ""
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
		mcpServer, mcpTool = item.Server, item.Tool
		result["result"], result["error"] = boundValue(item.Result), boundValue(item.Error)
	case "collabAgentToolCall":
		name, arguments = "Agent", map[string]any{"prompt": bounded(item.Prompt), "tool": item.Tool}
		// wait and closeAgent calls also list the subagent thread. Only the spawnAgent call started it.
		if item.Tool == "spawnAgent" {
			for _, receiver := range item.ReceiverThreadIDs {
				if receiver != t.threadID && receiver != thread {
					t.spawnedBy[receiver] = item.ID
					if _, started := t.modelCalls[receiver]; !started {
						t.modelCalls[receiver] = callTime{start: t.now()}
					}
				}
			}
		}
	default:
		return nil
	}
	if !completed {
		if existing, exists := t.tools[item.ID]; exists {
			// A main tool wins over a subagent tool that reused its ID.
			if parentToolID != "" || existing.parentToolID == "" {
				return fmt.Errorf("codex tool item %q started more than once", item.ID)
			}
			delete(t.tools, item.ID)
			if err := sink.ToolResult(runtime.ToolResult{
				ID: item.ID, Name: existing.name, Result: map[string]any{"error": "Codex reused the tool ID"}, IsError: true, Status: runtime.ToolUnfinished,
			}); err != nil {
				return err
			}
		}
		t.tools[item.ID] = activeTool{name: name, tool: item.Tool, server: mcpServer, thread: thread, parentToolID: parentToolID, arguments: item.Arguments}
		// A tool ends the open call of its own thread only.
		if call, open := t.modelCalls[thread]; open && call.end.IsZero() {
			t.modelCalls[thread] = callTime{start: call.start, end: t.now()}
		}
		return sink.ToolCall(runtime.ToolCall{
			ID: item.ID, Name: name, Arguments: arguments, MCPServer: mcpServer, MCPTool: mcpTool, ParentToolID: parentToolID,
		})
	}
	started, exists := t.tools[item.ID]
	if !exists || started.thread != thread {
		return fmt.Errorf("codex tool item %q completed without starting", item.ID)
	}
	if started.name != name {
		return fmt.Errorf("codex tool item %q changed name from %q to %q", item.ID, started.name, name)
	}
	delete(t.tools, item.ID)
	status := runtime.ToolRan
	if item.Status == "declined" {
		status = runtime.ToolDeclined
	}
	return sink.ToolResult(runtime.ToolResult{ID: item.ID, Name: name, Result: result, IsError: item.Status == "failed", Status: status})
}

func (t *eventTranslator) closeActiveTools(sink runtime.EventSink) error {
	ids := make([]string, 0, len(t.tools))
	for id := range t.tools {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if err := sink.ToolResult(runtime.ToolResult{
			ID: id, Name: t.tools[id].name, Result: map[string]any{"error": "Codex turn ended before tool completion"}, IsError: true, Status: runtime.ToolUnfinished,
		}); err != nil {
			return err
		}
		delete(t.tools, id)
	}
	return nil
}

func (t *eventTranslator) approvalTool(thread, server, toolName string, arguments map[string]any) (string, string, error) {
	matchedID := ""
	for id, tool := range t.tools {
		// A subagent can make the same call as the main agent. A request without a thread matches any.
		if tool.approvalMatched || (thread != "" && tool.thread != thread) || tool.server != server || !reflect.DeepEqual(tool.arguments, arguments) {
			continue
		}
		if toolName != "" && tool.name != server+"."+toolName {
			continue
		}
		if matchedID != "" {
			return "", "", fmt.Errorf("codex requested approval matching multiple active tools for server %q", server)
		}
		matchedID = id
	}
	if matchedID == "" {
		return "", "", fmt.Errorf("codex requested approval without a matching active tool for server %q", server)
	}
	matched := t.tools[matchedID]
	matched.approvalMatched = true
	t.tools[matchedID] = matched
	return matchedID, matched.name, nil
}

// App Server multiplexes native Shared agents onto the parent's stream. Their text and
// completion stay on their own threads; subagentItem translates their tools.
func (t *eventTranslator) isOtherThreadNotification(message rpcMessage) (bool, error) {
	if len(message.ID) != 0 || (!strings.HasPrefix(message.Method, "item/") && !strings.HasPrefix(message.Method, "turn/")) {
		return false, nil
	}
	var params struct{ ThreadID string }
	if err := json.Unmarshal(message.Params, &params); err != nil {
		return false, fmt.Errorf("decode Codex notification identity: %w", err)
	}
	// A missing identity must still reach the normal validation below.
	return params.ThreadID != "" && params.ThreadID != t.threadID, nil
}

func (t *eventTranslator) rejectBufferedPostTerminalActivity(frames <-chan rpcFrame) error {
	for {
		select {
		case frame, ok := <-frames:
			if !ok {
				return nil
			}
			if frame.err != nil {
				return frame.err
			}
			if other, err := t.isOtherThreadNotification(frame.message); err != nil {
				return err
			} else if other {
				continue
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
