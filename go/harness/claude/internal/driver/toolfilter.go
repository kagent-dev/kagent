package driver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

// unknownToolCode is the JSON-RPC error MCP servers answer a call of a tool
// they do not have with.
const unknownToolCode = -32602

// toolSelection is the tools of one server Claude may list and call. Claude
// Code itself exposes every tool a server lists, so the forwarder removes the
// others from the server's tools/list results and answers their tools/call
// requests itself.
type toolSelection map[string]struct{}

func newToolSelection(tools []string) toolSelection {
	if len(tools) == 0 {
		return nil
	}
	selection := make(toolSelection, len(tools))
	for _, tool := range tools {
		selection[tool] = struct{}{}
	}
	return selection
}

func (s toolSelection) allows(tool string) bool {
	_, ok := s[tool]
	return ok
}

// listRequestsKey carries the ids of the tools/list requests a forwarded
// request holds, whose results the response filters.
type listRequestsKey struct{}

// screenedMembers lists the members of a JSON-RPC message, and of a tools/call
// request's params, the selection is decided on. A spelling of one in another
// case is refused: a server whose decoder matches names case-insensitively
// would read it as the member the forwarder did not check.
var screenedMembers = map[string][]string{"": {"jsonrpc", "id", "method", "params"}, "params": {"name"}}

// screen reads a request Claude sends to a server with a tool selection and
// fails closed: a body that is not JSON-RPC is refused, a call of an
// unselected tool is answered by the forwarder itself, and anything else is
// forwarded re-encoded from what was checked, so a duplicate member the server
// might read differently never reaches it. The returned request carries the
// ids of its tools/list requests; false means the response is written.
func (s toolSelection) screen(response http.ResponseWriter, request *http.Request) (*http.Request, bool) {
	if request.Method != http.MethodPost {
		return request, true
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(response, "read MCP request: "+err.Error(), http.StatusRequestEntityTooLarge)
		return nil, false
	}
	messages, batch, err := splitMessages(body)
	if err != nil {
		http.Error(response, "a server with a tool selection takes JSON-RPC requests only", http.StatusBadRequest)
		return nil, false
	}
	lists := make(map[string]struct{})
	for i, raw := range messages {
		screened, err := s.screenMessage(raw)
		var refused unselectedToolError
		switch {
		case errors.As(err, &refused) && !batch:
			writeUnknownTool(response, refused.id, refused.tool)
			return nil, false
		case errors.As(err, &refused):
			http.Error(response, fmt.Sprintf("tool %q is not selected for this server", refused.tool), http.StatusForbidden)
			return nil, false
		case err != nil:
			http.Error(response, "MCP request refused: "+err.Error(), http.StatusBadRequest)
			return nil, false
		}
		messages[i] = screened.raw
		if screened.list {
			lists[compactID(screened.id)] = struct{}{}
		}
	}
	if batch {
		body, err = json.Marshal(messages)
	} else {
		body, err = json.Marshal(messages[0])
	}
	if err != nil {
		http.Error(response, "encode MCP request: "+err.Error(), http.StatusBadRequest)
		return nil, false
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	request.Header.Set("Content-Length", strconv.Itoa(len(body)))
	if len(lists) == 0 {
		return request, true
	}
	return request.WithContext(context.WithValue(request.Context(), listRequestsKey{}, lists)), true
}

type screenedMessage struct {
	raw  json.RawMessage
	id   json.RawMessage
	list bool
}

type unselectedToolError struct {
	id   json.RawMessage
	tool string
}

func (e unselectedToolError) Error() string { return fmt.Sprintf("tool %q is not selected", e.tool) }

// screenMessage decides one JSON-RPC message and returns it re-encoded: a
// tools/call of an unselected tool, and a message whose method, params or
// tool name is not unambiguous, are errors.
func (s toolSelection) screenMessage(raw json.RawMessage) (screenedMessage, error) {
	message, err := decodeMembers(raw, "")
	if err != nil {
		return screenedMessage{}, err
	}
	var method string
	if value, ok := message["method"]; ok && json.Unmarshal(value, &method) != nil {
		return screenedMessage{}, fmt.Errorf("method is not a string")
	}
	if method == "tools/call" {
		params, err := decodeMembers(message["params"], "params")
		if err != nil {
			return screenedMessage{}, err
		}
		var tool string
		if json.Unmarshal(params["name"], &tool) != nil {
			return screenedMessage{}, fmt.Errorf("tools/call names no tool")
		}
		if !s.allows(tool) {
			return screenedMessage{}, unselectedToolError{id: message["id"], tool: tool}
		}
		if message["params"], err = json.Marshal(params); err != nil {
			return screenedMessage{}, err
		}
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return screenedMessage{}, err
	}
	return screenedMessage{raw: encoded, id: message["id"], list: method == "tools/list" && len(message["id"]) != 0}, nil
}

// decodeMembers decodes a JSON object, refusing a spelling of one of the
// screened members of scope in another case.
func decodeMembers(raw json.RawMessage, scope string) (map[string]json.RawMessage, error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil || members == nil {
		return nil, fmt.Errorf("not a JSON object")
	}
	for name := range members {
		for _, screened := range screenedMembers[scope] {
			if name != screened && strings.EqualFold(name, screened) {
				return nil, fmt.Errorf("member %q is ambiguous with %q", name, screened)
			}
		}
	}
	return members, nil
}

func writeUnknownTool(response http.ResponseWriter, id json.RawMessage, tool string) {
	reply, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": unknownToolCode, "message": fmt.Sprintf("Unknown tool: %s", tool)},
	})
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Content-Length", strconv.Itoa(len(reply)))
	_, _ = response.Write(reply)
}

