package mcp

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

// The views get_session and get_session_turn answer a conversation in.
const (
	// detailSummary is per turn the user's message, the agent's reply and one
	// stub per tool call.
	detailSummary = "summary"
	// detailTranscript adds every message, tool calls with clipped arguments
	// and tool results as stubs.
	detailTranscript = "transcript"
	// detailFull is the A2A objects as the gateway answers them.
	detailFull = "full"
)

const (
	defaultSessionMaxBytes = 100_000
	minSessionMaxBytes     = 4096
	maxSessionMaxBytes     = 4 << 20
	// maxArgsBytes clips a tool call's arguments in a transcript.
	maxArgsBytes = 500
	// maxUnwrapDepth bounds how many envelopes a tool response is unwrapped
	// through.
	maxUnwrapDepth = 6
)

// Turn is one turn of a conversation, one A2A task, indexed from 0 oldest
// first. The summary view sets User, Reply and ToolCalls, the transcript view
// Messages, which hold all of them.
type Turn struct {
	Index     int            `json:"index"`
	TaskID    string         `json:"task_id"`
	State     string         `json:"state"`
	Timestamp string         `json:"timestamp,omitempty"`
	User      string         `json:"user,omitempty"`
	Reply     string         `json:"reply,omitempty"`
	ToolCalls []ToolCallStub `json:"tool_calls,omitempty"`
	Artifacts int            `json:"artifacts,omitempty"`
	Messages  []TurnMessage  `json:"messages,omitempty"`
	// Clipped says the turn alone exceeded the budget: its tool calls were
	// dropped and its texts clipped.
	Clipped bool `json:"clipped,omitempty"`
}

// ToolCallStub names one tool call of a turn. Index addresses it in
// get_session_turn's tool_call.
type ToolCallStub struct {
	Index   int    `json:"index"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name"`
	Error   bool   `json:"error,omitempty"`
	Pending bool   `json:"pending,omitempty"`
}

// TurnMessage is one message of a turn in the transcript view, or one of the
// task's artifacts after them (role "artifact"). Index is its position in the
// turn, as get_session_turn's from_message counts.
type TurnMessage struct {
	Index   int        `json:"index"`
	Role    string     `json:"role"`
	Name    string     `json:"name,omitempty"`
	Parts   []PartView `json:"parts,omitempty"`
	Clipped bool       `json:"clipped,omitempty"`
}

// PartView is one part of a message: exactly one field is set.
type PartView struct {
	Text       string          `json:"text,omitempty"`
	ToolCall   *ToolCallView   `json:"tool_call,omitempty"`
	ToolResult *ToolResultView `json:"tool_result,omitempty"`
	Other      *OtherPartView  `json:"other,omitempty"`
}

// ToolCallView is a tool call with its arguments as JSON, clipped.
type ToolCallView struct {
	Index int    `json:"index"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name"`
	Args  string `json:"args,omitempty"`
}

// ToolResultView stands for a tool response: the call it answers, whether it
// failed and its size.
type ToolResultView struct {
	Index int    `json:"index"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name"`
	Error bool   `json:"error,omitempty"`
	Bytes int    `json:"bytes"`
}

// OtherPartView stands for a file (raw or url) or data part.
type OtherPartView struct {
	Kind      string `json:"kind"`
	MediaType string `json:"media_type,omitempty"`
	Filename  string `json:"filename,omitempty"`
	URL       string `json:"url,omitempty"`
	Bytes     int    `json:"bytes,omitempty"`
}

// ToolCallDetail is one tool call whole: its arguments and its response
// unwrapped from the envelopes it travelled in. ArgsText and ResponseText
// replace Args and Response when they had to be clipped.
type ToolCallDetail struct {
	Index        int    `json:"index"`
	ID           string `json:"id,omitempty"`
	Name         string `json:"name"`
	Pending      bool   `json:"pending,omitempty"`
	Error        bool   `json:"error,omitempty"`
	Args         any    `json:"args,omitempty"`
	ArgsText     string `json:"args_text,omitempty"`
	Response     any    `json:"response,omitempty"`
	ResponseText string `json:"response_text,omitempty"`
}

type turn struct {
	index    int
	task     *a2atype.Task
	messages []*a2atype.Message
	calls    []*toolCall
	callOf   map[*a2atype.Part]*toolCall
	kindOf   map[*a2atype.Part]apia2a.ToolActivityKind
}

type toolCall struct {
	index    int
	id, name string
	args     any
	response any
	answered bool
	failed   bool
}

func readTurn(index int, task *a2atype.Task) *turn {
	t := &turn{
		index: index, task: task, messages: turnMessages(task),
		callOf: map[*a2atype.Part]*toolCall{}, kindOf: map[*a2atype.Part]apia2a.ToolActivityKind{},
	}
	for _, message := range t.messages {
		for _, part := range message.Parts {
			activity, ok := apia2a.ParseToolActivity(part)
			if !ok {
				continue
			}
			t.kindOf[part] = activity.Kind
			switch activity.Kind {
			case apia2a.ToolCallKind:
				call := &toolCall{index: len(t.calls), id: activity.ID, name: activity.Name, args: activity.Args}
				t.calls = append(t.calls, call)
				t.callOf[part] = call
			case apia2a.ToolResultKind:
				call := t.unanswered(activity.ID, activity.Name)
				if call == nil {
					call = &toolCall{index: len(t.calls), id: activity.ID, name: activity.Name}
					t.calls = append(t.calls, call)
				}
				call.answered = true
				call.response = activity.Response
				_, call.failed = unwrapResponse(call.response)
				t.callOf[part] = call
			}
		}
	}
	return t
}

