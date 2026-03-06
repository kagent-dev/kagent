package streaming

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// startEmbeddedNATS starts an in-process NATS server on a random port for testing.
func startEmbeddedNATS(t *testing.T) (*natsserver.Server, string) {
	t.Helper()
	opts := &natsserver.Options{
		Host:     "127.0.0.1",
		Port:     -1, // random port
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
	addr := ns.ClientURL()
	t.Cleanup(func() {
		ns.Shutdown()
		ns.WaitForShutdown()
	})
	return ns, addr
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

func TestNewStreamEvent(t *testing.T) {
	before := time.Now().UnixMilli()
	event := NewStreamEvent(EventTypeToken, "hello")
	after := time.Now().UnixMilli()

	if event.Type != EventTypeToken {
		t.Errorf("expected type %s, got %s", EventTypeToken, event.Type)
	}
	if event.Data != "hello" {
		t.Errorf("expected data %q, got %q", "hello", event.Data)
	}
	if event.Timestamp < before || event.Timestamp > after {
		t.Errorf("timestamp %d not in range [%d, %d]", event.Timestamp, before, after)
	}
}

func TestStreamEventSerialization(t *testing.T) {
	event := &StreamEvent{
		Type:      EventTypeToolStart,
		Data:      "my-tool",
		Timestamp: 1700000000000,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded StreamEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Type != event.Type {
		t.Errorf("type mismatch: got %s, want %s", decoded.Type, event.Type)
	}
	if decoded.Data != event.Data {
		t.Errorf("data mismatch: got %q, want %q", decoded.Data, event.Data)
	}
	if decoded.Timestamp != event.Timestamp {
		t.Errorf("timestamp mismatch: got %d, want %d", decoded.Timestamp, event.Timestamp)
	}
}

func TestSubjectForAgent(t *testing.T) {
	tests := []struct {
		agent   string
		session string
		want    string
	}{
		{"myagent", "sess123", "agent.myagent.sess123.stream"},
		{"a", "b", "agent.a.b.stream"},
	}
	for _, tt := range tests {
		got := SubjectForAgent(tt.agent, tt.session)
		if got != tt.want {
			t.Errorf("SubjectForAgent(%q, %q) = %q, want %q", tt.agent, tt.session, got, tt.want)
		}
	}
}

func TestPublishSubscribeRoundtrip(t *testing.T) {
	_, addr := startEmbeddedNATS(t)

	pubConn := connectNATS(t, addr)
	subConn := connectNATS(t, addr)

	pub := NewStreamPublisher(pubConn)
	sub := NewStreamSubscriber(subConn)

	subject := SubjectForAgent("testagent", "sess1")

	var received StreamEvent
	var mu sync.Mutex
	done := make(chan struct{})

	subscription, err := sub.Subscribe(subject, func(event *StreamEvent) {
		mu.Lock()
		defer mu.Unlock()
		received = *event
		close(done)
	})
	if err != nil {
		t.Fatalf("subscribe error: %v", err)
	}
	defer subscription.Unsubscribe()

	// Flush to ensure subscription is active on the server
	if err := subConn.Flush(); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	event := NewStreamEvent(EventTypeToken, "hello world")
	if err := pub.PublishToken(subject, event); err != nil {
		t.Fatalf("publish error: %v", err)
	}
	if err := pubConn.Flush(); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	mu.Lock()
	defer mu.Unlock()
	if received.Type != EventTypeToken {
		t.Errorf("received type %s, want %s", received.Type, EventTypeToken)
	}
	if received.Data != "hello world" {
		t.Errorf("received data %q, want %q", received.Data, "hello world")
	}
}

func TestPublishToolProgress(t *testing.T) {
	_, addr := startEmbeddedNATS(t)

	pubConn := connectNATS(t, addr)
	subConn := connectNATS(t, addr)

	pub := NewStreamPublisher(pubConn)
	sub := NewStreamSubscriber(subConn)

	subject := SubjectForAgent("agent1", "sess2")

	var events []StreamEvent
	var mu sync.Mutex
	allReceived := make(chan struct{})

	subscription, err := sub.Subscribe(subject, func(event *StreamEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, *event)
		if len(events) == 2 {
			close(allReceived)
		}
	})
	if err != nil {
		t.Fatalf("subscribe error: %v", err)
	}
	defer subscription.Unsubscribe()
	subConn.Flush()

	startEvent := NewStreamEvent(EventTypeToolStart, "calculator")
	endEvent := NewStreamEvent(EventTypeToolEnd, "calculator")

	if err := pub.PublishToolProgress(subject, startEvent); err != nil {
		t.Fatalf("publish tool start error: %v", err)
	}
	if err := pub.PublishToolProgress(subject, endEvent); err != nil {
		t.Fatalf("publish tool end error: %v", err)
	}
	pubConn.Flush()

	select {
	case <-allReceived:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for tool events")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != EventTypeToolStart {
		t.Errorf("first event type %s, want %s", events[0].Type, EventTypeToolStart)
	}
	if events[1].Type != EventTypeToolEnd {
		t.Errorf("second event type %s, want %s", events[1].Type, EventTypeToolEnd)
	}
}

func TestPublishApprovalRequest(t *testing.T) {
	_, addr := startEmbeddedNATS(t)

	pubConn := connectNATS(t, addr)
	subConn := connectNATS(t, addr)

	pub := NewStreamPublisher(pubConn)
	sub := NewStreamSubscriber(subConn)

	subject := SubjectForAgent("agent1", "sess3")

	done := make(chan StreamEvent, 1)

	subscription, err := sub.Subscribe(subject, func(event *StreamEvent) {
		done <- *event
	})
	if err != nil {
		t.Fatalf("subscribe error: %v", err)
	}
	defer subscription.Unsubscribe()
	subConn.Flush()

	req := &ApprovalRequest{
		WorkflowID: "wf-123",
		RunID:      "run-456",
		SessionID:  "sess3",
		Message:    "approve tool execution?",
		ToolName:   "dangerous-tool",
		ToolID:     "tc-789",
	}
	if err := pub.PublishApprovalRequest(subject, req); err != nil {
		t.Fatalf("publish approval request error: %v", err)
	}
	pubConn.Flush()

	select {
	case event := <-done:
		if event.Type != EventTypeApprovalRequest {
			t.Errorf("event type %s, want %s", event.Type, EventTypeApprovalRequest)
		}
		// Verify the nested ApprovalRequest can be decoded from Data
		var decoded ApprovalRequest
		if err := json.Unmarshal([]byte(event.Data), &decoded); err != nil {
			t.Fatalf("failed to decode approval request from event data: %v", err)
		}
		if decoded.WorkflowID != "wf-123" {
			t.Errorf("WorkflowID %q, want %q", decoded.WorkflowID, "wf-123")
		}
		if decoded.ToolName != "dangerous-tool" {
			t.Errorf("ToolName %q, want %q", decoded.ToolName, "dangerous-tool")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for approval request event")
	}
}

func TestSubscriptionCleanup(t *testing.T) {
	_, addr := startEmbeddedNATS(t)

	pubConn := connectNATS(t, addr)
	subConn := connectNATS(t, addr)

	pub := NewStreamPublisher(pubConn)
	sub := NewStreamSubscriber(subConn)

	subject := SubjectForAgent("agent1", "sess4")

	callCount := 0
	var mu sync.Mutex

	subscription, err := sub.Subscribe(subject, func(event *StreamEvent) {
		mu.Lock()
		defer mu.Unlock()
		callCount++
	})
	if err != nil {
		t.Fatalf("subscribe error: %v", err)
	}
	subConn.Flush()

	// Publish one event before unsubscribe
	if err := pub.PublishToken(subject, NewStreamEvent(EventTypeToken, "before")); err != nil {
		t.Fatalf("publish error: %v", err)
	}
	pubConn.Flush()

	// Wait for delivery
	time.Sleep(200 * time.Millisecond)

	// Unsubscribe
	if err := subscription.Unsubscribe(); err != nil {
		t.Fatalf("unsubscribe error: %v", err)
	}

	// Publish another event after unsubscribe
	if err := pub.PublishToken(subject, NewStreamEvent(EventTypeToken, "after")); err != nil {
		t.Fatalf("publish error: %v", err)
	}
	pubConn.Flush()

	// Wait to ensure no more events arrive
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if callCount != 1 {
		t.Errorf("expected 1 event before unsubscribe, got %d", callCount)
	}
}

func TestMultipleSubscribers(t *testing.T) {
	_, addr := startEmbeddedNATS(t)

	pubConn := connectNATS(t, addr)
	sub1Conn := connectNATS(t, addr)
	sub2Conn := connectNATS(t, addr)

	pub := NewStreamPublisher(pubConn)
	sub1 := NewStreamSubscriber(sub1Conn)
	sub2 := NewStreamSubscriber(sub2Conn)

	subject := SubjectForAgent("agent1", "sess5")

	var wg sync.WaitGroup
	wg.Add(2)

	var received1, received2 StreamEvent

	s1, err := sub1.Subscribe(subject, func(event *StreamEvent) {
		received1 = *event
		wg.Done()
	})
	if err != nil {
		t.Fatalf("subscribe1 error: %v", err)
	}
	defer s1.Unsubscribe()
	sub1Conn.Flush()

	s2, err := sub2.Subscribe(subject, func(event *StreamEvent) {
		received2 = *event
		wg.Done()
	})
	if err != nil {
		t.Fatalf("subscribe2 error: %v", err)
	}
	defer s2.Unsubscribe()
	sub2Conn.Flush()

	event := NewStreamEvent(EventTypeToken, "broadcast")
	if err := pub.PublishToken(subject, event); err != nil {
		t.Fatalf("publish error: %v", err)
	}
	pubConn.Flush()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for both subscribers")
	}

	if received1.Data != "broadcast" {
		t.Errorf("subscriber1 data %q, want %q", received1.Data, "broadcast")
	}
	if received2.Data != "broadcast" {
		t.Errorf("subscriber2 data %q, want %q", received2.Data, "broadcast")
	}
}

func TestNewNATSConnection(t *testing.T) {
	_, addr := startEmbeddedNATS(t)

	conn, err := NewNATSConnection(addr)
	if err != nil {
		t.Fatalf("NewNATSConnection error: %v", err)
	}
	defer conn.Close()

	if !conn.IsConnected() {
		t.Error("expected connection to be connected")
	}
}

func TestNewNATSConnectionBadAddr(t *testing.T) {
	_, err := NewNATSConnection("nats://127.0.0.1:1")
	if err == nil {
		t.Error("expected error connecting to bad address")
	}
}

func TestPublishToClosedConnection(t *testing.T) {
	_, addr := startEmbeddedNATS(t)
	conn := connectNATS(t, addr)
	pub := NewStreamPublisher(conn)

	conn.Close()

	err := pub.PublishToken("test.subject", NewStreamEvent(EventTypeToken, "data"))
	if err == nil {
		t.Error("expected error publishing to closed connection")
	}
}

func TestMalformedMessageIgnored(t *testing.T) {
	_, addr := startEmbeddedNATS(t)

	rawConn := connectNATS(t, addr)
	subConn := connectNATS(t, addr)

	sub := NewStreamSubscriber(subConn)

	subject := "test.malformed"
	called := false
	var mu sync.Mutex

	subscription, err := sub.Subscribe(subject, func(event *StreamEvent) {
		mu.Lock()
		defer mu.Unlock()
		called = true
	})
	if err != nil {
		t.Fatalf("subscribe error: %v", err)
	}
	defer subscription.Unsubscribe()
	subConn.Flush()

	// Publish raw malformed JSON
	if err := rawConn.Publish(subject, []byte("not json")); err != nil {
		t.Fatalf("publish error: %v", err)
	}
	rawConn.Flush()

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if called {
		t.Error("handler should not be called for malformed messages")
	}
}
