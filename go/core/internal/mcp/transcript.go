package mcp

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/core/internal/a2a"
)

const (
	defaultSessionMaxBytes = 100_000
	minSessionMaxBytes     = 4096
	maxSessionMaxBytes     = 4 << 20
)

// Turn is one turn of a conversation, one A2A task, addressed by its task ID,
// as a summary: the user's message, the agent's reply and one stub per tool
// call.
type Turn struct {
	TaskID    string         `json:"task_id"`
	State     string         `json:"state"`
	Timestamp string         `json:"timestamp,omitempty"`
	User      string         `json:"user,omitempty"`
	Reply     string         `json:"reply,omitempty"`
	ToolCalls []ToolCallStub `json:"tool_calls,omitempty"`
	Artifacts int            `json:"artifacts,omitempty"`
	// Clipped says the turn alone exceeded the budget: its tool calls were
	// dropped and its texts clipped.
	Clipped bool `json:"clipped,omitempty"`
}

// ToolCallStub names one tool call of a turn. Index addresses it in
// get_task's tool_call.
type ToolCallStub struct {
	Index   int    `json:"index"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name"`
	Pending bool   `json:"pending,omitempty"`
}

// TaskItem is one message or artifact of a task as A2A JSON, or, when it alone
// exceeds the budget, its index and size.
type TaskItem struct {
	Index        int `json:"index"`
	Message      any `json:"message,omitempty"`
	Artifact     any `json:"artifact,omitempty"`
	OmittedBytes int `json:"omitted_bytes,omitempty"`
}

// ToolCallParts is one tool call as the A2A parts that carry its call and its
// result. A part that exceeds the budget is left out and counted in
// OmittedBytes.
type ToolCallParts struct {
	Index        int    `json:"index"`
	ID           string `json:"id,omitempty"`
	Name         string `json:"name"`
	Pending      bool   `json:"pending,omitempty"`
	Call         any    `json:"call,omitempty"`
	Result       any    `json:"result,omitempty"`
	OmittedBytes int    `json:"omitted_bytes,omitempty"`
}

type turn struct {
	task    *a2atype.Task
	entries []entry
	calls   []*toolCall
}

// entry is one message or one artifact of a turn: exactly one is set.
type entry struct {
	message  *a2atype.Message
	artifact *a2atype.Artifact
}

func (e entry) parts() a2atype.ContentParts {
	if e.artifact != nil {
		return e.artifact.Parts
	}
	return e.message.Parts
}

func (e entry) metadata() a2atype.MetadataCarrier {
	if e.artifact != nil {
		return e.artifact
	}
	return e.message
}

// toolCall is a call and its result as the parts that first recorded them.
type toolCall struct {
	index        int
	id, name     string
	call, result *a2atype.Part
}

func readTurn(task *a2atype.Task) *turn {
	t := &turn{task: task, entries: turnEntries(task)}
	for _, e := range t.entries {
		for _, part := range e.parts() {
			activity, ok := apia2a.ParseToolActivity(part)
			if !ok {
				continue
			}
			switch activity.Kind {
			case apia2a.ToolCallKind:
				if call := t.byID(activity.ID); call != nil {
					if call.call == nil {
						call.call = part
					}
					continue
				}
				t.calls = append(t.calls, &toolCall{index: len(t.calls), id: activity.ID, name: activity.Name, call: part})
			case apia2a.ToolResultKind:
				call := t.unanswered(activity.ID, activity.Name)
				if call == nil {
					call = t.byID(activity.ID)
				}
				if call == nil {
					call = &toolCall{index: len(t.calls), id: activity.ID, name: activity.Name}
					t.calls = append(t.calls, call)
				}
				if call.result == nil {
					call.result = part
				}
			}
		}
	}
	return t
}

// byID is the call id names; nil for an empty id. A call or result recorded
// both in a message and in an artifact is one call.
func (t *turn) byID(id string) *toolCall {
	if id == "" {
		return nil
	}
	for _, call := range t.calls {
		if call.id == id {
			return call
		}
	}
	return nil
}

// unanswered is the first call without a result that id (or, without an id,
// name) names.
func (t *turn) unanswered(id, name string) *toolCall {
	for _, call := range t.calls {
		if call.result != nil {
			continue
		}
		if (id != "" && call.id == id) || (id == "" && call.name == name) {
			return call
		}
	}
	return nil
}

