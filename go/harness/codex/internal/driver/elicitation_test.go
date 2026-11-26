package driver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/harness/codex/config"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

const signInURL = "https://tickets.example.com/signin?state=secret"

func waitingElicitation() pendingRecord {
	return pendingRecord{Version: 1, TaskID: "task-1", ContextID: "context-1", ThreadID: "thread-1", Phase: phaseWaiting,
		Server: "tickets", Tool: "list", CallID: "call-1", Arguments: map[string]any{"project": "kd"},
		Elicitation: &runtime.URLElicitationRequest{ID: "elicitation-1", Server: "tickets", Message: "Sign in to tickets", URL: signInURL, ElicitationID: "e1"}}
}

// writeElicitingAppServer starts the call that asks for a sign-in, sends the
// given elicitation and checkpoints on interrupt. A turn carrying a tool output
// completes.
func writeElicitingAppServer(t *testing.T, executable, items, elicitation string) {
	t.Helper()
	script := `#!/bin/sh
read initialize
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read thread
printf '%s\n' "$thread" >> "$CAPTURE"
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
read -r turn
printf '%s\n' "$turn" >> "$CAPTURE"
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
case "$turn" in
  *toolOutput*)
    printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}}' ;;
  *)
` + items + `
    printf '%s\n' '` + elicitation + `'
    read -r reply
    printf '%s\n' "$reply" >> "$CAPTURE"
    case "$reply" in
      *turn/interrupt*)
        printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"interrupted"}}}'
        printf '%s\n' '{"id":4,"result":{}}' ;;
      *)
        printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}}' ;;
    esac
    ;;
esac
while read ignored; do :; done
if [ -n "$EXIT_CAPTURE" ]; then printf graceful > "$EXIT_CAPTURE"; fi
`
	require.NoError(t, os.WriteFile(executable, []byte(script), 0o700))
}

const ticketsCall = `    printf '%s\n' '{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"type":"mcpToolCall","id":"call-1","server":"tickets","tool":"list","arguments":{"project":"kd"},"status":"inProgress"}}}'`

func urlElicitationFrame(thread, mode, url string) string {
	params := map[string]any{"threadId": thread, "turnId": "turn-1", "serverName": "tickets", "mode": mode, "message": "Sign in to tickets", "_meta": nil}
	if mode == "url" {
		params["url"], params["elicitationId"] = url, "e1"
	} else {
		params["requestedSchema"] = map[string]any{"type": "object"}
	}
	if thread != "thread-1" {
		params["turnId"] = "child-turn"
	}
	frame, err := json.Marshal(map[string]any{"id": 7, "method": "mcpServer/elicitation/request", "params": params})
	if err != nil {
		panic(err)
	}
	return string(frame)
}

