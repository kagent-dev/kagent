package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/config"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

type recordingSink struct {
	id        string
	texts     []string
	calls     []runtime.ToolCall
	results   []runtime.ToolResult
	callReady chan struct{}
}

func (s *recordingSink) SessionStarted(v runtime.SessionStarted) error {
	s.id = v.ContinuationID
	return nil
}
func (s *recordingSink) TextDelta(v runtime.TextDelta) error {
	s.texts = append(s.texts, v.Text)
	return nil
}
func (s *recordingSink) ToolCall(v runtime.ToolCall) error {
	s.calls = append(s.calls, v)
	if s.callReady != nil {
		close(s.callReady)
		s.callReady = nil
	}
	return nil
}
func (s *recordingSink) ToolResult(v runtime.ToolResult) error {
	s.results = append(s.results, v)
	return nil
}
func (s *recordingSink) ModelCall(runtime.ModelCall) {}

func testRunner(t *testing.T, mode string) *Runner {
	t.Helper()
	c, err := config.Parse([]byte(`{"model":{"type":"openai","model":"mock","base_url":"http://localhost:1234/v1"},"instruction":"system","runtime":{"control_plane_url":"http://controller:8083"}}`))
	require.NoError(t, err)
	executable, err := os.Executable()
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "dsh")
	quoted := "'" + strings.ReplaceAll(executable, "'", "'\\''") + "'"
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nexec "+quoted+" -test.run=TestACPHelperProcess -- \"$@\"\n"), 0700))
	r, err := New(c, path, t.TempDir(), append(os.Environ(), "KAGENT_DSH_TEST_HELPER=1", "KAGENT_DSH_TEST_MODE="+mode))
	require.NoError(t, err)
	require.NoError(t, r.Validate(context.Background()))
	return r
}

func TestTurnsResumeAndEmitToolResults(t *testing.T) {
	r := testRunner(t, "")
	for _, id := range []string{"", "session-test"} {
		sink := &recordingSink{}
		out, err := r.Run(t.Context(), runtime.Turn{Prompt: "work", ContinuationID: id}, sink)
		require.NoError(t, err)
		require.Nil(t, out.Pending)
		require.Nil(t, out.Failure)
		require.Equal(t, "session-test", sink.id)
		require.Equal(t, []string{"finished"}, sink.texts)
		require.Len(t, sink.calls, 1)
		require.Equal(t, "read", sink.calls[0].Name)
		require.Len(t, sink.results, 1)
		require.False(t, sink.results[0].IsError)
	}
}

func TestPermissionResumeAndCancel(t *testing.T) {
	for _, mode := range []string{"allow", "reject", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			r := testRunner(t, "approval")
			sink := &recordingSink{}
			out, err := r.Run(t.Context(), runtime.Turn{Prompt: "work"}, sink)
			require.NoError(t, err)
			require.NotNil(t, out.Pending)
			request := out.Pending.Request().(*runtime.ApprovalRequest)
			require.Equal(t, "tool-1", request.CallID)
			if mode == "cancel" {
				require.NoError(t, out.Pending.Cancel(t.Context(), sink))
				return
			}
			out, err = out.Pending.Resume(t.Context(), &runtime.ApprovalDecision{ID: request.ID, Approved: mode == "allow"}, sink)
			require.NoError(t, err)
			require.Nil(t, out.Pending)
			require.Equal(t, mode == "reject", sink.results[0].IsError)
		})
	}
}

func TestActiveCancellationReapsProcessAndAllowsNextTurn(t *testing.T) {
	r := testRunner(t, "hang")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := make(chan struct{})
	sink := &recordingSink{callReady: ready}
	done := make(chan error, 1)
	go func() { _, err := r.Run(ctx, runtime.Turn{Prompt: "work"}, sink); done <- err }()
	select {
	case <-ready:
		cancel()
	case <-time.After(10 * time.Second):
		t.Fatal("prompt did not start")
	}
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("cancellation did not settle")
	}
	r.environment = append(r.environment, "KAGENT_DSH_TEST_MODE=")
	_, err := r.Run(t.Context(), runtime.Turn{Prompt: "next", ContinuationID: "session-test"}, &recordingSink{})
	require.NoError(t, err)
}

