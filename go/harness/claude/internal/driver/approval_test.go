package driver

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestApprovalBrokerAllowsAndDeniesProtectedCalls(t *testing.T) {
	broker, err := NewApprovalBroker([]string{"production_db"}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = broker.Close() })

	settings, err := broker.SettingsJSON()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(settings), `{"permissions":{"ask":["mcp__production_db__*"]}}`; got != want {
		t.Fatalf("settings = %s, want %s", got, want)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "approval-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint: broker.URL(), HTTPClient: &http.Client{Transport: &headerTransport{
			headers: broker.Headers(), base: http.DefaultTransport,
		}}, DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	for _, approved := range []bool{true, false} {
		toolUseID := "call-denied"
		if approved {
			toolUseID = "call-approved"
		}
		response := make(chan struct {
			result *mcp.CallToolResult
			err    error
		}, 1)
		go func() {
			result, callErr := session.CallTool(t.Context(), &mcp.CallToolParams{
				Name: ApprovalToolName,
				Arguments: map[string]any{
					"tool_name": "mcp__production_db__write",
					"input":     map[string]any{"value": 7}, "tool_use_id": toolUseID,
				},
			})
			response <- struct {
				result *mcp.CallToolResult
				err    error
			}{result: result, err: callErr}
		}()

		pending := <-broker.Requests()
		approval := pending.approvalRequest()
		if approval.CallID == "" || approval.Name != "mcp__production_db__write" || approval.Args["value"] != float64(7) {
			t.Fatalf("approval request = %#v", approval)
		}
		if err := pending.resolve(runtime.ApprovalDecision{ID: approval.ID, Approved: approved, RejectionReason: "operator denied"}); err != nil {
			t.Fatal(err)
		}
		got := <-response
		if got.err != nil || len(got.result.Content) != 1 {
			t.Fatalf("permission response = %#v, %v", got.result, got.err)
		}
		content, ok := got.result.Content[0].(*mcp.TextContent)
		if !ok {
			t.Fatalf("permission response content = %#v", got.result.Content[0])
		}
		var output permissionPromptOutput
		if err := json.Unmarshal([]byte(content.Text), &output); err != nil {
			t.Fatal(err)
		}
		want := "deny"
		if approved {
			want = "allow"
		}
		if output.Behavior != want {
			t.Fatalf("permission decision = %q, want %q", output.Behavior, want)
		}
		if approved && output.UpdatedInput["value"] != float64(7) {
			t.Fatalf("updated input = %#v", output.UpdatedInput)
		}
	}
}

type headerTransport struct {
	headers map[string]string
	base    http.RoundTripper
}

func (t *headerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	for name, value := range t.headers {
		request.Header.Set(name, value)
	}
	return t.base.RoundTrip(request)
}

func TestProcessDriverParksAndResumesSequentialApprovals(t *testing.T) {
	dir := t.TempDir()
	decisionOne := filepath.Join(dir, "decision-1")
	decisionTwo := filepath.Join(dir, "decision-2")
	executable := filepath.Join(dir, "claude")
	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"11111111-1111-4111-8111-111111111111"}'
