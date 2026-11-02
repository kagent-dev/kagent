package driver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

func newTestBroker(t *testing.T) *ApprovalBroker {
	t.Helper()
	broker, err := NewApprovalBroker([]string{"protected"}, t.TempDir(), 1<<20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = broker.Close() })
	return broker
}

func testRecord() deferredRecord {
	return deferredRecord{Version: 1, TaskID: "task", ContextID: "context", SessionID: "session", RequestID: "request", Phase: "waiting", Tool: DeferredTool{ID: "call", Name: "mcp__protected__write", Input: map[string]any{"value": 7}}}
}

func TestDeferredTraceOriginSurvivesRestoreAndAnotherPause(t *testing.T) {
	b := newTestBroker(t)
	d := NewProcessDriver(ProcessConfig{ApprovalBroker: b, MaxEventBytes: 1 << 20})
	origin := runtime.NewTraceOrigin(trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled}))
	turn := runtime.Turn{TaskID: "task", ContextID: "context", Origin: origin}
	for _, id := range []string{"call-1", "call-2"} {
		_, err := d.parkDeferred(turn, "session", &DeferredTool{ID: id, Name: "mcp__protected__write", Input: map[string]any{"value": 7}})
		require.NoError(t, err)
		// A new driver reads the record instead of retaining the pending handle.
		restored := NewProcessDriver(ProcessConfig{ApprovalBroker: b, MaxEventBytes: 1 << 20})
		_, _, pending, err := restored.RestorePending()
		require.NoError(t, err)
		require.NotNil(t, pending)
		turn.Origin = pending.(*deferredTurn).TraceOrigin()
		require.Equal(t, origin.SpanContext().TraceID(), turn.Origin.SpanContext().TraceID())
		require.Equal(t, origin.SpanContext().SpanID(), turn.Origin.SpanContext().SpanID())
	}
}

func postHook(t *testing.T, b *ApprovalBroker, input hookInput) hookOutput {
	t.Helper()
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, strings.TrimSuffix(b.URL(), "/mcp")+"/hook", bytes.NewReader(raw))
	require.NoError(t, err)
	for name, value := range b.Headers() {
		req.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var output hookOutput
	require.NoError(t, json.NewDecoder(response.Body).Decode(&output))
	return output
}

func TestApprovalHookDefersAndConsumesSavedDecisionOnce(t *testing.T) {
	for _, approved := range []bool{true, false} {
		t.Run(map[bool]string{true: "approve", false: "deny"}[approved], func(t *testing.T) {
			b := newTestBroker(t)
			input := hookInput{SessionID: "session", Event: "PreToolUse", ToolID: "call", ToolName: "mcp__protected__write", Input: map[string]any{"value": 7}}
			require.Equal(t, "defer", postHook(t, b, input).Specific.Decision)
			r := testRecord()
			require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
			require.Equal(t, "defer", postHook(t, b, input).Specific.Decision)
			r.Phase = "resuming"
			r.Decision = &runtime.ApprovalDecision{ID: r.RequestID, Approved: approved}
			require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
			output := postHook(t, b, input)
			want := "deny"
			if approved {
				want = "allow"
			}
			require.Equal(t, want, output.Specific.Decision)
			saved, err := loadDeferred(b.directory, 1<<20)
			require.NoError(t, err)
			require.Equal(t, "executing", saved.Phase)
			require.Equal(t, "deny", postHook(t, b, input).Specific.Decision, "duplicate hook must not release the tool twice")
		})
	}
}

func TestApprovalHookAnswersSavedQuestion(t *testing.T) {
	b := newTestBroker(t)
	r := testRecord()
	r.Tool = DeferredTool{ID: "question", Name: "AskUserQuestion", Input: map[string]any{"questions": []any{map[string]any{"question": "Which environment?", "options": []any{map[string]any{"label": "staging"}}, "multiSelect": false}}}}
	r.Phase = "resuming"
	r.Answer = &runtime.AskUserResponse{ID: r.RequestID, Answers: [][]string{{"staging"}}}
	require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
	output := postHook(t, b, hookInput{SessionID: r.SessionID, Event: "PreToolUse", ToolID: r.Tool.ID, ToolName: r.Tool.Name, Input: r.Tool.Input})
	require.Equal(t, "allow", output.Specific.Decision)
	require.Equal(t, map[string]any{"Which environment?": "staging"}, output.Specific.UpdatedInput["answers"])
	require.NotNil(t, output.Specific.UpdatedInput["questions"])
}

