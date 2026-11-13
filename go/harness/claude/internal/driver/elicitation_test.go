package driver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

const elicitationSession = "11111111-1111-4111-8111-111111111111"

func testElicitationRecord() deferredRecord {
	return deferredRecord{
		Version: 1, TaskID: "task", ContextID: "context", SessionID: elicitationSession, RequestID: "request", Phase: phaseWaiting,
		Elicitation: &runtime.URLElicitationRequest{ID: "request", Server: "tickets", Message: "Sign in to tickets", URL: "https://tickets.example.com/signin?state=secret", ElicitationID: "elicitation"},
	}
}

func urlElicitationHook() elicitationHookInput {
	return elicitationHookInput{SessionID: elicitationSession, Event: "Elicitation", Server: "tickets", Message: "Sign in to tickets", Mode: "url", URL: "https://tickets.example.com/signin?state=secret", ElicitationID: "elicitation"}
}

func postBroker(t *testing.T, b *ApprovalBroker, path string, input any, output any) {
	t.Helper()
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, strings.TrimSuffix(b.URL(), "/mcp")+path, bytes.NewReader(raw))
	require.NoError(t, err)
	for name, value := range b.Headers() {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NoError(t, json.NewDecoder(response.Body).Decode(output))
}

func postElicitation(t *testing.T, b *ApprovalBroker, input elicitationHookInput) string {
	t.Helper()
	var output elicitationHookOutput
	postBroker(t, b, "/elicitation", input, &output)
	require.Equal(t, "Elicitation", output.Specific.Event)
	return output.Specific.Action
}

func TestSettingsInstallTheElicitationHook(t *testing.T) {
	b := newTestBroker(t)
	raw, err := b.SettingsJSON()
	require.NoError(t, err)
	var settings struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal(raw, &settings))
	hooks := settings.Hooks["Elicitation"]
	require.Len(t, hooks, 1)
	require.Equal(t, "*", hooks[0].Matcher)
	require.Len(t, hooks[0].Hooks, 1)
	require.Equal(t, "http", hooks[0].Hooks[0].Type)
	require.Equal(t, strings.TrimSuffix(b.URL(), "/mcp")+"/elicitation", hooks[0].Hooks[0].URL)
}

func TestElicitationHookKeepsOnlyURLRequests(t *testing.T) {
	for name, test := range map[string]struct {
		change   func(*elicitationHookInput)
		action   string
		observed bool
	}{
		"url":           {change: func(*elicitationHookInput) {}, action: apia2a.URLElicitationCancel, observed: true},
		"form":          {change: func(i *elicitationHookInput) { i.Mode, i.URL = "form", "" }, action: apia2a.URLElicitationDecline},
		"script URL":    {change: func(i *elicitationHookInput) { i.URL = "javascript:alert(1)" }, action: apia2a.URLElicitationDecline},
		"relative URL":  {change: func(i *elicitationHookInput) { i.URL = "/signin" }, action: apia2a.URLElicitationDecline},
		"no server":     {change: func(i *elicitationHookInput) { i.Server = "" }, action: apia2a.URLElicitationDecline},
		"no session":    {change: func(i *elicitationHookInput) { i.SessionID = "" }, action: apia2a.URLElicitationDecline},
		"other event":   {change: func(i *elicitationHookInput) { i.Event = "ElicitationResult" }, action: apia2a.URLElicitationDecline},
		"loopback http": {change: func(i *elicitationHookInput) { i.URL = "http://localhost:8080/signin" }, action: apia2a.URLElicitationCancel, observed: true},
		"remote http":   {change: func(i *elicitationHookInput) { i.URL = "http://tickets.example.com/signin" }, action: apia2a.URLElicitationDecline},
	} {
		t.Run(name, func(t *testing.T) {
			b := newTestBroker(t)
			input := urlElicitationHook()
			test.change(&input)
			require.Equal(t, test.action, postElicitation(t, b, input))
			observed := b.takeElicitation(input.SessionID)
			if !test.observed {
				require.Nil(t, observed)
				return
			}
			require.Equal(t, &observedElicitation{SessionID: input.SessionID, Server: input.Server, Message: input.Message, URL: input.URL, ElicitationID: input.ElicitationID}, observed)
		})
	}
}