printf '%s\n' '{"type":"assistant","message":{"id":"msg_1","content":[{"type":"tool_use","id":"call-1","name":"mcp__protected__write","input":{}}]}}'
while [ ! -f "$DECISION_ONE" ]; do sleep 0.01; done
printf '%s\n' '{"type":"assistant","message":{"id":"msg_2","content":[{"type":"tool_use","id":"call-2","name":"mcp__protected__write","input":{}}]}}'
while [ ! -f "$DECISION_TWO" ]; do sleep 0.01; done
printf '%s\n' '{"type":"assistant","message":{"id":"msg_warning","content":[{"type":"text","text":"API Error: Connection lost mid-response. The response above may be incomplete."}]}}' '{"type":"assistant","message":{"id":"msg_continued","content":[{"type":"text","text":"continued"}]}}' '{"type":"result","subtype":"success","session_id":"11111111-1111-4111-8111-111111111111"}'
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	broker := &ApprovalBroker{requests: make(chan *PendingApprovalRequest, 2)}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: dir,
		Environment:   []string{"DECISION_ONE=" + decisionOne, "DECISION_TWO=" + decisionTwo},
		MaxEventBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 100 * time.Millisecond,
		ApprovalBroker: broker, SettingsPath: filepath.Join(dir, "settings.json"),
	})
	requests := []*PendingApprovalRequest{
		newTestPending("approval-1", "call-1"), newTestPending("approval-2", "call-2"),
	}
	for _, pending := range requests {
		broker.requests <- pending
	}
	bridgeDecisionToFile(t, requests[0], decisionOne)
	bridgeDecisionToFile(t, requests[1], decisionTwo)

	outcome, err := driver.Run(t.Context(), runtime.Turn{Prompt: "write twice"}, &recordingSink{})
	if err != nil || outcome.Pending == nil || outcome.Pending.Request().(*runtime.ApprovalRequest).ID != "approval-1" {
		t.Fatalf("first Run() = %#v, %v", outcome, err)
	}
	outcome, err = outcome.Pending.Resume(t.Context(), &runtime.ApprovalDecision{ID: "approval-1", Approved: true}, &recordingSink{})
	if err != nil || outcome.Pending == nil || outcome.Pending.Request().(*runtime.ApprovalRequest).ID != "approval-2" {
		t.Fatalf("second Run() = %#v, %v", outcome, err)
	}
	resumed := &recordingSink{}
	outcome, err = outcome.Pending.Resume(t.Context(), &runtime.ApprovalDecision{ID: "approval-2", Approved: false}, resumed)
	if err != nil || outcome.Pending != nil || outcome.Failure != nil {
		t.Fatalf("final Resume() = %#v, %v", outcome, err)
	}
	if resumed.text.String() != "continued" {
		t.Fatalf("resumed text = %q, want only continued", resumed.text.String())
	}
}