// turnEntries is a task's messages and artifacts in timeline order. Runtimes
// record tool activity as artifacts and the gateway answers artifacts apart
// from the history, so only the timeline position interleaves them. When an
// entry has none, the messages come first, then the artifacts.
func turnEntries(task *a2atype.Task) []entry {
	messages := turnMessages(task)
	entries := make([]entry, 0, len(messages)+len(task.Artifacts))
	for _, message := range messages {
		entries = append(entries, entry{message: message})
	}
	for _, artifact := range task.Artifacts {
		entries = append(entries, entry{artifact: artifact})
	}
	positions := make([]time.Time, len(entries))
	for i, e := range entries {
		position, ok := apia2a.TimelinePosition(e.metadata())
		if !ok {
			return entries
		}
		positions[i] = position
	}
	order := make([]int, len(entries))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return positions[a].Compare(positions[b]) })
	sorted := make([]entry, len(entries))
	for i, at := range order {
		sorted[i] = entries[at]
	}
	return sorted
}

// turnMessages is a task's history and, unless the history already holds it,
// its status message.
func turnMessages(task *a2atype.Task) []*a2atype.Message {
	status := task.Status.Message
	if status == nil {
		return task.History
	}
	for _, message := range task.History {
		if message == status || (status.ID != "" && message.ID == status.ID) {
			return task.History
		}
	}
	return append(slices.Clone(task.History), status)
}

func (t *turn) state() string { return t.task.Status.State.String() }

func (t *turn) timestamp() string {
	if t.task.Status.Timestamp == nil {
		return ""
	}
	return t.task.Status.Timestamp.UTC().Format(time.RFC3339)
}

// summary is the turn's first user message, its reply as invoke_agent answers
// it, and its tool call stubs.
func (t *turn) summary() Turn {
	out := Turn{TaskID: string(t.task.ID), State: t.state(), Timestamp: t.timestamp(), Artifacts: len(t.task.Artifacts), Reply: taskReply(t.task)}
	for _, e := range t.entries {
		if e.message != nil && e.message.Role == a2atype.MessageRoleUser {
			out.User = a2a.ExtractText(e.message)
			break
		}
	}
	for _, call := range t.calls {
		out.ToolCalls = append(out.ToolCalls, ToolCallStub{Index: call.index, ID: call.id, Name: call.name, Pending: call.result == nil})
	}
	return out
}

// items counts the turn's messages and artifacts.
func (t *turn) items() int { return len(t.entries) }

func (t *turn) item(i int) TaskItem {
	if e := t.entries[i]; e.artifact != nil {
		return TaskItem{Index: i, Artifact: e.artifact}
	}
	return TaskItem{Index: i, Message: t.entries[i].message}
}

// call is the turn's tool call ref names: its index or its id.
func (t *turn) call(ref string) *toolCall {
	if n, err := strconv.Atoi(ref); err == nil && n >= 0 && n < len(t.calls) {
		return t.calls[n]
	}
	for _, call := range t.calls {
		if call.id != "" && call.id == ref {
			return call
		}
	}
	return nil
}

// parts is call's parts within budget bytes, and whether a part was left out.
func (call *toolCall) parts(budget int) (ToolCallParts, bool) {
	out := ToolCallParts{Index: call.index, ID: call.id, Name: call.name, Pending: call.result == nil}
	omitted := false
	if call.call != nil {
		if size := jsonSize(call.call); jsonSize(out)+size <= budget {
			out.Call = call.call
		} else {
			out.OmittedBytes, omitted = out.OmittedBytes+size, true
		}
	}
	if call.result != nil {
		if size := jsonSize(call.result); jsonSize(out)+size <= budget {
			out.Result = call.result
		} else {
			out.OmittedBytes, omitted = out.OmittedBytes+size, true
		}
	}
	return out, omitted
}

// jsonSize is v's size in the answer.
func jsonSize(v any) int {
	data, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(data)
}

// clipText cuts s to at most n bytes on a rune boundary and says how much it
// cut, when n leaves room to say so.
func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	n = max(n, 0)
	reserve := len(clipMarker(len(s)))
	if n < reserve {
		reserve = 0
	}
	cut := n - reserve
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	if reserve == 0 {
		return s[:cut]
	}
	return s[:cut] + clipMarker(len(s)-cut)
}

func clipMarker(clipped int) string { return fmt.Sprintf("…[%d bytes clipped]", clipped) }

// fitTurn cuts a turn's summary that alone exceeds budget: it drops the tool
// calls, then clips the texts until it fits. It answers false when even that
// is too big.
func fitTurn(in Turn, budget int) (Turn, bool) {
	out := in
	out.Clipped = true
	if jsonSize(out) <= budget {
		return out, true
	}
	out.ToolCalls = nil
	rest := budget - jsonSize(Turn{TaskID: out.TaskID, State: out.State, Timestamp: out.Timestamp, Artifacts: out.Artifacts, Clipped: true})
	for userMax, replyMax := rest/4, rest*3/4; replyMax > 0; userMax, replyMax = userMax/2, replyMax/2 {
		out.User, out.Reply = clipText(in.User, userMax), clipText(in.Reply, replyMax)
		if jsonSize(out) <= budget {
			return out, true
		}
	}
	return out, false
}
