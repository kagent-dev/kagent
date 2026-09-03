package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

func TestProcessDriverRunsPinnedProtocol(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "codex")
	capture := filepath.Join(directory, "requests.jsonl")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "codex-cli 0.148.0"
  exit 0
fi
read initialize
printf '%s\n' "$initialize" >> "$CAPTURE"
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"codex-app-server","version":"0.148.0"}}}'
read initialized
printf '%s\n' "$initialized" >> "$CAPTURE"
read thread
printf '%s\n' "$thread" >> "$CAPTURE"
printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"01900000-0000-7000-8000-000000000001"}}}'
read turn
printf '%s\n' "$turn" >> "$CAPTURE"
printf '%s\n' '{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"01900000-0000-7000-8000-000000000002","status":"inProgress"}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"item/agentMessage/delta","params":{"threadId":"01900000-0000-7000-8000-000000000001","turnId":"01900000-0000-7000-8000-000000000002","itemId":"message-1","delta":"hello"}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"01900000-0000-7000-8000-000000000001","turn":{"id":"01900000-0000-7000-8000-000000000002","status":"completed"}}}'
sleep 5
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(directory, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, ExpectedVersion: "0.148.0", StrictVersion: true, Workspace: workspace,
		Model: "gpt-5.2-codex", Provider: "kagent-openai", DeveloperInstruction: "help",
		Environment: append(os.Environ(), "CAPTURE="+capture), MaxFrameBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 100 * time.Millisecond,
	})
	if err := driver.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	outcome, err := driver.Run(context.Background(), runtime.Turn{Prompt: "say hello"}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Failure != nil || sink.text.String() != "hello" || len(sink.sessions) != 1 || sink.sessions[0].ContinuationID != "01900000-0000-7000-8000-000000000001" {
		t.Fatalf("outcome = %#v, sink = %#v, text = %q", outcome, sink, sink.text.String())
	}
	requests, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"method":"initialize"`, `"method":"initialized"`, `"method":"thread/start"`, `"approvalPolicy":"never"`, `"sandbox":"danger-full-access"`, `"method":"turn/start"`, `"text":"say hello"`} {
		if !bytes.Contains(requests, []byte(fragment)) {
			t.Errorf("requests omit %s:\n%s", fragment, requests)
		}
	}
	resumed := &recordingSink{}
	if _, err := driver.Run(context.Background(), runtime.Turn{Prompt: "resume", ContinuationID: "01900000-0000-7000-8000-000000000001"}, resumed); err != nil {
		t.Fatal(err)
	}
	requests, err = os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(requests, []byte(`"method":"thread/resume"`)) || !bytes.Contains(requests, []byte(`"threadId":"01900000-0000-7000-8000-000000000001"`)) {
		t.Fatalf("resume did not select the exact native thread:\n%s", requests)
	}
}

func TestProcessDriverRejectsWorkspaceConfiguration(t *testing.T) {
	workspace := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".codex", "config.toml"), []byte("model = \"injected\""), 0o600); err != nil {
		t.Fatal(err)
	}
	driver := NewProcessDriver(ProcessConfig{Workspace: workspace})
	_, err := driver.Run(context.Background(), runtime.Turn{Prompt: "hello"}, &recordingSink{})
	if err == nil || !strings.Contains(err.Error(), "workspace Codex configuration") {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestProcessDriverParksAndResolvesMCPApproval(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "codex")
	capture := filepath.Join(directory, "approval.json")
	script := `#!/bin/sh
read initialize
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read thread
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
read turn
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
printf '%s\n' '{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"type":"mcpToolCall","id":"call-1","server":"protected","tool":"write","arguments":{"value":7},"status":"inProgress"}}}'
printf '%s\n' '{"id":7,"method":"mcpServer/elicitation/request","params":{"_meta":{"codex_approval_kind":"mcp_tool_call","tool_params":{"value":7}},"message":"Allow protected.write?","mode":"form","requestedSchema":{"type":"object","properties":{}},"serverName":"protected","threadId":"thread-1","turnId":"turn-1"}}'
read approval
printf '%s\n' "$approval" > "$CAPTURE"
printf '%s\n' '{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"type":"mcpToolCall","id":"call-1","server":"protected","tool":"write","arguments":{"value":7},"result":"ok","status":"completed"}}}'
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}}'
sleep 5
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(directory, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: workspace, Model: "model", Provider: "provider",
		Environment: append(os.Environ(), "CAPTURE="+capture), MaxFrameBytes: 4096, MaxStderrBytes: 1024,
		InterruptGrace: 100 * time.Millisecond, ApprovalServers: map[string]struct{}{"protected": {}},
	})
	sink := &recordingSink{}
	outcome, err := driver.Run(t.Context(), runtime.Turn{Prompt: "write"}, sink)
	if err != nil {
		t.Fatal(err)
	}
	request, ok := outcome.InputRequired.(*runtime.ApprovalRequest)
	if !ok || request.ID != "7" || request.CallID != "call-1" || request.Name != "protected.write" {
		t.Fatalf("input required = %#v", outcome.InputRequired)
	}
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatalf("MCP call was resolved before approval: %v", err)
	}
	outcome, err = driver.Run(t.Context(), runtime.Turn{InputResponse: &runtime.ApprovalDecision{
		ID: "7", Approved: false, RejectionReason: "production is still serving traffic",
	}}, sink)
	if err != nil || outcome.InputRequired != nil || outcome.Failure != nil {
		t.Fatalf("resume outcome = %#v, error = %v", outcome, err)
	}
	response, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(response, []byte(`"id":7`)) || !bytes.Contains(response, []byte(`"action":"decline"`)) || bytes.Contains(response, []byte(`"rejection_reason"`)) || bytes.Contains(response, []byte(`"content"`)) {
		t.Fatalf("approval response = %s", response)
	}
}