// Claude Code 2.1.285 writes an is_error tool_result for a denied call, after the decision.
func TestResumeMarksTheDeniedResultDeclined(t *testing.T) {
	for _, tc := range []struct {
		name     string
		approved bool
		want     runtime.ToolStatus
	}{
		{name: "denied", want: runtime.ToolDeclined},
		{name: "approved", approved: true, want: runtime.ToolRan},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			decision := filepath.Join(dir, "decision")
			executable := filepath.Join(dir, "claude")
			script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"11111111-1111-4111-8111-111111111111"}'
printf '%s\n' '{"type":"assistant","message":{"id":"msg_1","content":[{"type":"tool_use","id":"call-1","name":"mcp__tools__write","input":{}}]}}'
while [ ! -f "$DECISION" ]; do sleep 0.01; done
printf '%s\n' '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"call-1","content":"operator denied","is_error":true}]}}'
printf '%s\n' '{"type":"result","subtype":"success","session_id":"11111111-1111-4111-8111-111111111111"}'
`
			if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			broker := &ApprovalBroker{requests: make(chan *PendingApprovalRequest, 1)}
			driver := NewProcessDriver(ProcessConfig{
				Executable: executable, Workspace: dir, Environment: []string{"DECISION=" + decision},
				MaxEventBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 100 * time.Millisecond,
				ApprovalBroker: broker, SettingsPath: filepath.Join(dir, "settings.json"),
			})
			pending := newTestPending("approval-1", "call-1")
			broker.requests <- pending
			bridgeDecisionToFile(t, pending, decision)

			outcome, err := driver.Run(t.Context(), runtime.Turn{Prompt: "write"}, &recordingSink{})
			if err != nil || outcome.Pending == nil {
				t.Fatalf("Run() = %#v, %v", outcome, err)
			}
			sink := &recordingSink{}
			if _, err := outcome.Pending.Resume(t.Context(), &runtime.ApprovalDecision{ID: "approval-1", Approved: tc.approved}, sink); err != nil {
				t.Fatal(err)
			}
			if len(sink.results) != 1 || sink.results[0].Status != tc.want || !sink.results[0].IsError {
				t.Fatalf("results = %+v, want one %q result that keeps is_error", sink.results, tc.want)
			}
		})
	}
}

// Claude prints the rest of a message before it blocks on the gated call. Those lines
// must keep the time they arrived, not the time the parser reads them after the resume.
func TestModelCallKeepsItsTimesAcrossAnApprovalPause(t *testing.T) {
	dir := t.TempDir()
	decision := filepath.Join(dir, "decision")
	executable := filepath.Join(dir, "claude")
	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"11111111-1111-4111-8111-111111111111"}'
printf '%s\n' '{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_1","model":"m","usage":{"input_tokens":7}}}}'
printf '%s\n' '{"type":"assistant","message":{"id":"msg_1","model":"m","content":[{"type":"tool_use","id":"call-1","name":"mcp__protected__write","input":{}}]}}'
printf '%s\n' '{"type":"assistant","message":{"id":"msg_1","model":"m","content":[{"type":"tool_use","id":"call-2","name":"Read","input":{}}]}}'
printf '%s\n' '{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}}'
printf '%s\n' '{"type":"stream_event","event":{"type":"message_stop"}}'
while [ ! -f "$DECISION" ]; do sleep 0.01; done
printf '%s\n' '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"call-1","content":"ok"},{"type":"tool_result","tool_use_id":"call-2","content":"ok"}]}}'
printf '%s\n' '{"type":"result","subtype":"success","session_id":"11111111-1111-4111-8111-111111111111"}'
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	broker := &ApprovalBroker{requests: make(chan *PendingApprovalRequest, 1)}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: dir, Environment: []string{"DECISION=" + decision},
		MaxEventBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 100 * time.Millisecond,
		ApprovalBroker: broker, SettingsPath: filepath.Join(dir, "settings.json"),
	})
	pending := newTestPending("approval-1", "call-1")
	broker.requests <- pending
	bridgeDecisionToFile(t, pending, decision)

	outcome, err := driver.Run(t.Context(), runtime.Turn{Prompt: "write"}, &recordingSink{})
	if err != nil || outcome.Pending == nil {
		t.Fatalf("Run() = %#v, %v", outcome, err)
	}
	time.Sleep(300 * time.Millisecond)
	resumed := time.Now()
	sink := &recordingSink{}
	if _, err := outcome.Pending.Resume(t.Context(), &runtime.ApprovalDecision{ID: "approval-1", Approved: true}, sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.models) != 1 || !sink.models[0].End.Before(resumed) {
		t.Fatalf("model calls = %+v, want one that ended before the resume at %v", sink.models, resumed)
	}
}

// Claude Code stops reading a stream that the approval wait holds open, then reports it as a lost
// connection on resume. The call asked for a tool before the pause, so it is not a failure.
func TestModelCallCutByAnApprovalPauseIsNotAFailure(t *testing.T) {
	dir := t.TempDir()
	decision := filepath.Join(dir, "decision")
	executable := filepath.Join(dir, "claude")
	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"11111111-1111-4111-8111-111111111111"}'
printf '%s\n' '{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_1","model":"m","usage":{"input_tokens":7}}}}'
printf '%s\n' '{"type":"assistant","message":{"id":"msg_1","model":"m","content":[{"type":"tool_use","id":"call-1","name":"mcp__protected__write","input":{}}]}}'
while [ ! -f "$DECISION" ]; do sleep 0.01; done
printf '%s\n' '{"type":"assistant","message":{"id":"err_1","model":"<synthetic>","content":[{"type":"text","text":"API Error: Connection lost mid-response. The response above may be incomplete."}]},"error":"server_error","is_api_error_message":true}'
printf '%s\n' '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"call-1","content":"ok"}]}}'
printf '%s\n' '{"type":"result","subtype":"success","session_id":"11111111-1111-4111-8111-111111111111"}'
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	broker := &ApprovalBroker{requests: make(chan *PendingApprovalRequest, 1)}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: dir, Environment: []string{"DECISION=" + decision},
		MaxEventBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 100 * time.Millisecond,
		ApprovalBroker: broker, SettingsPath: filepath.Join(dir, "settings.json"),
	})
	pending := newTestPending("approval-1", "call-1")
	broker.requests <- pending
	bridgeDecisionToFile(t, pending, decision)

	outcome, err := driver.Run(t.Context(), runtime.Turn{Prompt: "write"}, &recordingSink{})
	if err != nil || outcome.Pending == nil {
		t.Fatalf("Run() = %#v, %v", outcome, err)
	}
	resumed := time.Now()
	sink := &recordingSink{}
	if _, err := outcome.Pending.Resume(t.Context(), &runtime.ApprovalDecision{ID: "approval-1", Approved: true}, sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.models) != 1 {
		t.Fatalf("model calls = %+v, want one", sink.models)
	}
	call := sink.models[0]
	if call.StopReason != tracing.FinishReasonToolCall || call.ErrorType != "" || !call.UsagePartial || !call.End.Before(resumed) {
		t.Fatalf("model call = %+v, want a partial tool_call that ended before the resume at %v", call, resumed)
	}
}

func TestProcessDriverCancelsParkedApproval(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}'\nprintf '%s\\n' '{\"type\":\"assistant\",\"message\":{\"id\":\"msg_1\",\"content\":[{\"type\":\"tool_use\",\"id\":\"call-1\",\"name\":\"mcp__protected__write\",\"input\":{}}]}}'\nwhile :; do :; done\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	broker := &ApprovalBroker{requests: make(chan *PendingApprovalRequest, 1)}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: dir, MaxEventBytes: 4096, MaxStderrBytes: 1024,
		InterruptGrace: 50 * time.Millisecond, ApprovalBroker: broker, SettingsPath: filepath.Join(dir, "settings.json"),
	})
	broker.requests <- newTestPending("approval-1", "call-1")
	outcome, err := driver.Run(t.Context(), runtime.Turn{Prompt: "write"}, &recordingSink{})
	if err != nil || outcome.Pending == nil {
		t.Fatalf("Run() = %#v, %v", outcome, err)
	}
	if err := outcome.Pending.Cancel(t.Context(), &recordingSink{}); err != nil {
		t.Fatal(err)
	}
}

func TestProcessDriverCancelsParkedApprovalKeepingBufferedModelCalls(t *testing.T) {
	dir, executable := writeFakeClaude(t, `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"11111111-1111-4111-8111-111111111111"}'