func TestElicitationHookKeepsTheFirstURLRequest(t *testing.T) {
	b := newTestBroker(t)
	first := urlElicitationHook()
	require.Equal(t, apia2a.URLElicitationCancel, postElicitation(t, b, first))
	second := first
	second.Server, second.URL = "docs", "https://docs.example.com/signin"
	require.Equal(t, apia2a.URLElicitationCancel, postElicitation(t, b, second))
	observed := b.takeElicitation(elicitationSession)
	require.NotNil(t, observed)
	require.Equal(t, first.URL, observed.URL)
	require.Nil(t, b.takeElicitation(elicitationSession))
}

func TestElicitationStopsTheTurn(t *testing.T) {
	b := newTestBroker(t)
	require.Equal(t, apia2a.URLElicitationCancel, postElicitation(t, b, urlElicitationHook()))
	for _, tool := range []string{"Bash", "mcp__protected__write"} {
		output := postHook(t, b, hookInput{SessionID: elicitationSession, Event: "PreToolUse", ToolID: "call-" + tool, ToolName: tool, Input: map[string]any{}})
		require.Equal(t, "deny", output.Specific.Decision, tool)
		require.NotNil(t, output.Continue, tool)
		require.False(t, *output.Continue, tool)
	}
	var post hookOutput
	postBroker(t, b, "/failure", hookInput{SessionID: elicitationSession, Event: "PostToolUse", ToolID: "call", ToolName: "mcp__tickets__list"}, &post)
	require.NotNil(t, post.Continue)
	require.False(t, *post.Continue)

	other := postHook(t, b, hookInput{SessionID: "other", Event: "PreToolUse", ToolID: "call", ToolName: "Bash", Input: map[string]any{}})
	require.Nil(t, other.Continue)
	require.Empty(t, other.Specific.Decision)
}

// elicitingClaude reports its session, waits until the test has called the
// Elicitation hook and then ends the turn, as Claude does after the hooks
// stopped it.
func elicitingClaude(t *testing.T) (directory, executable string) {
	t.Helper()
	return writeFakeClaude(t, "#!/bin/sh\n"+
		"printf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\""+elicitationSession+"\"}'\n"+
		"touch \"$READY\"\n"+
		"while [ ! -f \"$GO\" ]; do sleep 0.02; done\n"+
		"printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"session_id\":\""+elicitationSession+"\"}'\n")
}

func TestURLElicitationParksOnceTheProcessEnds(t *testing.T) {
	b := newTestBroker(t)
	directory, executable := elicitingClaude(t)
	ready, proceed := filepath.Join(directory, "ready"), filepath.Join(directory, "go")
	d := NewProcessDriver(ProcessConfig{Executable: executable, Workspace: directory, ApprovalBroker: b, Environment: []string{"READY=" + ready, "GO=" + proceed}, MaxEventBytes: 1 << 20, MaxStderrBytes: 1024, InterruptGrace: time.Second})
	type result struct {
		outcome runtime.Outcome
		err     error
	}
	done := make(chan result, 1)
	go func() {
		outcome, err := d.Run(t.Context(), runtime.Turn{TaskID: "task", ContextID: "context", Prompt: "list my tickets"}, &recordingSink{})
		done <- result{outcome, err}
	}()
	require.Eventually(t, func() bool { _, err := os.Stat(ready); return err == nil }, 10*time.Second, 10*time.Millisecond)
	require.Equal(t, apia2a.URLElicitationCancel, postElicitation(t, b, urlElicitationHook()))
	require.NoError(t, os.WriteFile(proceed, nil, 0o600))
	ran := <-done
	require.NoError(t, ran.err)
	outcome := ran.outcome
	require.NotNil(t, outcome.Pending)
	request, ok := outcome.Pending.Request().(*runtime.URLElicitationRequest)
	require.True(t, ok)
	require.NotEmpty(t, request.ID)
	require.Equal(t, &runtime.URLElicitationRequest{ID: request.ID, Server: "tickets", Message: "Sign in to tickets", URL: "https://tickets.example.com/signin?state=secret", ElicitationID: "elicitation"}, request)

	saved, err := loadDeferred(b.directory, 1<<20)
	require.NoError(t, err)
	require.Equal(t, phaseWaiting, saved.Phase)
	require.Equal(t, elicitationSession, saved.SessionID)
	taskID, contextID, restored, err := NewProcessDriver(d.config).RestorePending()
	require.NoError(t, err)
	require.Equal(t, "task", taskID)
	require.Equal(t, "context", contextID)
	require.Equal(t, request, restored.Request())
}

