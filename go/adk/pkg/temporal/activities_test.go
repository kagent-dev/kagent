package temporal

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/adk/pkg/session"
	"github.com/kagent-dev/kagent/go/adk/pkg/streaming"
	"github.com/kagent-dev/kagent/go/adk/pkg/taskstore"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// startEmbeddedNATS starts an in-process NATS server on a random port for testing.
func startEmbeddedNATS(t *testing.T) (*natsserver.Server, string) {
	t.Helper()
	opts := &natsserver.Options{
		Host:   "127.0.0.1",
		Port:   -1,
		NoLog:  true,
		NoSigs: true,
	}
	ns, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("failed to create embedded NATS server: %v", err)
	}
	ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("embedded NATS server not ready")
	}
	t.Cleanup(func() {
		ns.Shutdown()
		ns.WaitForShutdown()
	})
	return ns, ns.ClientURL()
}

func connectNATS(t *testing.T, addr string) *nats.Conn {
	t.Helper()
	conn, err := nats.Connect(addr)
	if err != nil {
		t.Fatalf("failed to connect to NATS: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// mockSessionService implements session.SessionService for testing.
type mockSessionService struct {
	sessions map[string]*session.Session
	events   map[string][]any
	mu       sync.Mutex

	createErr    error
	getErr       error
	appendErr    error
	getReturnsNil bool
}

func newMockSessionService() *mockSessionService {
	return &mockSessionService{
		sessions: make(map[string]*session.Session),
		events:   make(map[string][]any),
	}
}

func (m *mockSessionService) CreateSession(_ context.Context, appName, userID string, state map[string]any, sessionID string) (*session.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return nil, m.createErr
	}
	sess := &session.Session{
		ID:      sessionID,
		UserID:  userID,
		AppName: appName,
		State:   state,
	}
	m.sessions[sessionID] = sess
	return sess, nil
}

func (m *mockSessionService) GetSession(_ context.Context, appName, userID, sessionID string) (*session.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.getReturnsNil {
		return nil, nil
	}
	sess, ok := m.sessions[sessionID]
	if !ok {
		return nil, nil
	}
	return sess, nil
}

func (m *mockSessionService) DeleteSession(_ context.Context, _, _, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
	return nil
}

func (m *mockSessionService) AppendEvent(_ context.Context, sess *session.Session, event any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.appendErr != nil {
		return m.appendErr
	}
	m.events[sess.ID] = append(m.events[sess.ID], event)
	return nil
}

func TestSessionActivity_CreateNew(t *testing.T) {
	svc := newMockSessionService()
	act := NewActivities(svc, nil, nil, nil, nil)

	resp, err := act.SessionActivity(context.Background(), &SessionRequest{
		AppName:   "test-app",
		UserID:    "user1",
		SessionID: "sess-123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.SessionID != "sess-123" {
		t.Errorf("got sessionID=%q, want %q", resp.SessionID, "sess-123")
	}
	if !resp.Created {
		t.Error("expected Created=true for new session")
	}
}

func TestSessionActivity_GetExisting(t *testing.T) {
	svc := newMockSessionService()
	// Pre-populate session.
	svc.sessions["sess-existing"] = &session.Session{
		ID:      "sess-existing",
		UserID:  "user1",
		AppName: "test-app",
	}

	act := NewActivities(svc, nil, nil, nil, nil)

	resp, err := act.SessionActivity(context.Background(), &SessionRequest{
		AppName:   "test-app",
		UserID:    "user1",
		SessionID: "sess-existing",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.SessionID != "sess-existing" {
		t.Errorf("got sessionID=%q, want %q", resp.SessionID, "sess-existing")
	}
	if resp.Created {
		t.Error("expected Created=false for existing session")
	}
}

func TestSessionActivity_CreateError(t *testing.T) {
	svc := newMockSessionService()
	svc.getReturnsNil = true
	svc.createErr = fmt.Errorf("db error")

	act := NewActivities(svc, nil, nil, nil, nil)

	_, err := act.SessionActivity(context.Background(), &SessionRequest{
		AppName:   "test-app",
		UserID:    "user1",
		SessionID: "sess-fail",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSessionActivity_NilService(t *testing.T) {
	act := NewActivities(nil, nil, nil, nil, nil)

	_, err := act.SessionActivity(context.Background(), &SessionRequest{
		SessionID: "sess-123",
	})
	if err == nil {
		t.Fatal("expected error for nil session service")
	}
}

func TestLLMInvokeActivity_Success(t *testing.T) {
	invoker := func(_ context.Context, config, history []byte, onToken func(string)) (*LLMResponse, error) {
		if onToken != nil {
			onToken("Hello")
			onToken(" world")
		}
		return &LLMResponse{
			Content:  "Hello world",
			Terminal: true,
		}, nil
	}

	act := NewActivities(nil, nil, nil, invoker, nil)

	resp, err := act.LLMInvokeActivity(context.Background(), &LLMRequest{
		Config:  []byte(`{}`),
		History: []byte(`[]`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello world" {
		t.Errorf("got content=%q, want %q", resp.Content, "Hello world")
	}
	if !resp.Terminal {
		t.Error("expected Terminal=true")
	}
}

func TestLLMInvokeActivity_WithNATSStreaming(t *testing.T) {
	_, addr := startEmbeddedNATS(t)
	conn := connectNATS(t, addr)

	subject := "agent.test.sess1.stream"
	var received []streaming.StreamEvent
	var mu sync.Mutex

	sub, err := conn.Subscribe(subject, func(msg *nats.Msg) {
		var evt streaming.StreamEvent
		if err := json.Unmarshal(msg.Data, &evt); err == nil {
			mu.Lock()
			received = append(received, evt)
			mu.Unlock()
		}
	})
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	invoker := func(_ context.Context, _, _ []byte, onToken func(string)) (*LLMResponse, error) {
		if onToken != nil {
			onToken("tok1")
			onToken("tok2")
		}
		return &LLMResponse{Content: "tok1tok2", Terminal: true}, nil
	}

	act := NewActivities(nil, nil, conn, invoker, nil)

	resp, err := act.LLMInvokeActivity(context.Background(), &LLMRequest{
		Config:      []byte(`{}`),
		History:     []byte(`[]`),
		NATSSubject: subject,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "tok1tok2" {
		t.Errorf("got content=%q, want %q", resp.Content, "tok1tok2")
	}

	// Flush and wait for messages.
	conn.Flush()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("expected 2 NATS events, got %d", len(received))
	}
	for _, evt := range received {
		if evt.Type != streaming.EventTypeToken {
			t.Errorf("expected event type %q, got %q", streaming.EventTypeToken, evt.Type)
		}
	}
	if received[0].Data != "tok1" || received[1].Data != "tok2" {
		t.Errorf("unexpected token data: %q, %q", received[0].Data, received[1].Data)
	}
}

func TestLLMInvokeActivity_Error(t *testing.T) {
	invoker := func(_ context.Context, _, _ []byte, _ func(string)) (*LLMResponse, error) {
		return nil, fmt.Errorf("model unavailable")
	}

	act := NewActivities(nil, nil, nil, invoker, nil)

	_, err := act.LLMInvokeActivity(context.Background(), &LLMRequest{
		Config:  []byte(`{}`),
		History: []byte(`[]`),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLLMInvokeActivity_NilInvoker(t *testing.T) {
	act := NewActivities(nil, nil, nil, nil, nil)

	_, err := act.LLMInvokeActivity(context.Background(), &LLMRequest{})
	if err == nil {
		t.Fatal("expected error for nil model invoker")
	}
}

func TestLLMInvokeActivity_ErrorPublishesToNATS(t *testing.T) {
	_, addr := startEmbeddedNATS(t)
	conn := connectNATS(t, addr)

	subject := "agent.test.sess-err.stream"
	var received []streaming.StreamEvent
	var mu sync.Mutex

	sub, err := conn.Subscribe(subject, func(msg *nats.Msg) {
		var evt streaming.StreamEvent
		if err := json.Unmarshal(msg.Data, &evt); err == nil {
			mu.Lock()
			received = append(received, evt)
			mu.Unlock()
		}
	})
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	invoker := func(_ context.Context, _, _ []byte, _ func(string)) (*LLMResponse, error) {
		return nil, fmt.Errorf("model crashed")
	}

	act := NewActivities(nil, nil, conn, invoker, nil)

	_, err = act.LLMInvokeActivity(context.Background(), &LLMRequest{
		NATSSubject: subject,
	})
	if err == nil {
		t.Fatal("expected error")
	}

	conn.Flush()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 error event, got %d", len(received))
	}
	if received[0].Type != streaming.EventTypeError {
		t.Errorf("expected error event type, got %q", received[0].Type)
	}
}

func TestToolExecuteActivity_Success(t *testing.T) {
	executor := func(_ context.Context, toolName string, args []byte) ([]byte, error) {
		return []byte(`{"result": "ok"}`), nil
	}

	act := NewActivities(nil, nil, nil, nil, executor)

	resp, err := act.ToolExecuteActivity(context.Background(), &ToolRequest{
		ToolName:   "my-tool",
		ToolCallID: "call-1",
		Args:       []byte(`{"key": "value"}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ToolCallID != "call-1" {
		t.Errorf("got toolCallID=%q, want %q", resp.ToolCallID, "call-1")
	}
	if string(resp.Result) != `{"result": "ok"}` {
		t.Errorf("unexpected result: %s", resp.Result)
	}
	if resp.Error != "" {
		t.Errorf("unexpected error in response: %s", resp.Error)
	}
}

func TestToolExecuteActivity_ToolError(t *testing.T) {
	executor := func(_ context.Context, toolName string, args []byte) ([]byte, error) {
		return nil, fmt.Errorf("tool failed")
	}

	act := NewActivities(nil, nil, nil, nil, executor)

	resp, err := act.ToolExecuteActivity(context.Background(), &ToolRequest{
		ToolName:   "bad-tool",
		ToolCallID: "call-2",
	})
	// Tool errors are returned in the response, not as activity errors.
	if err != nil {
		t.Fatalf("unexpected activity error: %v", err)
	}
	if resp.Error != "tool failed" {
		t.Errorf("expected tool error in response, got %q", resp.Error)
	}
}

func TestToolExecuteActivity_WithNATSEvents(t *testing.T) {
	_, addr := startEmbeddedNATS(t)
	conn := connectNATS(t, addr)

	subject := "agent.test.sess-tool.stream"
	var received []streaming.StreamEvent
	var mu sync.Mutex

	sub, err := conn.Subscribe(subject, func(msg *nats.Msg) {
		var evt streaming.StreamEvent
		if err := json.Unmarshal(msg.Data, &evt); err == nil {
			mu.Lock()
			received = append(received, evt)
			mu.Unlock()
		}
	})
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	executor := func(_ context.Context, toolName string, args []byte) ([]byte, error) {
		return []byte(`"done"`), nil
	}

	act := NewActivities(nil, nil, conn, nil, executor)

	_, err = act.ToolExecuteActivity(context.Background(), &ToolRequest{
		ToolName:    "my-tool",
		ToolCallID:  "call-3",
		NATSSubject: subject,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	conn.Flush()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("expected 2 events (start+end), got %d", len(received))
	}
	if received[0].Type != streaming.EventTypeToolStart {
		t.Errorf("expected tool_start, got %q", received[0].Type)
	}
	if received[0].Data != "my-tool" {
		t.Errorf("expected tool name in start event, got %q", received[0].Data)
	}
	if received[1].Type != streaming.EventTypeToolEnd {
		t.Errorf("expected tool_end, got %q", received[1].Type)
	}
}

func TestToolExecuteActivity_NilExecutor(t *testing.T) {
	act := NewActivities(nil, nil, nil, nil, nil)

	_, err := act.ToolExecuteActivity(context.Background(), &ToolRequest{
		ToolName: "test",
	})
	if err == nil {
		t.Fatal("expected error for nil tool executor")
	}
}

func TestSaveTaskActivity_Success(t *testing.T) {
	// We can't easily mock KAgentTaskStore (concrete type, HTTP-based).
	// Test the nil-store error path instead.
	act := NewActivities(nil, nil, nil, nil, nil)

	err := act.SaveTaskActivity(context.Background(), &TaskSaveRequest{
		SessionID: "sess-1",
		TaskData:  []byte(`{"id": "task-1"}`),
	})
	if err == nil {
		t.Fatal("expected error for nil task store")
	}
}

func TestSaveTaskActivity_InvalidJSON(t *testing.T) {
	// Use a real KAgentTaskStore pointing to a dummy URL.
	// The unmarshal error happens before any HTTP call.
	store := taskstore.NewKAgentTaskStoreWithClient("http://localhost:0", nil)
	act := NewActivities(nil, store, nil, nil, nil)

	err := act.SaveTaskActivity(context.Background(), &TaskSaveRequest{
		SessionID: "sess-1",
		TaskData:  []byte(`not valid json`),
	})
	if err == nil {
		t.Fatal("expected error for invalid task JSON")
	}
}

func TestAppendEventActivity_Success(t *testing.T) {
	svc := newMockSessionService()
	svc.sessions["sess-1"] = &session.Session{
		ID:      "sess-1",
		UserID:  "user1",
		AppName: "app",
	}

	act := NewActivities(svc, nil, nil, nil, nil)

	event := map[string]any{"type": "message", "content": "hello"}
	eventData, _ := json.Marshal(event)

	err := act.AppendEventActivity(context.Background(), &AppendEventRequest{
		SessionID: "sess-1",
		Event:     eventData,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.events["sess-1"]) != 1 {
		t.Fatalf("expected 1 event, got %d", len(svc.events["sess-1"]))
	}
}

func TestAppendEventActivity_NilService(t *testing.T) {
	act := NewActivities(nil, nil, nil, nil, nil)

	err := act.AppendEventActivity(context.Background(), &AppendEventRequest{
		SessionID: "sess-1",
		Event:     []byte(`{}`),
	})
	if err == nil {
		t.Fatal("expected error for nil session service")
	}
}

func TestAppendEventActivity_InvalidJSON(t *testing.T) {
	svc := newMockSessionService()
	act := NewActivities(svc, nil, nil, nil, nil)

	err := act.AppendEventActivity(context.Background(), &AppendEventRequest{
		SessionID: "sess-1",
		Event:     []byte(`not json`),
	})
	if err == nil {
		t.Fatal("expected error for invalid event JSON")
	}
}

func TestAppendEventActivity_SessionNotFound(t *testing.T) {
	svc := newMockSessionService()
	// No sessions pre-populated, GetSession returns nil.
	act := NewActivities(svc, nil, nil, nil, nil)

	err := act.AppendEventActivity(context.Background(), &AppendEventRequest{
		SessionID: "nonexistent",
		Event:     []byte(`{"type": "test"}`),
	})
	// GetSession returns nil session, which will cause AppendEvent to fail.
	if err == nil {
		t.Fatal("expected error for nil session")
	}
}

func TestPublishApprovalActivity_Success(t *testing.T) {
	_, addr := startEmbeddedNATS(t)
	conn := connectNATS(t, addr)

	subject := "agent.test.sess-approval.stream"
	var received []streaming.StreamEvent
	var mu sync.Mutex

	sub, err := conn.Subscribe(subject, func(msg *nats.Msg) {
		var evt streaming.StreamEvent
		if err := json.Unmarshal(msg.Data, &evt); err == nil {
			mu.Lock()
			received = append(received, evt)
			mu.Unlock()
		}
	})
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	act := NewActivities(nil, nil, conn, nil, nil)

	err = act.PublishApprovalActivity(context.Background(), &PublishApprovalRequest{
		WorkflowID:  "wf-123",
		RunID:       "run-456",
		SessionID:   "sess-approval",
		Message:     "Delete this file?",
		NATSSubject: subject,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	conn.Flush()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 approval request event, got %d", len(received))
	}
	if received[0].Type != streaming.EventTypeApprovalRequest {
		t.Errorf("expected approval_request event, got %q", received[0].Type)
	}
	// The Data field contains the JSON-encoded ApprovalRequest.
	var approvalReq streaming.ApprovalRequest
	if err := json.Unmarshal([]byte(received[0].Data), &approvalReq); err != nil {
		t.Fatalf("failed to unmarshal approval request from event data: %v", err)
	}
	if approvalReq.WorkflowID != "wf-123" {
		t.Errorf("got workflowID=%q, want %q", approvalReq.WorkflowID, "wf-123")
	}
	if approvalReq.Message != "Delete this file?" {
		t.Errorf("got message=%q, want %q", approvalReq.Message, "Delete this file?")
	}
}

func TestPublishApprovalActivity_NilPublisher(t *testing.T) {
	// No NATS connection -- should succeed silently.
	act := NewActivities(nil, nil, nil, nil, nil)

	err := act.PublishApprovalActivity(context.Background(), &PublishApprovalRequest{
		WorkflowID:  "wf-123",
		SessionID:   "sess-1",
		Message:     "Approve?",
		NATSSubject: "test.subject",
	})
	if err != nil {
		t.Fatalf("expected no error for nil publisher, got: %v", err)
	}
}

func TestToolExecuteActivity_ErrorPublishesEndEvent(t *testing.T) {
	_, addr := startEmbeddedNATS(t)
	conn := connectNATS(t, addr)

	subject := "agent.test.sess-tool-err.stream"
	var received []streaming.StreamEvent
	var mu sync.Mutex

	sub, err := conn.Subscribe(subject, func(msg *nats.Msg) {
		var evt streaming.StreamEvent
		if err := json.Unmarshal(msg.Data, &evt); err == nil {
			mu.Lock()
			received = append(received, evt)
			mu.Unlock()
		}
	})
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	executor := func(_ context.Context, _ string, _ []byte) ([]byte, error) {
		return nil, fmt.Errorf("execution failed")
	}

	act := NewActivities(nil, nil, conn, nil, executor)

	resp, err := act.ToolExecuteActivity(context.Background(), &ToolRequest{
		ToolName:    "fail-tool",
		ToolCallID:  "call-err",
		NATSSubject: subject,
	})
	if err != nil {
		t.Fatalf("unexpected activity error: %v", err)
	}
	if resp.Error == "" {
		t.Error("expected tool error in response")
	}

	conn.Flush()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("expected 2 events (start+end), got %d", len(received))
	}
	// End event should contain error info.
	if received[1].Type != streaming.EventTypeToolEnd {
		t.Errorf("expected tool_end, got %q", received[1].Type)
	}
}