printf '%s\n' '{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_1","model":"claude-opus-5-5","usage":{"input_tokens":5}}}}'
printf '%s\n' '{"type":"assistant","message":{"id":"msg_1","model":"claude-opus-5-5","content":[{"type":"tool_use","id":"call-1","name":"mcp__protected__write","input":{}}]}}'
while [ ! -f "$RELEASE" ]; do sleep 0.01; done
printf '%s\n' '{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}}'
touch "$PRINTED"
exec sleep 30
`)
	release, printed := filepath.Join(dir, "release"), filepath.Join(dir, "printed")
	broker := &ApprovalBroker{requests: make(chan *PendingApprovalRequest, 1)}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: dir, Environment: []string{"RELEASE=" + release, "PRINTED=" + printed},
		MaxEventBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 50 * time.Millisecond,
		ApprovalBroker: broker, SettingsPath: filepath.Join(dir, "settings.json"),
	})
	broker.requests <- newTestPending("approval-1", "call-1")
	outcome, err := driver.Run(t.Context(), runtime.Turn{Prompt: "write"}, &recordingSink{})
	if err != nil || outcome.Pending == nil {
		t.Fatalf("Run() = %#v, %v", outcome, err)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for _, err := os.Stat(printed); err != nil; _, err = os.Stat(printed) {
		if time.Now().After(deadline) {
			t.Fatalf("fake Claude never printed its model call: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	sink := &recordingSink{}
	if err := outcome.Pending.Cancel(t.Context(), sink); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if len(sink.models) != 1 || sink.models[0].OutputTokens != 3 || sink.models[0].UsagePartial {
		t.Fatalf("model calls = %#v, want the buffered complete call", sink.models)
	}
}

func newTestPending(id, callID string) *PendingApprovalRequest {
	return &PendingApprovalRequest{
		request:  runtime.ApprovalRequest{ID: id, CallID: callID, Name: "mcp__protected__write"},
		input:    map[string]any{},
		decision: make(chan runtime.ApprovalDecision, 1), done: make(chan struct{}),
	}
}

func bridgeDecisionToFile(t *testing.T, pending *PendingApprovalRequest, path string) {
	t.Helper()
	go func() {
		<-pending.decision
		if err := os.WriteFile(path, []byte("decision\n"), 0o600); err != nil {
			t.Errorf("write decision file: %v", err)
		}
	}()
}