func TestApprovalHookRejectsChangedToolAndCorruptData(t *testing.T) {
	b := newTestBroker(t)
	r := testRecord()
	r.Phase = "resuming"
	r.Decision = &runtime.ApprovalDecision{ID: r.RequestID, Approved: true}
	require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
	input := hookInput{SessionID: r.SessionID, Event: "PreToolUse", ToolID: r.Tool.ID, ToolName: r.Tool.Name, Input: map[string]any{"value": 8}}
	require.Equal(t, "deny", postHook(t, b, input).Specific.Decision)
	require.NoError(t, os.WriteFile(filepath.Join(b.directory, "claude-pending.json"), []byte(`{"broken":true}`), 0o600))
	require.Equal(t, "deny", postHook(t, b, input).Specific.Decision)
	driver := NewProcessDriver(ProcessConfig{ApprovalBroker: b, MaxEventBytes: 1 << 20})
	task, contextID, pending, err := driver.RestorePending()
	require.Error(t, err)
	require.Empty(t, task)
	require.Empty(t, contextID)
	require.Nil(t, pending)
}

func TestDeferredRecoveryRefusesUnfinishedReplay(t *testing.T) {
	for _, phase := range []string{"resuming", "executing", "released", "abandoned"} {
		t.Run(phase, func(t *testing.T) {
			b := newTestBroker(t)
			r := testRecord()
			r.Phase = phase
			r.Decision = &runtime.ApprovalDecision{ID: r.RequestID, Approved: true}
			require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
			driver := NewProcessDriver(ProcessConfig{ApprovalBroker: b, MaxEventBytes: 1 << 20})
			task, contextID, pending, err := driver.RestorePending()
			require.ErrorIs(t, err, runtime.ErrSessionAbandoned)
			require.Empty(t, task)
			require.Empty(t, contextID)
			require.Nil(t, pending)
		})
	}
}

func TestPermissionHostDeniesWithoutWaiting(t *testing.T) {
	b := newTestBroker(t)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: b.URL(), HTTPClient: &http.Client{Transport: &headerTransport{headers: b.Headers(), base: http.DefaultTransport}}, DisableStandaloneSSE: true}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: ApprovalToolName, Arguments: map[string]any{"tool_name": "mcp__protected__write", "tool_use_id": "call", "input": map[string]any{"value": 7}}})
	require.NoError(t, err)
	require.Contains(t, result.Content[0].(*mcp.TextContent).Text, `"behavior":"deny"`)
}

type headerTransport struct {
	headers map[string]string
	base    http.RoundTripper
}

func (t *headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range t.headers {
		r.Header.Set(k, v)
	}
	return t.base.RoundTrip(r)
}

var _ http.RoundTripper = (*headerTransport)(nil)

func TestApprovalHookRejectsUnauthenticatedRequest(t *testing.T) {
	b := newTestBroker(t)
	body := `{"session_id":"session","hook_event_name":"PreToolUse","tool_use_id":"call","tool_name":"mcp__protected__write","tool_input":{}}`
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, strings.TrimSuffix(b.URL(), "/mcp")+"/hook", strings.NewReader(body))
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
}

func TestDeferredRecordRejectsIncompleteState(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*deferredRecord)
	}{
		{name: "missing task ownership", mutate: func(r *deferredRecord) { r.TaskID = "" }},
		{name: "unknown phase", mutate: func(r *deferredRecord) { r.Phase = "unknown" }},
		{name: "resume without decision", mutate: func(r *deferredRecord) { r.Phase = "resuming" }},
		{name: "mismatched decision", mutate: func(r *deferredRecord) {
			r.Phase = "resuming"
			r.Decision = &runtime.ApprovalDecision{ID: "different", Approved: true}
		}},
		{name: "response on waiting record", mutate: func(r *deferredRecord) { r.Decision = &runtime.ApprovalDecision{ID: r.RequestID, Approved: true} }},
		{name: "canceling waiting record", mutate: func(r *deferredRecord) { r.Canceling = true }},
	} {
		t.Run(test.name, func(t *testing.T) { r := testRecord(); test.mutate(&r); require.Error(t, r.validate()) })
	}
}