func TestURLElicitationParksTheCallAndResumesWithTheAnswer(t *testing.T) {
	for action, test := range map[string]struct {
		executes bool
		output   string
	}{
		apia2a.URLElicitationAccept:  {executes: true, output: "kd-1"},
		apia2a.URLElicitationDecline: {output: `The person declined the interaction that the MCP server \\\"tickets\\\" asked for at a URL`},
		apia2a.URLElicitationCancel:  {output: `The person canceled the interaction that the MCP server \\\"tickets\\\" asked for at a URL`},
	} {
		t.Run(action, func(t *testing.T) {
			directory := t.TempDir()
			executable, capture, exitCapture := filepath.Join(directory, "codex"), filepath.Join(directory, "requests"), filepath.Join(directory, "exit")
			writeElicitingAppServer(t, executable, ticketsCall, urlElicitationFrame("thread-1", "url", signInURL))
			var calls []string
			cfg := ProcessConfig{Executable: executable, Workspace: directory, Model: "model", Provider: "provider", PendingDir: directory,
				Environment: append(os.Environ(), "CAPTURE="+capture, "EXIT_CAPTURE="+exitCapture), MaxFrameBytes: 4096, MaxStderrBytes: 4096,
				InterruptGrace: time.Second, Tools: fakeToolCaller{call: func(_ context.Context, server, tool string, args map[string]any) (*mcp.CallToolResult, error) {
					raw, err := json.Marshal(args)
					require.NoError(t, err)
					calls = append(calls, server+"."+tool+" "+string(raw))
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "kd-1"}}}, nil
				}}}
			driver := NewProcessDriver(cfg)
			sink := &recordingSink{}
			outcome, err := driver.Run(t.Context(), runtime.Turn{TaskID: "task-1", ContextID: "context-1", Prompt: "list tickets"}, sink)
			require.NoError(t, err)
			require.NotNil(t, outcome.Pending)
			request, ok := outcome.Pending.Request().(*runtime.URLElicitationRequest)
			require.True(t, ok)
			require.Equal(t, &runtime.URLElicitationRequest{ID: request.ID, Server: "tickets", Message: "Sign in to tickets", URL: signInURL, ElicitationID: "e1"}, request)
			exit, err := os.ReadFile(exitCapture)
			require.NoError(t, err)
			require.Equal(t, "graceful", string(exit), "the native process exits before input-required")
			for _, result := range sink.results {
				require.NotEqual(t, "call-1", result.ID, "the interrupted call stays unresolved")
			}
			saved, err := driver.loadPending()
			require.NoError(t, err)
			require.Equal(t, "call-1", saved.CallID)
			require.Equal(t, map[string]any{"project": "kd"}, saved.Arguments)

			_, _, pending, err := NewProcessDriver(cfg).RestorePending()
			require.NoError(t, err)
			sink = &recordingSink{}
			outcome, err = pending.Resume(t.Context(), &runtime.URLElicitationResponse{ID: request.ID, Action: action}, sink)
			require.NoError(t, err)
			require.Nil(t, outcome.Pending)
			if test.executes {
				require.Equal(t, []string{`tickets.list {"project":"kd"}`}, calls)
			} else {
				require.Empty(t, calls)
			}
			require.Len(t, sink.results, 1)
			require.Equal(t, "call-1", sink.results[0].ID)
			require.Equal(t, !test.executes, sink.results[0].IsError)
			if !test.executes {
				require.Equal(t, runtime.ToolDeclined, sink.results[0].Status)
			}
			requests, err := os.ReadFile(capture)
			require.NoError(t, err)
			require.Contains(t, string(requests), `"method":"turn/interrupt"`)
			require.Contains(t, string(requests), `"toolOutput"`)
			require.Contains(t, string(requests), test.output)
			require.NotContains(t, string(requests[strings.Index(string(requests), `"toolOutput"`):]), "tickets.example.com", "the URL never reaches the model")
			saved, err = driver.loadPending()
			require.NoError(t, err)
			require.Equal(t, phaseComplete, saved.Phase)
		})
	}
}