// filterResponse removes the unselected tools from the tools/list results of
// a response, a JSON body or an event stream.
func (s toolSelection) filterResponse(maxBytes int64) func(*http.Response) error {
	return func(response *http.Response) error {
		lists, _ := response.Request.Context().Value(listRequestsKey{}).(map[string]struct{})
		if len(lists) == 0 || response.StatusCode != http.StatusOK {
			return nil
		}
		mediaType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
		switch mediaType {
		case "application/json":
			body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
			_ = response.Body.Close()
			if err != nil {
				return fmt.Errorf("read MCP response: %w", err)
			}
			if int64(len(body)) > maxBytes {
				return fmt.Errorf("MCP response exceeds %d bytes", maxBytes)
			}
			body = s.filterMessages(body, lists)
			response.Body = io.NopCloser(bytes.NewReader(body))
			response.ContentLength = int64(len(body))
			response.Header.Set("Content-Length", strconv.Itoa(len(body)))
		case "text/event-stream":
			// The filtered stream's length is known only at its end.
			response.ContentLength = -1
			response.Header.Del("Content-Length")
			response.Body = &eventStreamFilter{
				source: bufio.NewReader(response.Body), closer: response.Body, maxBytes: maxBytes,
				filter: func(data []byte) []byte { return s.filterMessages(data, lists) },
			}
		}
		return nil
	}
}

// filterMessages returns raw with the unselected tools removed from the
// results of the listed tools/list requests; anything else is returned as is.
func (s toolSelection) filterMessages(raw []byte, lists map[string]struct{}) []byte {
	messages, batch, err := splitMessages(raw)
	if err != nil {
		return raw
	}
	changed := false
	for i, message := range messages {
		if filtered, ok := s.filterListResult(message, lists); ok {
			messages[i], changed = filtered, true
		}
	}
	if !changed {
		return raw
	}
	var out []byte
	if batch {
		out, err = json.Marshal(messages)
	} else {
		out, err = json.Marshal(messages[0])
	}
	if err != nil {
		return raw
	}
	return out
}

