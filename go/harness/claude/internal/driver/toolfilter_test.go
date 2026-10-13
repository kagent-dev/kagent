package driver

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

const toolsListResult = `{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"echo","inputSchema":{"type":"object"}},{"name":"delete_all"},{"name":"get_time"}],"nextCursor":"next"}}`

// newMCPUpstream answers tools/list with three tools, in a JSON body or an
// event stream, and every other request with a fixed result. It counts the
// tools/call requests that reach it and keeps the body of the last.
func newMCPUpstream(t *testing.T, eventStream bool) (*httptest.Server, *atomic.Int32, *atomic.Value) {
	t.Helper()
	var calls atomic.Int32
	var lastBody atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		body, _ := io.ReadAll(request.Body)
		_ = json.Unmarshal(body, &message)
		reply := `{"jsonrpc":"2.0","id":` + string(message.ID) + `,"result":{"content":[{"type":"text","text":"done"}]}}`
		switch message.Method {
		case "tools/list":
			reply = strings.Replace(toolsListResult, "%s", string(message.ID), 1)
		case "tools/call":
			calls.Add(1)
			lastBody.Store(string(body))
		}
		if !eventStream {
			response.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(response, reply)
			return
		}
		response.Header().Set("Content-Type", "text/event-stream")
		// A notification event first, then the reply split over two data
		// lines between two of its members, as a server may frame it.
		half := strings.Index(reply, `"result"`)
		_, _ = io.WriteString(response, "event: message\r\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\r\n\r\n")
		_, _ = io.WriteString(response, "id: 7\nevent: message\ndata: "+reply[:half]+"\ndata: "+reply[half:]+"\n\n")
	}))
	t.Cleanup(server.Close)
	return server, &calls, &lastBody
}

func postForwarder(t *testing.T, forwarder *CredentialForwarder, name, body string) (*http.Response, string) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, forwarder.URL(name), strings.NewReader(body))
	require.NoError(t, err)
	for header, value := range forwarder.Headers() {
		request.Header.Set(header, value)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response, string(raw)
}

// listedTools returns the tool names of the tools/list result in a JSON body
// or in the data of an event stream's events.
func listedTools(t *testing.T, body string) []string {
	t.Helper()
	payloads := []string{body}
	if strings.Contains(body, "data:") {
		payloads = nil
		for event := range strings.SplitSeq(strings.ReplaceAll(body, "\r\n", "\n"), "\n\n") {
			var data []string
			for line := range strings.SplitSeq(event, "\n") {
				if value, ok := strings.CutPrefix(line, "data:"); ok {
					data = append(data, strings.TrimPrefix(value, " "))
				}
			}
			if len(data) != 0 {
				payloads = append(payloads, strings.Join(data, "\n"))
			}
		}
	}
	for _, payload := range payloads {
		var message struct {
			Result *struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
				NextCursor string `json:"nextCursor"`
			} `json:"result"`
		}
		require.NoError(t, json.Unmarshal([]byte(payload), &message), payload)
		if message.Result == nil {
			continue
		}
		require.Equal(t, "next", message.Result.NextCursor, "the result keeps its other fields")
		names := make([]string, 0, len(message.Result.Tools))
		for _, tool := range message.Result.Tools {
			names = append(names, tool.Name)
		}
		return names
	}
	t.Fatalf("no tools/list result in %q", body)
	return nil
}