func TestURLElicitationIsDeclinedWhenItCannotPark(t *testing.T) {
	secondCall := `    printf '%s\n' '{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"type":"mcpToolCall","id":"call-2","server":"tickets","tool":"list","arguments":{"project":"other"},"status":"inProgress"}}}'`
	childSpawn := `    printf '%s\n' '{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"type":"collabAgentToolCall","id":"spawn-1","tool":"spawnAgent","receiverThreadIds":["child-1"],"status":"inProgress"}}}'`
	for name, test := range map[string]struct {
		items, frame string
	}{
		"form":            {items: ticketsCall, frame: urlElicitationFrame("thread-1", "form", "")},
		"script URL":      {items: ticketsCall, frame: urlElicitationFrame("thread-1", "url", "javascript:alert(1)")},
		"remote http URL": {items: ticketsCall, frame: urlElicitationFrame("thread-1", "url", "http://tickets.example.com/signin")},
		"no active call":  {items: "", frame: urlElicitationFrame("thread-1", "url", signInURL)},
		"two calls":       {items: ticketsCall + "\n" + secondCall, frame: urlElicitationFrame("thread-1", "url", signInURL)},
		"child thread":    {items: childSpawn, frame: urlElicitationFrame("child-1", "url", signInURL)},
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			executable, capture := filepath.Join(directory, "codex"), filepath.Join(directory, "requests")
			writeElicitingAppServer(t, executable, test.items, test.frame)
			d := NewProcessDriver(ProcessConfig{Executable: executable, Workspace: directory, PendingDir: directory,
				Environment: append(os.Environ(), "CAPTURE="+capture), MaxFrameBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: time.Second,
				Tools: fakeToolCaller{call: func(context.Context, string, string, map[string]any) (*mcp.CallToolResult, error) {
					return nil, errors.New("must not run")
				}}})
			outcome, err := d.Run(t.Context(), runtime.Turn{TaskID: "task-1", ContextID: "context-1", Prompt: "list tickets"}, &recordingSink{})
			require.NoError(t, err)
			require.Nil(t, outcome.Pending)
			requests, err := os.ReadFile(capture)
			require.NoError(t, err)
			lines := strings.Split(strings.TrimSpace(string(requests)), "\n")
			var response struct {
				ID     int `json:"id"`
				Result struct {
					Action string `json:"action"`
				} `json:"result"`
			}
			require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &response))
			require.Equal(t, 7, response.ID)
			require.Equal(t, "decline", response.Result.Action)
			_, _, pending, err := d.RestorePending()
			require.NoError(t, err)
			require.Nil(t, pending)
		})
	}
}

func TestURLElicitationReparksWhileTheServerStillAsks(t *testing.T) {
	r := waitingElicitation()
	fresh := "https://tickets.example.com/signin?state=fresh"
	d := NewProcessDriver(ProcessConfig{PendingDir: t.TempDir(), MaxFrameBytes: 4096, Tools: fakeToolCaller{call: func(context.Context, string, string, map[string]any) (*mcp.CallToolResult, error) {
		return nil, &toolNotExecutedError{err: errors.New("MCP server tickets asked for input"), elicitation: &mcp.ElicitParams{Mode: "url", Message: "Sign in again", URL: fresh, ElicitationID: "e2"}}
	}}})
	require.NoError(t, d.savePending(&r))
	_, _, pending, err := d.RestorePending()
	require.NoError(t, err)
	outcome, err := pending.Resume(t.Context(), &runtime.URLElicitationResponse{ID: r.Elicitation.ID, Action: apia2a.URLElicitationAccept}, &recordingSink{})
	require.NoError(t, err)
	require.NotNil(t, outcome.Pending)
	request, ok := outcome.Pending.Request().(*runtime.URLElicitationRequest)
	require.True(t, ok)
	require.NotEqual(t, r.Elicitation.ID, request.ID)
	require.Equal(t, &runtime.URLElicitationRequest{ID: request.ID, Server: "tickets", Message: "Sign in again", URL: fresh, ElicitationID: "e2"}, request)
	saved, err := d.loadPending()
	require.NoError(t, err)
	want := r
	want.Elicitation = request
	require.Equal(t, want, *saved)
}

func TestURLElicitationReparksTheSameRequestWithoutANewOne(t *testing.T) {
	r := waitingElicitation()
	d := NewProcessDriver(ProcessConfig{PendingDir: t.TempDir(), MaxFrameBytes: 4096, Tools: fakeToolCaller{call: func(context.Context, string, string, map[string]any) (*mcp.CallToolResult, error) {
		return nil, &toolNotExecutedError{err: errors.New("deferred MCP header environment is unavailable")}
	}}})
	require.NoError(t, d.savePending(&r))
	_, _, pending, err := d.RestorePending()
	require.NoError(t, err)
	outcome, err := pending.Resume(t.Context(), &runtime.URLElicitationResponse{ID: r.Elicitation.ID, Action: apia2a.URLElicitationAccept}, &recordingSink{})
	require.NoError(t, err)
	require.NotNil(t, outcome.Pending)
	require.Equal(t, pending.Request(), outcome.Pending.Request())
	saved, err := d.loadPending()
	require.NoError(t, err)
	require.Equal(t, r, *saved)
}