func TestDeferredRecordRejectsSymlinkAndTrailingData(t *testing.T) {
	directory := t.TempDir()
	r := testRecord()
	require.NoError(t, saveDeferred(directory, 1<<20, &r))
	path := filepath.Join(directory, "claude-pending.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(data, []byte(` {}`)...), 0o600))
	_, err = loadDeferred(directory, 1<<20)
	require.Error(t, err)
	require.NoError(t, os.Remove(path))
	target := filepath.Join(directory, "target")
	require.NoError(t, os.WriteFile(target, data, 0o600))
	require.NoError(t, os.Symlink(target, path))
	_, err = loadDeferred(directory, 1<<20)
	require.Error(t, err)
}

func TestApprovalHookDoesNotReviveResolvedCalls(t *testing.T) {
	for _, phase := range []string{"complete", "canceled"} {
		t.Run(phase, func(t *testing.T) {
			b := newTestBroker(t)
			r := testRecord()
			r.Phase = phase
			if phase == "complete" {
				r.Decision = &runtime.ApprovalDecision{ID: r.RequestID, Approved: true}
			} else {
				r.Canceling = true
			}
			require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
			output := postHook(t, b, hookInput{SessionID: r.SessionID, Event: "PreToolUse", ToolID: r.Tool.ID, ToolName: r.Tool.Name, Input: r.Tool.Input})
			require.Equal(t, "deny", output.Specific.Decision)
			if phase == "canceled" {
				require.NotNil(t, output.Continue)
				require.False(t, *output.Continue)
			}
		})
	}
}

func TestApprovalHookPassesOrdinaryToolsAndBlocksCancellation(t *testing.T) {
	b := newTestBroker(t)
	input := hookInput{SessionID: "session", Event: "PreToolUse", ToolID: "ordinary", ToolName: "Bash", Input: map[string]any{"command": "true"}}
	require.Empty(t, postHook(t, b, input).Specific.Decision)
	r := testRecord()
	r.Canceling, r.Phase = true, "resuming"
	require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
	for _, id := range []string{"ordinary", r.Tool.ID} {
		input.ToolID = id
		output := postHook(t, b, input)
		require.Equal(t, "deny", output.Specific.Decision)
		require.NotNil(t, output.Continue)
		require.False(t, *output.Continue)
	}
}

func TestCancellationFailureHookRequestsStop(t *testing.T) {
	for _, event := range []string{"PostToolUseFailure", "PostToolUse"} {
		t.Run(event, func(t *testing.T) { testCancellationPostToolHook(t, event) })
	}
}

func testCancellationPostToolHook(t *testing.T, event string) {
	t.Helper()
	b := newTestBroker(t)
	r := testRecord()
	r.Canceling, r.Phase = true, "executing"
	require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
	raw, err := json.Marshal(hookInput{SessionID: r.SessionID, Event: event, ToolID: r.Tool.ID, ToolName: r.Tool.Name})
	require.NoError(t, err)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, strings.TrimSuffix(b.URL(), "/mcp")+"/failure", bytes.NewReader(raw))
	require.NoError(t, err)
	for name, value := range b.Headers() {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var output hookOutput
	require.NoError(t, json.NewDecoder(response.Body).Decode(&output))
	require.NotNil(t, output.Continue)
	require.False(t, *output.Continue)
}

func TestDeferredLaunchFailureKeepsOriginalInputWaiting(t *testing.T) {
	b := newTestBroker(t)
	r := testRecord()
	require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
	expected, err := loadDeferred(b.directory, 1<<20)
	require.NoError(t, err)
	d := NewProcessDriver(ProcessConfig{Executable: filepath.Join(t.TempDir(), "missing-claude"), Workspace: t.TempDir(), ApprovalBroker: b, MaxEventBytes: 1 << 20, MaxStderrBytes: 1024, InterruptGrace: time.Second})
	_, _, pending, err := d.RestorePending()
	require.NoError(t, err)
	for range 2 {
		outcome, err := pending.Resume(t.Context(), &runtime.ApprovalDecision{ID: r.RequestID, Approved: true}, &recordingSink{})
		require.NoError(t, err)
		require.NotNil(t, outcome.Pending)
		require.Equal(t, pending.Request(), outcome.Pending.Request())
		saved, err := loadDeferred(b.directory, 1<<20)
		require.NoError(t, err)
		require.Equal(t, expected, saved)
		// Reconstructing from disk retains the same approval, not uncertainty.
		_, _, pending, err = d.RestorePending()
		require.NoError(t, err)
	}
	// A failed cancellation launch must also preserve the retryable request.
	require.Error(t, pending.Cancel(t.Context(), &recordingSink{}))
	saved, err := loadDeferred(b.directory, 1<<20)
	require.NoError(t, err)
	require.Equal(t, expected, saved)
}

func TestDeferredFailureAbandonsSession(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		name := "resume"
		if cancel {
			name = "cancel"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestBroker(t)
			r := testRecord()
			require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
			directory := t.TempDir()
			executable := filepath.Join(directory, "claude")
			require.NoError(t, os.WriteFile(executable, []byte("#!/bin/sh\nexit 17\n"), 0o700))
			driver := NewProcessDriver(ProcessConfig{Executable: executable, Workspace: directory, ApprovalBroker: b, MaxEventBytes: 1 << 20, MaxStderrBytes: 1024, InterruptGrace: time.Second})
			_, _, pending, err := driver.RestorePending()
			require.NoError(t, err)
			if cancel {
				err = pending.Cancel(t.Context(), &recordingSink{})
			} else {
				_, err = pending.Resume(t.Context(), &runtime.ApprovalDecision{ID: r.RequestID, Approved: true}, &recordingSink{})
			}
			require.ErrorIs(t, err, runtime.ErrSessionAbandoned)
			saved, err := loadDeferred(b.directory, 1<<20)
			require.NoError(t, err)
			require.Equal(t, "abandoned", saved.Phase)
			restored := NewProcessDriver(ProcessConfig{ApprovalBroker: b, MaxEventBytes: 1 << 20})
			taskID, contextID, pending, err := restored.RestorePending()
			require.ErrorIs(t, err, runtime.ErrSessionAbandoned)
			require.Empty(t, taskID)
			require.Empty(t, contextID)
			require.Nil(t, pending)
		})
	}
}

