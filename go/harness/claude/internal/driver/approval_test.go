package driver

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
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
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}'\nwhile [ ! -f \"$DECISION_ONE\" ]; do sleep 0.01; done\nwhile [ ! -f \"$DECISION_TWO\" ]; do sleep 0.01; done\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}'\n"
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
	outcome, err = outcome.Pending.Resume(t.Context(), &runtime.ApprovalDecision{ID: "approval-2", Approved: false}, &recordingSink{})
	if err != nil || outcome.Pending != nil || outcome.Failure != nil {
		t.Fatalf("final Resume() = %#v, %v", outcome, err)
	}
}

func TestProcessDriverCancelsParkedApproval(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}'\nwhile :; do :; done\n"
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
	if err := outcome.Pending.Cancel(t.Context()); err != nil {
		t.Fatal(err)
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