func (s toolSelection) filterListResult(raw json.RawMessage, lists map[string]struct{}) (json.RawMessage, bool) {
	var message map[string]json.RawMessage
	if json.Unmarshal(raw, &message) != nil {
		return nil, false
	}
	if _, listed := lists[compactID(message["id"])]; !listed || message["result"] == nil {
		return nil, false
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(message["result"], &result) != nil {
		return nil, false
	}
	var tools []json.RawMessage
	if json.Unmarshal(result["tools"], &tools) != nil {
		return nil, false
	}
	kept := make([]json.RawMessage, 0, len(tools))
	for _, tool := range tools {
		var described struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(tool, &described) == nil && s.allows(described.Name) {
			kept = append(kept, tool)
		}
	}
	var err error
	if result["tools"], err = json.Marshal(kept); err != nil {
		return nil, false
	}
	if message["result"], err = json.Marshal(result); err != nil {
		return nil, false
	}
	filtered, err := json.Marshal(message)
	return filtered, err == nil
}

// splitMessages returns the JSON-RPC messages of a body: one object, or the
// elements of a batch.
func splitMessages(raw []byte) ([]json.RawMessage, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) != 0 && trimmed[0] == '[' {
		var messages []json.RawMessage
		if err := json.Unmarshal(trimmed, &messages); err != nil {
			return nil, true, err
		}
		return messages, true, nil
	}
	var message json.RawMessage
	if err := json.Unmarshal(trimmed, &message); err != nil {
		return nil, false, err
	}
	return []json.RawMessage{message}, false, nil
}

func compactID(id json.RawMessage) string {
	var out bytes.Buffer
	if json.Compact(&out, id) != nil {
		return string(id)
	}
	return out.String()
}

// eventStreamFilter passes an event stream through one event at a time, the
// data of each event run through filter. An event's other fields and its
// framing are kept as the server sent them.
type eventStreamFilter struct {
	source   *bufio.Reader
	closer   io.Closer
	maxBytes int64
	filter   func([]byte) []byte
	pending  bytes.Buffer
	err      error
}

func (f *eventStreamFilter) Read(p []byte) (int, error) {
	for f.pending.Len() == 0 {
		if f.err != nil {
			return 0, f.err
		}
		f.err = f.nextEvent()
	}
	return f.pending.Read(p)
}

func (f *eventStreamFilter) Close() error { return f.closer.Close() }

// nextEvent reads the lines of one event up to and including the blank line
// that ends it, and queues the event with its data filtered.
func (f *eventStreamFilter) nextEvent() error {
	var lines [][]byte
	var data [][]byte
	var size int64
	for {
		line, err := f.source.ReadBytes('\n')
		size += int64(len(line))
		if size > f.maxBytes {
			return fmt.Errorf("MCP event exceeds %d bytes", f.maxBytes)
		}
		if len(line) != 0 {
			lines = append(lines, line)
			content := bytes.TrimRight(line, "\r\n")
			if value, ok := bytes.CutPrefix(content, []byte("data:")); ok {
				data = append(data, bytes.TrimPrefix(value, []byte(" ")))
			}
			if len(content) == 0 {
				break
			}
		}
		if err != nil {
			f.queue(lines, data)
			if errors.Is(err, io.EOF) && len(lines) != 0 {
				return nil
			}
			return err
		}
	}
	f.queue(lines, data)
	return nil
}

func (f *eventStreamFilter) queue(lines, data [][]byte) {
	if len(data) == 0 {
		for _, line := range lines {
			f.pending.Write(line)
		}
		return
	}
	joined := bytes.Join(data, []byte("\n"))
	filtered := f.filter(joined)
	if bytes.Equal(filtered, joined) {
		for _, line := range lines {
			f.pending.Write(line)
		}
		return
	}
	// The filtered data is compact JSON, one line: it replaces the event's
	// first data line, and its other data lines are dropped.
	written := false
	for _, line := range lines {
		if !bytes.HasPrefix(line, []byte("data:")) {
			f.pending.Write(line)
			continue
		}
		if !written {
			f.pending.WriteString("data: ")
			f.pending.Write(filtered)
			f.pending.WriteByte('\n')
			written = true
		}
	}
}