// unanswered is the first call without a response that id (or, without an
// id, name) names.
func (t *turn) unanswered(id, name string) *toolCall {
	for _, call := range t.calls {
		if call.answered {
			continue
		}
		if (id != "" && call.id == id) || (id == "" && call.name == name) {
			return call
		}
	}
	return nil
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

func (t *turn) header() Turn {
	return Turn{Index: t.index, TaskID: string(t.task.ID), State: t.state(), Timestamp: t.timestamp(), Artifacts: len(t.task.Artifacts)}
}

// summary is the turn's first user message, its status message (else its
// artifacts' text) and its tool call stubs.
func (t *turn) summary() Turn {
	out := t.header()
	out.Reply = partsText(statusParts(t.task))
	if out.Reply == "" {
		var texts []string
		for _, artifact := range t.task.Artifacts {
			if text := partsText(artifact.Parts); text != "" {
				texts = append(texts, text)
			}
		}
		out.Reply = strings.Join(texts, "\n")
	}
	for _, message := range t.messages {
		if message.Role == a2atype.MessageRoleUser {
			out.User = partsText(message.Parts)
			break
		}
	}
	for _, call := range t.calls {
		out.ToolCalls = append(out.ToolCalls, ToolCallStub{Index: call.index, ID: call.id, Name: call.name, Error: call.failed, Pending: !call.answered})
	}
	return out
}

func statusParts(task *a2atype.Task) a2atype.ContentParts {
	if task.Status.Message == nil {
		return nil
	}
	return task.Status.Message.Parts
}

func (t *turn) transcript() Turn {
	out := t.header()
	for i := range t.items() {
		out.Messages = append(out.Messages, t.messageView(i))
	}
	return out
}

// items counts the turn's messages and, after them, its artifacts.
func (t *turn) items() int { return len(t.messages) + len(t.task.Artifacts) }

func (t *turn) messageView(i int) TurnMessage {
	if i >= len(t.messages) {
		artifact := t.task.Artifacts[i-len(t.messages)]
		out := TurnMessage{Index: i, Role: "artifact", Name: artifact.Name}
		for _, part := range artifact.Parts {
			out.Parts = append(out.Parts, t.partView(part))
		}
		return out
	}
	message := t.messages[i]
	out := TurnMessage{Index: i, Role: strings.ToLower(strings.TrimPrefix(string(message.Role), "ROLE_"))}
	for _, part := range message.Parts {
		out.Parts = append(out.Parts, t.partView(part))
	}
	return out
}

// rawItem is the i-th message as A2A JSON, or an artifact as {"artifact": …}.
func (t *turn) rawItem(i int) (json.RawMessage, error) {
	if i < len(t.messages) {
		return json.Marshal(t.messages[i])
	}
	return json.Marshal(map[string]*a2atype.Artifact{"artifact": t.task.Artifacts[i-len(t.messages)]})
}

func (t *turn) partView(part *a2atype.Part) PartView {
	if call, ok := t.callOf[part]; ok {
		if t.kindOf[part] == apia2a.ToolCallKind {
			return PartView{ToolCall: &ToolCallView{Index: call.index, ID: call.id, Name: call.name, Args: clipText(jsonText(call.args), maxArgsBytes)}}
		}
		return PartView{ToolResult: &ToolResultView{Index: call.index, ID: call.id, Name: call.name, Error: call.failed, Bytes: len(jsonText(call.response))}}
	}
	switch content := part.Content.(type) {
	case a2atype.Text:
		return PartView{Text: string(content)}
	case a2atype.Raw:
		return PartView{Other: &OtherPartView{Kind: "raw", MediaType: part.MediaType, Filename: part.Filename, Bytes: len(content)}}
	case a2atype.URL:
		return PartView{Other: &OtherPartView{Kind: "url", MediaType: part.MediaType, Filename: part.Filename, URL: string(content)}}
	}
	return PartView{Other: &OtherPartView{Kind: "data", MediaType: part.MediaType, Filename: part.Filename, Bytes: len(jsonText(part.Data()))}}
}

// call is the turn's tool call ref names: its index or its id.
func (t *turn) call(ref string) *toolCall {
	for _, call := range t.calls {
		if fmt.Sprint(call.index) == ref {
			return call
		}
	}
	for _, call := range t.calls {
		if call.id != "" && call.id == ref {
			return call
		}
	}
	return nil
}

// toolCallDetail is call whole, within budget bytes.
func (t *turn) toolCallDetail(call *toolCall, budget int) (ToolCallDetail, bool) {
	out := ToolCallDetail{Index: call.index, ID: call.id, Name: call.name, Pending: !call.answered, Error: call.failed}
	out.Args = call.args
	if call.answered {
		out.Response, _ = unwrapResponse(call.response)
	}
	if jsonSize(out) <= budget {
		return out, false
	}
	// The smaller of arguments and response stays whole when it fits half the
	// room; the other is clipped to what is left.
	args, response := json.RawMessage(jsonText(out.Args)), json.RawMessage(jsonText(out.Response))
	out.Args, out.Response = nil, nil
	room := budget - jsonSize(out)
	argsShare, responseShare := room/2, room/2
	switch {
	case len(args) <= room/2:
		argsShare, responseShare = len(args), room-len(args)
	case len(response) <= room/2:
		argsShare, responseShare = room-len(response), len(response)
	}
	if len(args) <= argsShare {
		out.Args = args
	}
	if len(response) <= responseShare {
		out.Response = response
	}
	for range 8 {
		if out.Args == nil && len(args) > 0 {
			out.ArgsText = clipText(jsonString(args), argsShare)
		}
		if out.Response == nil && len(response) > 0 {
			out.ResponseText = clipText(jsonString(response), responseShare)
		}
		over := jsonSize(out) - budget
		if over <= 0 {
			break
		}
		// Escaping grew the clipped texts: take the excess off them.
		if out.ResponseText != "" {
			responseShare = max(responseShare-over, 0)
		} else {
			argsShare = max(argsShare-over, 0)
		}
	}
	return out, true
}

// jsonString is a JSON string's value, or any other JSON as its text.
func jsonString(data json.RawMessage) string {
	var s string
	if json.Unmarshal(data, &s) == nil {
		return s
	}
	return string(data)
}

// unwrapResponse decodes a tool response through the envelopes it travels in:
// a runtime's {result, isError}, an MCP CallToolResult with one text content,
// and JSON encoded as a string. It answers the innermost value and whether any
// layer reported an error.
func unwrapResponse(v any) (any, bool) {
	failed := false
	for range maxUnwrapDepth {
		next, layerFailed, ok := unwrapOnce(v)
		failed = failed || layerFailed
		if !ok {
			break
		}
		v = next
	}
	if m, ok := v.(map[string]any); ok && reportsError(m["error"]) {
		failed = true
	}
	return v, failed
}

// reportsError says whether an error field holds an error: true, a message or
// an object with content, not false, "", null or {}.
func reportsError(v any) bool {
	switch e := v.(type) {
	case bool:
		return e
	case string:
		return e != ""
	case map[string]any:
		return len(e) > 0
	case []any:
		return len(e) > 0
	}
	return false
}

func unwrapOnce(v any) (next any, failed, ok bool) {
	switch value := v.(type) {
	case string:
		s := strings.TrimSpace(value)
		if s == "" || (s[0] != '{' && s[0] != '[') {
			return v, false, false
		}
		var out any
		if json.Unmarshal([]byte(s), &out) != nil {
			return v, false, false
		}
		return out, false, true
	case map[string]any:
		failed, _ = value["isError"].(bool)
		if content, isList := value["content"].([]any); isList && len(content) == 1 && onlyKeys(value, "content", "isError", "structuredContent", "_meta") {
			if item, isMap := content[0].(map[string]any); isMap && item["type"] == "text" {
				if text, isText := item["text"].(string); isText {
					return text, failed, true
				}
			}
		}
		if result, has := value["result"]; has && onlyKeys(value, "result", "isError") {
			return result, failed, true
		}
		return v, failed, false
	}
	return v, false, false
}

func onlyKeys(m map[string]any, keys ...string) bool {
	for k := range m {
		if !slices.Contains(keys, k) {
			return false
		}
	}
	return true
}

func partsText(parts a2atype.ContentParts) string {
	var texts []string
	for _, part := range parts {
		if part == nil {
			continue
		}
		if text := part.Text(); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

func jsonText(v any) string {
	if v == nil {
		return ""
	}
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
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
	out.Messages = nil
	if jsonSize(out) <= budget {
		return out, true
	}
	out.ToolCalls = nil
	rest := budget - jsonSize(Turn{Index: out.Index, TaskID: out.TaskID, State: out.State, Timestamp: out.Timestamp, Artifacts: out.Artifacts, Clipped: true})
	for userMax, replyMax := rest/4, rest*3/4; replyMax > 0; userMax, replyMax = userMax/2, replyMax/2 {
		out.User, out.Reply = clipText(in.User, userMax), clipText(in.Reply, replyMax)
		if jsonSize(out) <= budget {
			return out, true
		}
	}
	return out, false
}

// fitMessage cuts a message that alone exceeds budget: its texts are clipped
// to a share each, and its parts dropped when that is not enough.
func fitMessage(in TurnMessage, budget int) TurnMessage {
	out := in
	out.Clipped = true
	out.Parts = slices.Clone(in.Parts)
	share := budget / (len(out.Parts) + 1)
	for i, part := range out.Parts {
		if part.Text != "" {
			out.Parts[i].Text = clipText(part.Text, share)
		}
	}
	if jsonSize(out) > budget {
		out.Parts = nil
	}
	return out
}