// An approved call to a protected server that asks for a sign-in waits on the
// sign-in, and accept then runs the call without asking for approval again.
func TestApprovedCallWaitsOnTheServersURLElicitation(t *testing.T) {
	approval := waitingRecord()
	var calls []map[string]any
	signedIn := false
	d := NewProcessDriver(ProcessConfig{PendingDir: t.TempDir(), MaxFrameBytes: 4096, Tools: fakeToolCaller{call: func(_ context.Context, server, tool string, args map[string]any) (*mcp.CallToolResult, error) {
		require.Equal(t, "protected", server)
		require.Equal(t, "write", tool)
		calls = append(calls, args)
		if !signedIn {
			return nil, &toolNotExecutedError{err: errors.New("MCP server protected asked for input"), elicitation: &mcp.ElicitParams{Mode: "url", Message: "Sign in to protected", URL: signInURL, ElicitationID: "e1"}}
		}
		return nil, &toolNotExecutedError{err: errors.New("stop before the native turn")}
	}}})
	require.NoError(t, d.savePending(&approval))
	_, _, pending, err := d.RestorePending()
	require.NoError(t, err)
	outcome, err := pending.Resume(t.Context(), &runtime.ApprovalDecision{ID: approval.Approval.ID, Approved: true}, &recordingSink{})
	require.NoError(t, err)
	request, ok := outcome.Pending.Request().(*runtime.URLElicitationRequest)
	require.True(t, ok, "request = %T", outcome.Pending.Request())
	require.Equal(t, &runtime.URLElicitationRequest{ID: request.ID, Server: "protected", Message: "Sign in to protected", URL: signInURL, ElicitationID: "e1"}, request)
	saved, err := d.loadPending()
	require.NoError(t, err)
	require.Nil(t, saved.Approval)
	require.Nil(t, saved.Decision)
	require.Equal(t, "call-1", saved.CallID)
	require.Equal(t, approval.Approval.Args, saved.Arguments)

	signedIn = true
	_, _, restored, err := d.RestorePending()
	require.NoError(t, err)
	outcome, err = restored.Resume(t.Context(), &runtime.URLElicitationResponse{ID: request.ID, Action: apia2a.URLElicitationAccept}, &recordingSink{})
	require.NoError(t, err)
	require.Equal(t, request, outcome.Pending.Request())
	require.Equal(t, []map[string]any{approval.Approval.Args, approval.Approval.Args}, calls)
}

func TestURLElicitationRejectsAnotherAnswer(t *testing.T) {
	for name, response := range map[string]runtime.InputResponse{
		"other request": &runtime.URLElicitationResponse{ID: "other", Action: apia2a.URLElicitationAccept},
		"bad action":    &runtime.URLElicitationResponse{ID: "elicitation-1", Action: "maybe"},
		"approval":      &runtime.ApprovalDecision{ID: "elicitation-1", Approved: true},
	} {
		t.Run(name, func(t *testing.T) {
			r := waitingElicitation()
			d := NewProcessDriver(ProcessConfig{PendingDir: t.TempDir(), MaxFrameBytes: 4096, Tools: fakeToolCaller{call: func(context.Context, string, string, map[string]any) (*mcp.CallToolResult, error) {
				return nil, errors.New("must not run")
			}}})
			require.NoError(t, d.savePending(&r))
			_, _, pending, err := d.RestorePending()
			require.NoError(t, err)
			_, err = pending.Resume(t.Context(), response, &recordingSink{})
			require.Error(t, err)
		})
	}
}