func TestDeferredInvalidAnswerAbandonsSessionBeforeReplay(t *testing.T) {
	for _, answers := range [][][]string{{{}}, {{"PostgreSQL", "MySQL"}}} {
		t.Run(fmt.Sprint(answers), func(t *testing.T) {
			b := newTestBroker(t)
			r := testRecord()
			r.Tool.Name = "AskUserQuestion"
			r.Tool.Input = map[string]any{"questions": []any{map[string]any{"question": "Which database?", "multiSelect": false}}}
			require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
			d := NewProcessDriver(ProcessConfig{ApprovalBroker: b, MaxEventBytes: 1 << 20, Executable: "must-not-run"})
			_, _, pending, err := d.RestorePending()
			require.NoError(t, err)
			_, err = pending.Resume(t.Context(), &runtime.AskUserResponse{ID: r.RequestID, Answers: answers}, &recordingSink{})
			require.ErrorIs(t, err, runtime.ErrSessionAbandoned)
			fresh := NewProcessDriver(d.config)
			_, _, restored, err := fresh.RestorePending()
			require.ErrorIs(t, err, runtime.ErrSessionAbandoned)
			require.Nil(t, restored)
		})
	}
}

func TestApprovalPersistenceErrorsDenyAndLogCause(t *testing.T) {
	for _, failure := range []string{"read", "write", "hook"} {
		t.Run(failure, func(t *testing.T) {
			b := newTestBroker(t)
			r := testRecord()
			r.Phase, r.Decision = phaseExecuting, &runtime.ApprovalDecision{ID: r.RequestID, Approved: true}
			require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
			if failure == "write" {
				// Reads through a directory symlink succeed, but private writes
				// reject it before replacing the journal.
				link := filepath.Join(t.TempDir(), "journal")
				require.NoError(t, os.Symlink(b.directory, link))
				b.directory = link
			} else {
				require.NoError(t, os.WriteFile(filepath.Join(b.directory, "claude-pending.json"), []byte("{"), 0o600))
			}
			var logs bytes.Buffer
			logger, err := logging.New(&logs, "error")
			require.NoError(t, err)
			ctx := logging.IntoContext(t.Context(), logger)
			if failure == "hook" {
				request := httptest.NewRequest(http.MethodPost, "/hook", strings.NewReader(`{"session_id":"session","hook_event_name":"PreToolUse","tool_use_id":"call","tool_name":"mcp__protected__write"}`)).WithContext(ctx)
				response := httptest.NewRecorder()
				b.handleHook(response, request)
				require.Equal(t, http.StatusOK, response.Code)
				var output hookOutput
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &output))
				require.Equal(t, "deny", output.Specific.Decision)
			} else {
				result, _, err := b.handlePermission(ctx, nil, &permissionPromptInput{ToolName: r.Tool.Name, ToolUseID: r.Tool.ID, Input: r.Tool.Input})
				require.NoError(t, err)
				var output permissionPromptOutput
				require.NoError(t, json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &output))
				require.Equal(t, "deny", output.Behavior)
				require.NotContains(t, output.Message, "single tool call")
			}
			require.Contains(t, logs.String(), `"error":`)
			if failure == "write" {
				require.Contains(t, logs.String(), "persist Claude deferred input")
			} else {
				require.Contains(t, logs.String(), "decode Claude deferred input")
			}
			require.NotContains(t, logs.String(), b.token)
		})
	}
}