func TestURLElicitationResumesWithThePersonsAnswer(t *testing.T) {
	for action, want := range map[string]string{
		apia2a.URLElicitationAccept:  "The person completed the interaction that the MCP server \\\"tickets\\\" asked for at a URL. Retry the tool call that needed it.",
		apia2a.URLElicitationDecline: "The person declined the interaction that the MCP server \\\"tickets\\\" asked for at a URL",
		apia2a.URLElicitationCancel:  "The person canceled the interaction that the MCP server \\\"tickets\\\" asked for at a URL",
	} {
		t.Run(action, func(t *testing.T) {
			b := newTestBroker(t)
			r := testElicitationRecord()
			require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
			directory, executable := writeFakeClaude(t, "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE\"\ncat > \"$CAPTURE.stdin\"\n"+
				"printf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\""+elicitationSession+"\"}' '{\"type\":\"result\",\"subtype\":\"success\",\"session_id\":\""+elicitationSession+"\"}'\n")
			capture := filepath.Join(directory, "capture")
			d := NewProcessDriver(ProcessConfig{Executable: executable, Workspace: directory, ApprovalBroker: b, Environment: []string{"CAPTURE=" + capture}, MaxEventBytes: 1 << 20, MaxStderrBytes: 1024, InterruptGrace: time.Second})
			_, _, pending, err := d.RestorePending()
			require.NoError(t, err)
			outcome, err := pending.Resume(t.Context(), &runtime.URLElicitationResponse{ID: r.RequestID, Action: action}, &recordingSink{})
			require.NoError(t, err)
			require.Nil(t, outcome.Pending)
			require.Nil(t, outcome.Failure)

			args, err := os.ReadFile(capture)
			require.NoError(t, err)
			require.Contains(t, string(args), "--resume\n"+elicitationSession+"\n")
			input, err := os.ReadFile(capture + ".stdin")
			require.NoError(t, err)
			require.Contains(t, string(input), want)
			require.NotContains(t, string(input), "tickets.example.com")
			saved, err := loadDeferred(b.directory, 1<<20)
			require.NoError(t, err)
			require.Equal(t, phaseComplete, saved.Phase)
			require.Equal(t, &runtime.URLElicitationResponse{ID: r.RequestID, Action: action}, saved.Action)
			_, _, restored, err := NewProcessDriver(d.config).RestorePending()
			require.NoError(t, err)
			require.Nil(t, restored)
		})
	}
}

func TestURLElicitationResumeRejectsAnotherAnswer(t *testing.T) {
	for name, response := range map[string]runtime.InputResponse{
		"other request": &runtime.URLElicitationResponse{ID: "other", Action: apia2a.URLElicitationAccept},
		"bad action":    &runtime.URLElicitationResponse{ID: "request", Action: "maybe"},
		"approval":      &runtime.ApprovalDecision{ID: "request", Approved: true},
	} {
		t.Run(name, func(t *testing.T) {
			b := newTestBroker(t)
			r := testElicitationRecord()
			require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
			d := NewProcessDriver(ProcessConfig{Executable: "must-not-run", ApprovalBroker: b, MaxEventBytes: 1 << 20})
			_, _, pending, err := d.RestorePending()
			require.NoError(t, err)
			_, err = pending.Resume(t.Context(), response, &recordingSink{})
			require.Error(t, err)
			saved, err := loadDeferred(b.directory, 1<<20)
			require.NoError(t, err)
			require.Equal(t, &r, saved)
		})
	}
}