func TestPendingElicitationRecordRejectsInvalidState(t *testing.T) {
	accept := &runtime.URLElicitationResponse{ID: "elicitation-1", Action: apia2a.URLElicitationAccept}
	decline := &runtime.URLElicitationResponse{ID: "elicitation-1", Action: apia2a.URLElicitationDecline}
	for name, change := range map[string]func(*pendingRecord){
		"also an approval":       func(r *pendingRecord) { r.Approval = waitingRecord().Approval },
		"no call":                func(r *pendingRecord) { r.CallID = "" },
		"no URL":                 func(r *pendingRecord) { r.Elicitation.URL = "" },
		"remote http URL":        func(r *pendingRecord) { r.Elicitation.URL = "http://tickets.example.com/signin" },
		"other server":           func(r *pendingRecord) { r.Server = "docs" },
		"waiting with an answer": func(r *pendingRecord) { r.Action = accept },
		"executing a decline":    func(r *pendingRecord) { r.Phase, r.Action = phaseExecuting, decline },
		"result without result":  func(r *pendingRecord) { r.Phase, r.Action = phaseResult, decline },
		"approval decision": func(r *pendingRecord) {
			r.Phase, r.Decision = phaseResult, &runtime.ApprovalDecision{ID: "elicitation-1", Approved: true}
		},
		"answer for another ID": func(r *pendingRecord) {
			r.Phase, r.Action, r.Result = phaseResult, &runtime.URLElicitationResponse{ID: "other", Action: apia2a.URLElicitationDecline}, &mcp.CallToolResult{}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := waitingElicitation()
			r.Elicitation = &runtime.URLElicitationRequest{ID: r.Elicitation.ID, Server: r.Elicitation.Server, URL: r.Elicitation.URL}
			change(&r)
			require.Error(t, r.validate())
		})
	}
	r := waitingElicitation()
	r.Phase, r.Action = phaseExecuting, accept
	require.NoError(t, r.validate())
}

func TestDeferredMCPTreatsAnotherElicitationAsNotExecuted(t *testing.T) {
	signIn := &mcp.ElicitParams{Mode: "url", Message: "Sign in", URL: signInURL, ElicitationID: "e1"}
	remote := &mcp.ElicitParams{Mode: "url", Message: "Sign in", URL: "http://tickets.example.com/signin"}
	form := &mcp.ElicitParams{Mode: "form", Message: "Project?", RequestedSchema: map[string]any{"type": "object"}}
	for name, test := range map[string]struct {
		handler func() (*mcp.CallToolResult, error)
		url     bool
	}{
		"-32042": {url: true, handler: func() (*mcp.CallToolResult, error) {
			return nil, mcp.URLElicitationRequiredError([]*mcp.ElicitParams{signIn})
		}},
		"input required": {url: true, handler: func() (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{InputRequests: mcp.InputRequestMap{"sign_in": signIn}}, nil
		}},
		"form input": {handler: func() (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{InputRequests: mcp.InputRequestMap{"project": form}}, nil
		}},
		"remote http URL": {handler: func() (*mcp.CallToolResult, error) {
			return nil, mcp.URLElicitationRequiredError([]*mcp.ElicitParams{remote})
		}},
	} {
		handler := test.handler
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			server := mcp.NewServer(&mcp.Implementation{Name: "tickets", Version: "1"}, nil)
			mcp.AddTool(server, &mcp.Tool{Name: "list"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
				calls.Add(1)
				result, err := handler()
				return result, nil, err
			})
			endpoint := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
			t.Cleanup(endpoint.Close)
			caller := NewMCPCaller(map[string]config.MCPServer{"tickets": {URL: endpoint.URL}}, nil)
			_, err := caller.Call(t.Context(), "tickets", "list", map[string]any{})
			var notExecuted *toolNotExecutedError
			require.ErrorAs(t, err, &notExecuted)
			require.Equal(t, int32(1), calls.Load())
			if test.url {
				require.NotNil(t, notExecuted.elicitation)
				require.Equal(t, signInURL, notExecuted.elicitation.URL)
			} else {
				require.Nil(t, notExecuted.elicitation)
			}
		})
	}
}