func TestProcessDriverParksAndResolvesAskUserWithExperimentalAPI(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "codex")
	initializeCapture := filepath.Join(directory, "initialize.json")
	responseCapture := filepath.Join(directory, "response.json")
	script := `#!/bin/sh
read initialize
printf '%s\n' "$initialize" > "$INITIALIZE_CAPTURE"
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read thread
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
read turn
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
printf '%s\n' '{"id":8,"method":"item/tool/requestUserInput","params":{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","isBlocking":true,"questions":[{"id":"namespace","header":"Namespace","question":"Which namespace?","options":[{"label":"default","description":"Default namespace"}],"isOther":true,"isSecret":false},{"id":"cluster","header":"Cluster","question":"Which cluster?","options":null,"isOther":false,"isSecret":false}]}}'
read response
printf '%s\n' "$response" > "$RESPONSE_CAPTURE"
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}}'
sleep 5
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(directory, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: workspace, Model: "model", Provider: "provider",
		Environment:   append(os.Environ(), "INITIALIZE_CAPTURE="+initializeCapture, "RESPONSE_CAPTURE="+responseCapture),
		MaxFrameBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 100 * time.Millisecond,
	})
	outcome, err := driver.Run(t.Context(), runtime.Turn{Prompt: "deploy"}, &recordingSink{})
	if err != nil {
		t.Fatal(err)
	}
	request, ok := outcome.InputRequired.(*runtime.AskUserRequest)
	if !ok || request.ID != "8" || len(request.Questions) != 2 || request.Questions[0].ID != "namespace" || !request.Questions[0].IsOther {
		t.Fatalf("ask-user request = %#v", outcome.InputRequired)
	}
	initialize, err := os.ReadFile(initializeCapture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(initialize, []byte(`"capabilities":{"experimentalApi":true}`)) {
		t.Fatalf("experimental API was not enabled: %s", initialize)
	}

	outcome, err = driver.Run(t.Context(), runtime.Turn{InputResponse: &runtime.AskUserResponse{
		ID: "8", Answers: [][]string{{"default"}, {"production"}},
	}}, &recordingSink{})
	if err != nil || outcome.InputRequired != nil || outcome.Failure != nil {
		t.Fatalf("resume outcome = %#v, error = %v", outcome, err)
	}
	response, err := os.ReadFile(responseCapture)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"id":8`, `"answers"`, `"namespace":{"answers":["default"]}`, `"cluster":{"answers":["production"]}`} {
		if !bytes.Contains(response, []byte(fragment)) {
			t.Fatalf("ask-user response omits %s: %s", fragment, response)
		}
	}
}