func TestURLElicitationLaunchFailureKeepsItWaiting(t *testing.T) {
	b := newTestBroker(t)
	r := testElicitationRecord()
	require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
	d := NewProcessDriver(ProcessConfig{Executable: filepath.Join(t.TempDir(), "missing-claude"), Workspace: t.TempDir(), ApprovalBroker: b, MaxEventBytes: 1 << 20, MaxStderrBytes: 1024, InterruptGrace: time.Second})
	_, _, pending, err := d.RestorePending()
	require.NoError(t, err)
	outcome, err := pending.Resume(t.Context(), &runtime.URLElicitationResponse{ID: r.RequestID, Action: apia2a.URLElicitationAccept}, &recordingSink{})
	require.NoError(t, err)
	require.Equal(t, pending, outcome.Pending)
	saved, err := loadDeferred(b.directory, 1<<20)
	require.NoError(t, err)
	require.Equal(t, &r, saved)
}

func TestURLElicitationFailedResumeAbandonsSession(t *testing.T) {
	b := newTestBroker(t)
	r := testElicitationRecord()
	require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
	directory, executable := writeFakeClaude(t, "#!/bin/sh\nexit 17\n")
	d := NewProcessDriver(ProcessConfig{Executable: executable, Workspace: directory, ApprovalBroker: b, MaxEventBytes: 1 << 20, MaxStderrBytes: 1024, InterruptGrace: time.Second})
	_, _, pending, err := d.RestorePending()
	require.NoError(t, err)
	_, err = pending.Resume(t.Context(), &runtime.URLElicitationResponse{ID: r.RequestID, Action: apia2a.URLElicitationAccept}, &recordingSink{})
	require.ErrorIs(t, err, runtime.ErrSessionAbandoned)
	_, _, restored, err := NewProcessDriver(d.config).RestorePending()
	require.ErrorIs(t, err, runtime.ErrSessionAbandoned)
	require.Nil(t, restored)
}

func TestURLElicitationCancelWritesATombstone(t *testing.T) {
	b := newTestBroker(t)
	r := testElicitationRecord()
	require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
	d := NewProcessDriver(ProcessConfig{Executable: "must-not-run", ApprovalBroker: b, MaxEventBytes: 1 << 20})
	_, _, pending, err := d.RestorePending()
	require.NoError(t, err)
	require.NoError(t, pending.Cancel(t.Context(), &recordingSink{}))
	saved, err := loadDeferred(b.directory, 1<<20)
	require.NoError(t, err)
	require.Equal(t, phaseCanceled, saved.Phase)
	_, _, restored, err := d.RestorePending()
	require.NoError(t, err)
	require.Nil(t, restored)
	require.Error(t, pending.Cancel(t.Context(), &recordingSink{}))
}

func TestRestorePendingWithURLElicitation(t *testing.T) {
	for name, test := range map[string]struct {
		phase string
		check func(*testing.T, runtime.PendingTurn, error)
	}{
		"resuming is uncertain": {phase: phaseResuming, check: func(t *testing.T, pending runtime.PendingTurn, err error) {
			require.ErrorIs(t, err, runtime.ErrSessionAbandoned)
			require.Nil(t, pending)
		}},
		"abandoned": {phase: phaseAbandoned, check: func(t *testing.T, pending runtime.PendingTurn, err error) {
			require.ErrorIs(t, err, runtime.ErrSessionAbandoned)
			require.Nil(t, pending)
		}},
		"complete": {phase: phaseComplete, check: func(t *testing.T, pending runtime.PendingTurn, err error) {
			require.NoError(t, err)
			require.Nil(t, pending)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			b := newTestBroker(t)
			r := testElicitationRecord()
			r.Phase = test.phase
			if test.phase == phaseResuming || test.phase == phaseComplete {
				r.Action = &runtime.URLElicitationResponse{ID: r.RequestID, Action: apia2a.URLElicitationAccept}
			}
			require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
			_, _, pending, err := NewProcessDriver(ProcessConfig{ApprovalBroker: b, MaxEventBytes: 1 << 20}).RestorePending()
			test.check(t, pending, err)
		})
	}
}