func TestCredentialForwarderKeepsClaudeToTheToolSelection(t *testing.T) {
	for _, eventStream := range []bool{false, true} {
		name := map[bool]string{false: "JSON", true: "event stream"}[eventStream]
		t.Run(name, func(t *testing.T) {
			upstream, calls, lastBody := newMCPUpstream(t, eventStream)
			forwarder, err := NewCredentialForwarder(map[string]UpstreamMCPServer{
				"selected": {URL: upstream.URL + "/mcp", Tools: []string{"echo", "get_time"}},
				"whole":    {URL: upstream.URL + "/mcp"},
			}, 1<<20)
			require.NoError(t, err)
			t.Cleanup(func() { _ = forwarder.Close() })

			_, body := postForwarder(t, forwarder, "selected", `{"jsonrpc":"2.0","id":"list-1","method":"tools/list"}`)
			require.Equal(t, []string{"echo", "get_time"}, listedTools(t, body))
			_, body = postForwarder(t, forwarder, "whole", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
			require.Equal(t, []string{"echo", "delete_all", "get_time"}, listedTools(t, body), "a server without a selection is listed whole")

			response, body := postForwarder(t, forwarder, "selected", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"delete_all"}}`)
			require.Equal(t, http.StatusOK, response.StatusCode)
			require.JSONEq(t, `{"jsonrpc":"2.0","id":2,"error":{"code":-32602,"message":"Unknown tool: delete_all"}}`, body)
			require.Zero(t, calls.Load(), "a call of an unselected tool never reaches the server")

			response, _ = postForwarder(t, forwarder, "selected", `[{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo"}},{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"delete_all"}}]`)
			require.Equal(t, http.StatusForbidden, response.StatusCode)
			require.Zero(t, calls.Load(), "a batch calling an unselected tool never reaches the server")

			for _, refused := range []string{
				`not json`,
				`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"echo","NAME":"delete_all"}}`,
				`{"jsonrpc":"2.0","id":8,"Method":"tools/call","method":"ping","params":{"name":"delete_all"}}`,
				`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":7}}`,
				`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":"delete_all"}`,
				`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"delete_all"}}`,
				`[{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"echo"}}, 1]`,
			} {
				response, body := postForwarder(t, forwarder, "selected", refused)
				require.True(t, response.StatusCode != http.StatusOK || strings.Contains(body, "Unknown tool"), "%s answered %d %s", refused, response.StatusCode, body)
			}
			require.Zero(t, calls.Load(), "a request the forwarder cannot decide never reaches the server")

			// A duplicate name reaches the server once, as the forwarder read it.
			_, body = postForwarder(t, forwarder, "selected", `{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"delete_all","name":"echo"}}`)
			require.Contains(t, body, `"text":"done"`)
			require.Equal(t, int32(1), calls.Load())
			require.JSONEq(t, `{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"echo"}}`, lastBody.Load().(string))

			_, body = postForwarder(t, forwarder, "selected", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"echo"}}`)
			require.Contains(t, body, `"text":"done"`)
			_, _ = postForwarder(t, forwarder, "whole", `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"delete_all"}}`)
			require.Equal(t, int32(3), calls.Load(), "selected calls and calls of a whole server are forwarded")
		})
	}
}

func TestToolSelectionFiltersOnlyTheListedRequests(t *testing.T) {
	selection := newToolSelection([]string{"echo"})
	other := strings.Replace(toolsListResult, "%s", "9", 1)
	require.Equal(t, other, string(selection.filterMessages([]byte(other), map[string]struct{}{"1": {}})),
		"a result of another request is passed through unchanged")
	require.Equal(t, "not json", string(selection.filterMessages([]byte("not json"), map[string]struct{}{"1": {}})))

	batch := "[" + strings.Replace(toolsListResult, "%s", "1", 1) + "," + other + "]"
	var filtered []json.RawMessage
	require.NoError(t, json.Unmarshal(selection.filterMessages([]byte(batch), map[string]struct{}{"1": {}}), &filtered))
	require.Len(t, filtered, 2)
	require.Equal(t, []string{"echo"}, listedTools(t, string(filtered[0])))
	require.JSONEq(t, other, string(filtered[1]))
}

func TestEventStreamFilterBoundsAnEvent(t *testing.T) {
	filter := &eventStreamFilter{
		source: bufioReader("data: " + strings.Repeat("x", 64) + "\n\n"), closer: io.NopCloser(strings.NewReader("")),
		maxBytes: 32, filter: func(data []byte) []byte { return data },
	}
	_, err := io.ReadAll(filter)
	require.ErrorContains(t, err, "exceeds 32 bytes")
}

func bufioReader(s string) *bufio.Reader { return bufio.NewReader(strings.NewReader(s)) }