func TestRejectsWrongSessionAndSanitizesRemoteErrors(t *testing.T) {
	for _, mode := range []string{"wrong-session", "remote-error"} {
		t.Run(mode, func(t *testing.T) {
			out, err := testRunner(t, mode).Run(t.Context(), runtime.Turn{Prompt: "work"}, &recordingSink{})
			if mode == "remote-error" {
				require.NoError(t, err)
				require.NotNil(t, out.Failure)
				require.NotContains(t, out.Failure.Message, "secret")
			} else {
				require.Error(t, err)
			}
		})
	}
}

// A real subprocess exercises framing, request correlation, EOF cleanup and cancellation.
func TestACPHelperProcess(t *testing.T) {
	if os.Getenv("KAGENT_DSH_TEST_HELPER") != "1" {
		return
	}
	for _, arg := range os.Args {
		if arg == "--version" {
			fmt.Println(config.Version)
			os.Exit(0)
		}
	}
	mode := os.Getenv("KAGENT_DSH_TEST_MODE")
	writer := bufio.NewWriter(os.Stdout)
	write := func(m message) {
		data, err := json.Marshal(m)
		if err != nil {
			os.Exit(2)
		}
		fmt.Fprintln(writer, string(data))
		if err := writer.Flush(); err != nil {
			os.Exit(3)
		}
	}
	result := func(id json.RawMessage, value any) {
		data, _ := json.Marshal(value)
		write(message{JSONRPC: "2.0", ID: id, Result: data})
	}
	notify := func(u any) {
		data, _ := json.Marshal(struct {
			SessionID string `json:"sessionId"`
			Update    any    `json:"update"`
		}{SessionID: "session-test", Update: u})
		write(message{JSONRPC: "2.0", Method: "session/update", Params: data})
	}
	finish := func(failed bool) {
		status := "completed"
		if failed {
			status = "failed"
		}
		notify(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "tool-1", "status": status, "content": []any{}})
		notify(map[string]any{"sessionUpdate": "agent_message_chunk", "content": content{Type: "text", Text: "finished"}})
		result(json.RawMessage(promptID), map[string]string{"stopReason": "end_turn"})
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var m message
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			os.Exit(4)
		}
		switch m.Method {
		case "initialize":
			result(m.ID, json.RawMessage(`{"protocolVersion":1,"agentCapabilities":{"mcpCapabilities":{"http":true},"sessionCapabilities":{"resume":{}}}}`))
		case "session/new":
			result(m.ID, map[string]string{"sessionId": "session-test"})
		case "session/resume":
			var p sessionParams
			_ = json.Unmarshal(m.Params, &p)
			if p.SessionID != "session-test" {
				os.Exit(5)
			}
			result(m.ID, struct{}{})
		case "session/prompt":
			if mode == "remote-error" {
				write(message{JSONRPC: "2.0", ID: m.ID, Error: &rpcError{Code: -32603, Message: "secret credential"}})
				continue
			}
			if mode == "wrong-session" {
				write(message{JSONRPC: "2.0", Method: "session/update", Params: json.RawMessage(`{"sessionId":"other","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"wrong"}}}`)})
				continue
			}
			notify(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "tool-1", "title": "read", "status": "in_progress", "rawInput": map[string]string{"path": "file"}})
			if mode == "approval" {
				write(message{JSONRPC: "2.0", ID: json.RawMessage("99"), Method: "session/request_permission", Params: json.RawMessage(`{"sessionId":"session-test","toolCall":{"toolCallId":"tool-1"},"options":[{"optionId":"allow-once","kind":"allow_once"},{"optionId":"reject-once","kind":"reject_once"}]}`)})
			} else if mode != "hang" {
				finish(false)
			}
		case "session/cancel":
			result(json.RawMessage(promptID), map[string]string{"stopReason": "cancelled"})
		case "session/close":
			result(m.ID, struct{}{})
		case "":
			var response permissionResponse
			_ = json.Unmarshal(m.Result, &response)
			if response.Outcome.Outcome != "cancelled" {
				finish(response.Outcome.OptionID == "reject-once")
			}
		default:
			os.Exit(6)
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, os.ErrClosed) {
		os.Exit(7)
	}
	os.Exit(0)
}