// After accept, the model retries the call; a protected tool it calls then is
// deferred as a new call, not matched against the elicitation.
func TestResumingElicitationDefersAProtectedRetry(t *testing.T) {
	b := newTestBroker(t)
	r := testElicitationRecord()
	r.Phase, r.Action = phaseResuming, &runtime.URLElicitationResponse{ID: r.RequestID, Action: apia2a.URLElicitationAccept}
	require.NoError(t, saveDeferred(b.directory, 1<<20, &r))
	output := postHook(t, b, hookInput{SessionID: elicitationSession, Event: "PreToolUse", ToolID: "call-2", ToolName: "mcp__protected__write", Input: map[string]any{}})
	require.Equal(t, "defer", output.Specific.Decision)
	require.Nil(t, output.Continue)
}

func TestElicitationRecordRejectsInvalidState(t *testing.T) {
	accept := func(id string) *runtime.URLElicitationResponse {
		return &runtime.URLElicitationResponse{ID: id, Action: apia2a.URLElicitationAccept}
	}
	for name, change := range map[string]func(*deferredRecord){
		"version":             func(r *deferredRecord) { r.Version = 2 },
		"no session":          func(r *deferredRecord) { r.SessionID = "" },
		"no server":           func(r *deferredRecord) { r.Elicitation.Server = "" },
		"other request ID":    func(r *deferredRecord) { r.Elicitation.ID = "other" },
		"script URL":          func(r *deferredRecord) { r.Elicitation.URL = "javascript:alert(1)" },
		"remote http URL":     func(r *deferredRecord) { r.Elicitation.URL = "http://tickets.example.com/signin" },
		"with a tool":         func(r *deferredRecord) { r.Tool = DeferredTool{ID: "call", Name: "mcp__protected__write"} },
		"with a decision":     func(r *deferredRecord) { r.Decision = &runtime.ApprovalDecision{ID: r.RequestID, Approved: true} },
		"canceling":           func(r *deferredRecord) { r.Canceling = true },
		"waiting with action": func(r *deferredRecord) { r.Action = accept(r.RequestID) },
		"resuming no action":  func(r *deferredRecord) { r.Phase = phaseResuming },
		"unknown action": func(r *deferredRecord) {
			r.Phase, r.Action = phaseResuming, &runtime.URLElicitationResponse{ID: r.RequestID, Action: "maybe"}
		},
		"action for another request": func(r *deferredRecord) { r.Phase, r.Action = phaseResuming, accept("other") },
		"executing":                  func(r *deferredRecord) { r.Phase, r.Action = phaseExecuting, accept(r.RequestID) },
	} {
		t.Run(name, func(t *testing.T) {
			r := testElicitationRecord()
			change(&r)
			require.Error(t, r.validate())
			require.Error(t, saveDeferred(t.TempDir(), 1<<20, &r))
		})
	}
}

func TestDeferredToolRejectsAnElicitationAction(t *testing.T) {
	r := testRecord()
	r.Action = &runtime.URLElicitationResponse{ID: r.RequestID, Action: apia2a.URLElicitationAccept}
	require.Error(t, r.validate())
}

func TestElicitationRecordRejectsMalformedFile(t *testing.T) {
	r := testElicitationRecord()
	raw, err := json.Marshal(r)
	require.NoError(t, err)
	for name, data := range map[string][]byte{
		"trailing data": append(append([]byte(nil), raw...), []byte(" {}")...),
		"unknown field": []byte(strings.Replace(string(raw), `"version":1`, `"version":1,"extra":true`, 1)),
		"oversized":     append(append([]byte(nil), raw...), bytes.Repeat([]byte(" "), 1<<20)...),
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(directory, "claude-pending.json"), data, 0o600))
			_, err := loadDeferred(directory, 1<<20)
			require.Error(t, err)
		})
	}
}
