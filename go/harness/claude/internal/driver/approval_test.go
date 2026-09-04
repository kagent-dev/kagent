package driver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

func TestApprovalBrokerAllowsAndDeniesProtectedCalls(t *testing.T) {
	broker, err := NewApprovalBroker([]string{"production_db"}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = broker.Close() })

	settings, err := broker.SettingsJSON([]string{"readonly"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(settings)
	for _, want := range []string{`mcp__readonly__*`, `mcp__production_db__.*`, `"Authorization":"Bearer `} {
		if !strings.Contains(text, want) {
			t.Fatalf("settings %s do not contain %q", settings, want)
		}
	}
	if strings.Contains(text, `mcp__production_db__*"`) {
		t.Fatalf("protected server was added to permissions.allow: %s", settings)
	}

	for _, approved := range []bool{true, false} {
		body := []byte(`{"session_id":"session-1","hook_event_name":"PreToolUse","tool_name":"mcp__production_db__write","tool_input":{"value":7},"tool_use_id":"call-1-` + boolText(approved) + `"}`)
		response := make(chan struct {
			status int
			body   []byte
			err    error
		}, 1)
		go func() {
			request, requestErr := http.NewRequest(http.MethodPost, "http://"+broker.listener.Addr().String()+approvalHookPath, bytes.NewReader(body))
			if requestErr != nil {
				response <- struct {
					status int
					body   []byte
					err    error
				}{err: requestErr}
				return
			}
			request.Header.Set("Authorization", "Bearer "+broker.token)
			got, requestErr := http.DefaultClient.Do(request)
			if requestErr != nil {
				response <- struct {
					status int
					body   []byte
					err    error
				}{err: requestErr}
				return
			}
			defer got.Body.Close()
			responseBody, requestErr := io.ReadAll(got.Body)
			response <- struct {
				status int
				body   []byte
				err    error
			}{status: got.StatusCode, body: responseBody, err: requestErr}
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
		if got.err != nil || got.status != http.StatusOK {
			t.Fatalf("hook response = %d %s, %v", got.status, got.body, got.err)
		}
		var output hookOutput
		if err := json.Unmarshal(got.body, &output); err != nil {
			t.Fatal(err)
		}
		want := "deny"
		if approved {
			want = "allow"
		}
		if output.Specific.PermissionDecision != want {
			t.Fatalf("permission decision = %q, want %q", output.Specific.PermissionDecision, want)
		}
	}
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
	broker := &ApprovalBroker{requests: make(chan *PendingHookRequest, 2)}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: dir,
		Environment:   []string{"DECISION_ONE=" + decisionOne, "DECISION_TWO=" + decisionTwo},
		MaxEventBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 100 * time.Millisecond,
		ApprovalBroker: broker, SettingsPath: filepath.Join(dir, "settings.json"),
	})
	requests := []*PendingHookRequest{
		newTestPending("approval-1", "call-1"), newTestPending("approval-2", "call-2"),
	}
	for _, pending := range requests {
		broker.requests <- pending
	}
	bridgeDecisionToFile(t, requests[0], decisionOne)
	bridgeDecisionToFile(t, requests[1], decisionTwo)

	outcome, err := driver.Run(t.Context(), runtime.Turn{Prompt: "write twice"}, &recordingSink{})
	if err != nil || outcome.InputRequired.(*runtime.ApprovalRequest).ID != "approval-1" {
		t.Fatalf("first Run() = %#v, %v", outcome, err)
	}
	outcome, err = driver.Run(t.Context(), runtime.Turn{InputResponse: &runtime.ApprovalDecision{ID: "approval-1", Approved: true}}, &recordingSink{})
	if err != nil || outcome.InputRequired.(*runtime.ApprovalRequest).ID != "approval-2" {
		t.Fatalf("second Run() = %#v, %v", outcome, err)
	}
	outcome, err = driver.Run(t.Context(), runtime.Turn{InputResponse: &runtime.ApprovalDecision{ID: "approval-2", Approved: false}}, &recordingSink{})
	if err != nil || outcome.InputRequired != nil || outcome.Failure != nil || driver.parked != nil {
		t.Fatalf("final Run() = %#v, %v; parked = %#v", outcome, err, driver.parked)
	}
}

func TestProcessDriverCancelsParkedApproval(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"11111111-1111-4111-8111-111111111111\"}'\nwhile :; do :; done\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	broker := &ApprovalBroker{requests: make(chan *PendingHookRequest, 1)}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: dir, MaxEventBytes: 4096, MaxStderrBytes: 1024,
		InterruptGrace: 50 * time.Millisecond, ApprovalBroker: broker, SettingsPath: filepath.Join(dir, "settings.json"),
	})
	broker.requests <- newTestPending("approval-1", "call-1")
	outcome, err := driver.Run(t.Context(), runtime.Turn{Prompt: "write"}, &recordingSink{})
	if err != nil || outcome.InputRequired == nil {
		t.Fatalf("Run() = %#v, %v", outcome, err)
	}
	if err := driver.CancelParked(t.Context()); err != nil {
		t.Fatal(err)
	}
	if driver.parked != nil {
		t.Fatalf("parked session was retained: %#v", driver.parked)
	}
}

func newTestPending(id, callID string) *PendingHookRequest {
	return &PendingHookRequest{
		request:   runtime.ApprovalRequest{ID: id, CallID: callID, Name: "mcp__protected__write"},
		sessionID: "11111111-1111-4111-8111-111111111111",
		decision:  make(chan runtime.ApprovalDecision, 1), done: make(chan struct{}),
	}
}

func bridgeDecisionToFile(t *testing.T, pending *PendingHookRequest, path string) {
	t.Helper()
	go func() {
		<-pending.decision
		if err := os.WriteFile(path, []byte("decision\n"), 0o600); err != nil {
			t.Errorf("write decision file: %v", err)
		}
	}()
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