func TestDecodeAskUserRequestRejectsDuplicateQuestionIDs(t *testing.T) {
	message := rpcMessage{ID: json.RawMessage(`8`), Params: json.RawMessage(`{
		"threadId":"thread-1","turnId":"turn-1","questions":[
			{"id":"target","header":"One","question":"First?"},
			{"id":"target","header":"Two","question":"Second?"}
		]
	}`)}
	if _, err := decodeAskUserRequest(message, newEventTranslator("thread-1", "turn-1")); err == nil || !strings.Contains(err.Error(), "duplicate question ID") {
		t.Fatalf("decodeAskUserRequest() error = %v", err)
	}
}

func TestDecodeAskUserRequestRejectsSecretQuestions(t *testing.T) {
	message := rpcMessage{ID: json.RawMessage(`8`), Params: json.RawMessage(`{
		"threadId":"thread-1","turnId":"turn-1","questions":[
			{"id":"token","header":"Token","question":"Enter a token","isSecret":true}
		]
	}`)}
	if _, err := decodeAskUserRequest(message, newEventTranslator("thread-1", "turn-1")); err == nil || !strings.Contains(err.Error(), "secret ask-user") {
		t.Fatalf("decodeAskUserRequest() error = %v", err)
	}
}

func TestProcessDriverCancelsParkedMCPApproval(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "codex")
	approvalCapture := filepath.Join(directory, "approval.json")
	interruptCapture := filepath.Join(directory, "interrupt.json")
	script := `#!/bin/sh
read initialize
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read thread
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
read turn
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
printf '%s\n' '{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"type":"mcpToolCall","id":"call-1","server":"protected","tool":"write","arguments":{},"status":"inProgress"}}}'
printf '%s\n' '{"id":7,"method":"mcpServer/elicitation/request","params":{"_meta":{"codex_approval_kind":"mcp_tool_call","tool_params":{}},"message":"Allow protected.write?","mode":"form","requestedSchema":{"type":"object","properties":{}},"serverName":"protected","threadId":"thread-1","turnId":"turn-1"}}'
read approval
printf '%s\n' "$approval" > "$APPROVAL_CAPTURE"
read interrupt
printf '%s\n' "$interrupt" > "$INTERRUPT_CAPTURE"
printf '%s\n' '{"id":4,"result":{}}'
sleep 5
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(directory, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: workspace, Model: "model", Provider: "provider",
		Environment:   append(os.Environ(), "APPROVAL_CAPTURE="+approvalCapture, "INTERRUPT_CAPTURE="+interruptCapture),
		MaxFrameBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 100 * time.Millisecond,
		ApprovalServers: map[string]struct{}{"protected": {}},
	})
	outcome, err := driver.Run(t.Context(), runtime.Turn{Prompt: "write"}, &recordingSink{})
	if err != nil || outcome.InputRequired == nil {
		t.Fatalf("Run() = %#v, %v", outcome, err)
	}
	if err := driver.CancelParked(t.Context()); err != nil {
		t.Fatal(err)
	}

	approval, err := os.ReadFile(approvalCapture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(approval, []byte(`"id":7`)) || !bytes.Contains(approval, []byte(`"action":"cancel"`)) || !bytes.Contains(approval, []byte(`"content":null`)) {
		t.Fatalf("cancel response = %s", approval)
	}
	interrupt, err := os.ReadFile(interruptCapture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(interrupt, []byte(`"method":"turn/interrupt"`)) || !bytes.Contains(interrupt, []byte(`"threadId":"thread-1"`)) || !bytes.Contains(interrupt, []byte(`"turnId":"turn-1"`)) {
		t.Fatalf("interrupt request = %s", interrupt)
	}
	if driver.parked != nil {
		t.Fatal("driver retained canceled session")
	}
}

func TestApprovalResponseAcceptsWithEmptyRequestedContent(t *testing.T) {
	response := approvalResponse(runtime.ApprovalDecision{ID: "7", Approved: true})
	if response["action"] != "accept" {
		t.Fatalf("approval response = %#v", response)
	}
	if content, ok := response["content"].(map[string]any); !ok || len(content) != 0 {
		t.Fatalf("approval content = %#v", response["content"])
	}
}
